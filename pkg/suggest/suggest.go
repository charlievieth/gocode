package suggest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"hash/maphash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charlievieth/buildutil"
	"github.com/mdempsky/gocode/pkg/cache"
	"github.com/mdempsky/gocode/pkg/lookdot"
)

//go:generate go run -tags generate genstdlib.go

type Config struct {
	// TODO:
	// 	1. initialize and (optionally) match to file (if excluded)
	// 	   this might require using a per-Suggest Context to avoid
	// 	   races (basically, this needs to be read-only)
	// 	2. make sure we upgrade any nil funcs to the fast versions
	Context *build.Context

	Importer           types.Importer
	Logf               func(fmt string, args ...interface{})
	Builtin            bool
	IgnoreCase         bool
	UnimportedPackages bool
}

// TODO: use an LRU fileCache for this???
var fileCache = struct {
	lock  sync.Mutex
	files map[string]fileCacheEntry
	fset  *token.FileSet
}{
	files: make(map[string]fileCacheEntry),
	fset:  token.NewFileSet(),
}

type fileCacheEntry struct {
	file  *ast.File
	mtime time.Time
	size  int64
	hash  uint64
}

// Suggest returns a list of suggestion candidates and the length of
// the text that should be replaced, if any.
func (c *Config) Suggest(filename string, data []byte, cursor int) ([]Candidate, int) {
	if cursor < 0 {
		return nil, 0
	}

	filename = filepath.Clean(filename)
	ctxt := c.context(filename, data)
	fset, pos, pkg, imports := c.analyzePackage(ctxt, filename, data, cursor)
	if pkg == nil {
		c.Logf("no package found for %s", filename)
		return nil, 0
	}
	scope := pkg.Scope().Innermost(pos)

	ctx, expr, partial := deduceCursorContext(data, cursor)
	b := candidateCollector{
		localpkg:   pkg,
		imports:    imports,
		partial:    partial,
		filter:     objectFilters[partial],
		builtin:    ctx != selectContext && c.Builtin,
		ignoreCase: c.IgnoreCase,
	}

	switch ctx {
	case emptyResultsContext:
		// don't show results in certain cases
		return nil, 0

	case selectContext:
		tv, _ := types.Eval(fset, pkg, pos, expr)
		if lookdot.Walk(&tv, b.appendObject) {
			break
		}

		_, obj := scope.LookupParent(expr, pos)
		if pkgName, isPkg := obj.(*types.PkgName); isPkg {
			c.packageCandidates(pkgName.Imported(), &b)
			break
		}
		if !c.UnimportedPackages {
			return nil, 0
		}
		pkg := c.resolveKnownPackageIdent(expr)
		if pkg == nil {
			return nil, 0
		}
		c.packageCandidates(pkg, &b)

	case compositeLiteralContext:
		tv, _ := types.Eval(fset, pkg, pos, expr)
		if tv.IsType() {
			if _, isStruct := tv.Type.Underlying().(*types.Struct); isStruct {
				c.fieldNameCandidates(tv.Type, &b)
				break
			}
		}
		fallthrough
	case unknownContext:
		c.scopeCandidates(scope, pos, &b)
	}

	res := b.getCandidates()
	if len(res) == 0 {
		return nil, 0
	}
	return res, len(partial)
}

func (c *Config) context(filename string, data []byte) *build.Context {
	// TODO: match Context to filename
	dupe := build.Default
	if c.Context != nil {
		dupe = *c.Context
	}
	ctxt := &dupe
	// WARN
	ctxt.OpenFile = cache.ContextOpenFile(ctxt)
	return ctxt
}

/*
func (c *Config) parseOtherFile(filename string) *ast.File {
	entry := cache.files[filename]

	fi, err := os.Stat(filename)
	if err != nil {
		// TODO(mdempsky): How to handle this cleanly?
		panic(err)
	}

	if entry.mtime != fi.ModTime() {
		file, err := parser.ParseFile(cache.fset, filename, nil, 0)
		if err != nil {
			c.logParseError(fmt.Sprintf("Error parsing %q", filename), err)
		}
		trimAST(file, token.NoPos)

		entry = fileCacheEntry{file, fi.ModTime()}
		cache.files[filename] = entry
	}

	return entry.file
}
*/

var hashSeed = maphash.MakeSeed()

func hashData(b []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	_, _ = h.Write(b)
	return h.Sum64()
}

func readFile(name string, sizeHint int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	if sizeHint < 0 {
		if fi, err := f.Stat(); err == nil {
			sizeHint = fi.Size()
		}
	}
	var size int
	if int64(int(sizeHint)) == sizeHint {
		size = int(sizeHint)
	}
	size += 512 // extra bytes in case the size hint is short

	data := make([]byte, 0, size)
	for {
		if len(data) >= cap(data) {
			d := append(data[:cap(data)], 0)
			data = d[:len(data)]
		}
		n, err := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}

func openFile(ctxt *build.Context, name string) (io.ReadCloser, error) {
	if fn := ctxt.OpenFile; fn != nil {
		return fn(name)
	}
	return os.Open(name)
}

// TODO: check file content before invalidating cache
func (c *Config) parseOtherFile(ctxt *build.Context, filename string) *ast.File {
	entry := fileCache.files[filename]

	fi, err := os.Stat(filename)
	if err != nil {
		// TODO(mdempsky): How to handle this cleanly?
		panic(err)
	}

	// WARN: use Context.OpenFile since it caches the results

	if !entry.mtime.Equal(fi.ModTime()) {
		// Check if only the modtime changed
		// data, err := readFile(filename, fi.Size()) // TODO: use Context.OpenFile
		// if err != nil {
		// 	// WARN: handle
		// 	// if os.IsNotExist(err) {
		// 	// }
		// }

		rc, err := openFile(ctxt, filename)
		if err != nil {
			// WARN: handle
			// if os.IsNotExist(err) {
			// }
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			// WARN: handle
			// if os.IsNotExist(err) {
			// }
		}

		hash := hashData(data)
		if hash == entry.hash && entry.size == fi.Size() {
			return entry.file
		}

		file, err := parser.ParseFile(fileCache.fset, filename, data, 0)
		if err != nil {
			// WARN: do we want to cache invalid files ???
			c.logParseError(fmt.Sprintf("Error parsing %q", filename), err)
		}
		trimAST(file, token.NoPos)

		entry = fileCacheEntry{
			file:  file,
			mtime: fi.ModTime(),
			size:  fi.Size(),
			hash:  hash,
		}
		fileCache.files[filename] = entry
	}

	return entry.file
}

// TODO: return this from analyzePackage() and don't export
type Package struct {
	FileSet     *token.FileSet
	Pos         token.Pos
	Package     *types.Package
	ImportSpecs []*ast.ImportSpec
}

func (c *Config) analyzePackage(ctxt *build.Context, filename string, data []byte, cursor int) (*token.FileSet, token.Pos, *types.Package, []*ast.ImportSpec) {
	fileCache.lock.Lock()
	defer fileCache.lock.Unlock()

	// Reset every 1GB of files so fset doesn't overflow.
	if fileCache.fset.Base() >= 1e9 {
		fileCache.fset = token.NewFileSet()
		fileCache.files = make(map[string]fileCacheEntry)
	}

	// TODO: increase the number of entries
	//
	// Delete random files to keep the cache at most 100 entries.
	for k := range fileCache.files {
		if len(fileCache.files) <= 100 {
			break
		}
		delete(fileCache.files, k)
	}

	// If we're in trailing white space at the end of a scope,
	// sometimes go/types doesn't recognize that variables should
	// still be in scope there.
	filesemi := bytes.Join([][]byte{data[:cursor], []byte(";"), data[cursor:]}, nil)

	fileAST, err := parser.ParseFile(fileCache.fset, filename, filesemi, parser.AllErrors)
	if err != nil {
		c.logParseError("Error parsing input file (outer block)", err)
	}
	astPos := fileAST.Pos()
	if astPos == 0 {
		return nil, token.NoPos, nil, nil
	}
	pos := fileCache.fset.File(astPos).Pos(cursor)
	trimAST(fileAST, pos)

	files := []*ast.File{fileAST}
	for _, otherName := range c.findOtherPackageFiles(ctxt, filename, fileAST.Name.Name) {
		files = append(files, c.parseOtherFile(ctxt, otherName))
	}

	cfg := types.Config{
		Importer: c.Importer,
		Error:    func(err error) {},
	}
	pkg, _ := cfg.Check("", fileCache.fset, files, nil)

	return fileCache.fset, pos, pkg, fileAST.Imports
}

// trimAST clears any part of the AST not relevant to type checking
// expressions at pos.
func trimAST(file *ast.File, pos token.Pos) {
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if pos < n.Pos() || pos >= n.End() {
			switch n := n.(type) {
			case *ast.FuncDecl:
				n.Body = nil
			case *ast.BlockStmt:
				n.List = nil
			case *ast.CaseClause:
				n.Body = nil
			case *ast.CommClause:
				n.Body = nil
			case *ast.CompositeLit:
				// Leave elts in place for [...]T
				// array literals, because they can
				// affect the expression's type.
				if !isEllipsisArray(n.Type) {
					n.Elts = nil
				}
			}
		}
		return true
	})
}

func isEllipsisArray(n ast.Expr) bool {
	at, ok := n.(*ast.ArrayType)
	if !ok {
		return false
	}
	_, ok = at.Len.(*ast.Ellipsis)
	return ok
}

func (c *Config) fieldNameCandidates(typ types.Type, b *candidateCollector) {
	s := typ.Underlying().(*types.Struct)
	for i, n := 0, s.NumFields(); i < n; i++ {
		b.appendObject(s.Field(i))
	}
}

func (c *Config) packageCandidates(pkg *types.Package, b *candidateCollector) {
	c.scopeCandidates(pkg.Scope(), token.NoPos, b)
}

func mergeStrings(s1, s2, scratch []string) []string {
	a := scratch[0:cap(scratch)]
	i, j, k := 0, 0, 0
	for ; i < len(s1) && j < len(s2) && k < len(a); k++ {
		if s1[i] == s2[j] {
			a[k] = s2[j]
			i++
			j++
		} else if s1[i] < s2[j] {
			a[k] = s1[i]
			i++
		} else {
			a[k] = s2[j]
			j++
		}
	}
	if i < len(s1) {
		k += copy(a[k:], s1[i:])
	}
	if j < len(s2) {
		k += copy(a[k:], s2[j:])
	}
	return a[:k]
}

func (c *Config) scopeCandidates(scope *types.Scope, pos token.Pos, b *candidateCollector) {
	// Previously this method used a map to skip duplicate
	// names, which was slow.
	var all []string
	var scratch []string
	for sc := scope; sc != nil; sc = sc.Parent() {
		names := sc.Names()
		if len(all) == 0 {
			all = names
			continue
		}
		if len(names) > 0 {
			if n := len(all) + len(names); cap(scratch) < n {
				// TODO: can we use append for this?
				scratch = make([]string, n)
			}
			all = mergeStrings(all, names, scratch)
		}
	}
	for _, name := range all {
		_, obj := scope.LookupParent(name, pos)
		if obj != nil {
			b.appendObject(obj)
		}
	}
}

func (c *Config) logParseError(intro string, err error) {
	if c.Logf == nil {
		return
	}
	if el, ok := err.(scanner.ErrorList); ok {
		c.Logf("%s:", intro)
		for _, er := range el {
			c.Logf(" %s", er)
		}
	} else {
		c.Logf("%s: %s", intro, err)
	}
}

type fileEntryName interface {
	Name() string
}

// func readdirnames(dirname string) ([]string, error) {
// 	f, err := os.Open(dirname)
// 	if err != nil {
// 		return nil, err
// 	}
// 	names, err := f.Readdirnames(-1)
// 	f.Close()
// 	return names, err
// }

func readDir(ctxt *build.Context, name string) ([]fs.DirEntry, error) {
	if fn := ctxt.ReadDir; fn != nil {
		fis, err := fn(name)
		if err != nil {
			return nil, err
		}
		des := make([]fs.DirEntry, len(fis))
		for i, fi := range fis {
			des[i] = fs.FileInfoToDirEntry(fi)
		}
		return des, nil
	}
	return os.ReadDir(name)
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// package_files_tests
func (c *Config) findOtherPackageFiles(ctxt *build.Context, filename, pkgName string) []string {
	if filename == "" {
		return nil
	}

	dir, file := filepath.Split(filename)

	// TODO: use Context.ReadDir
	dents, err := os.ReadDir(dir)
	if err != nil {
		panic(err) // WARN WARN
	}
	isTestFile := strings.HasSuffix(file, "_test.go")

	a := dents[:0]
	for _, d := range dents {
		if d.Type().IsDir() {
			continue
		}
		name := d.Name()
		if len(name) == 0 || name[0] == '.' || name[0] == '_' {
			continue
		}
		if name == file || !strings.HasSuffix(name, ".go") {
			continue
		}
		if !isTestFile && strings.HasSuffix(name, "_test.go") {
			continue
		}
		a = append(a, d)
	}
	dents = a

	// WARN: 4
	if len(dents) == 0 {
		return nil
	}
	if len(dents) <= 2 {
		out := make([]string, 0, 2)
		for _, d := range dents {
			path := dir + d.Name()
			pname, ok := buildutil.ShortImport(ctxt, path)
			if ok && pname == pkgName {
				out = append(out, path)
			}
		}
		return out
	}

	// TODO(mdempsky): Use go/build.(*Context).MatchFile or
	// something to properly handle build tags?

	n := min(len(dents), 4)
	workc := make(chan string, min(len(dents), n*16))
	out := make([]string, 0, len(dents))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range workc {
				pname, ok := buildutil.ShortImport(ctxt, path)
				if ok && pname == pkgName {
					mu.Lock()
					out = append(out, path)
					mu.Unlock()
				}
			}
		}()
	}
	for _, d := range dents {
		workc <- dir + d.Name()
	}
	close(workc)
	wg.Wait()
	sort.Strings(out)

	return out
}

func (c *Config) resolveKnownPackageIdent(pkgName string) *types.Package {
	pkgName, ok := knownPackageIdents[pkgName]
	if !ok {
		return nil
	}
	pkg, _ := c.Importer.Import(pkgName)
	return pkg
}

func pkgNameFor(filename string) string {
	file, _ := parser.ParseFile(token.NewFileSet(), filename, nil, parser.PackageClauseOnly)
	if file == nil {
		return ""
	}
	return file.Name.Name
}
