package suggest

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path/filepath"

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

	// TOOD: add this (but maybe make it the default)
	// NoMatchContextToFile bool
}

// TODO: use an LRU fileCache for this???
// var fileCache = struct {
// 	lock  sync.Mutex
// 	files map[string]fileCacheEntry
// 	fset  *token.FileSet
// }{
// 	files: make(map[string]fileCacheEntry),
// 	fset:  token.NewFileSet(),
// }

// type fileCacheEntry struct {
// 	file  *ast.File
// 	mtime time.Time
// 	size  int64
// 	hash  uint64
// }

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

	// TODO: deduceCursorContext() needs some work
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
	ctxt, err := buildutil.MatchContext(c.Context, filename, data)
	if err != nil {
		if c.Logf != nil {
			c.Logf("failed to match build.Context:", err)
		}
		ctxt = c.Context
		if ctxt == nil {
			dupe := build.Default
			ctxt = &dupe
		}
	}
	// WARN: should we det Dir here ???
	// if ctxt.Dir == "" {
	// 	ctxt.Dir = filepath.Dir(filename)
	// }
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

/*
// TODO: check file content before invalidating cache
func (c *Config) parseOtherFile(ctxt *build.Context, filename string) *ast.File {
	entry := fileCache.files[filename]

	fi, err := os.Stat(filename)
	if err != nil {
		c.logParseError("Error stating other package file", err)
		return nil
	}

	// TODO: consider using Context.OpenFile() though this makes
	// caching using modtime impossible.

	if !entry.mtime.Equal(fi.ModTime()) {
		data, err := os.ReadFile(filename)
		if err != nil {
			c.logParseError("Error reading other package file", err)
			return nil
		}

		hash := hashData(data)
		if hash == entry.hash && entry.size == fi.Size() {
			// File content is unchanged - update the modification time
			entry.mtime = fi.ModTime()
			fileCache.files[filename] = entry
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
*/

// TODO: return this from analyzePackage() and don't export
type Package struct {
	FileSet     *token.FileSet
	Pos         token.Pos
	Package     *types.Package
	ImportSpecs []*ast.ImportSpec
}

// TODO: completion fails when there are simple errors
func (c *Config) analyzePackage(ctxt *build.Context, filename string, data []byte,
	cursor int) (*token.FileSet, token.Pos, *types.Package, []*ast.ImportSpec) {

	cc := cache.LoadAstCache()
	fset := cc.FileSet()

	// If we're in trailing white space at the end of a scope,
	// sometimes go/types doesn't recognize that variables should
	// still be in scope there.
	filesemi := bytes.Join([][]byte{data[:cursor], []byte(";"), data[cursor:]}, nil)

	fileAST, err := parser.ParseFile(fset, filename, filesemi, parser.AllErrors)
	if err != nil {
		c.logParseError("Error parsing input file (outer block)", err)
	}
	astPos := fileAST.Pos()
	if astPos == 0 {
		return nil, token.NoPos, nil, nil
	}
	pos := fset.File(astPos).Pos(cursor)

	// WARN: do we need to trim the current files AST?
	// trimAST(fileAST, pos)

	pkgFiles, err := cc.ParsePackage(
		ctxt,
		filepath.Dir(filename),
		fileAST.Name.Name,
		cache.FilterForFile(filename),
	)
	if err != nil && c.Logf != nil {
		c.Logf("parsing package files:", err)
	}
	files := append(pkgFiles, fileAST)

	// pkgFiles := c.findOtherPackageFiles(ctxt, filename, fileAST.Name.Name)
	// files := make([]*ast.File, 1, len(pkgFiles)+1)
	// files[0] = fileAST
	// for _, name := range pkgFiles {
	// 	if af := c.parseOtherFile(ctxt, name); af != nil {
	// 		files = append(files, af)
	// 	}
	// }

	cfg := types.Config{
		Importer: c.Importer,
		Error:    func(_ error) {},
	}
	pkg, _ := cfg.Check("", fset, files, nil)

	return fset, pos, pkg, fileAST.Imports
}

// TODO: data race due to concurrent map writes/reads
//
// switch len(pkgFiles) {
// case 0:
// 	// no-op
// case 1:
// 	if af := c.parseOtherFile(ctxt, pkgFiles[0]); af != nil {
// 		files = append(files, af)
// 	}
// default:
// 	var mu sync.Mutex
// 	var wg sync.WaitGroup
// 	n := min(4, len(pkgFiles))
// 	ch := make(chan string, n*16)
// 	for i := 0; i < n; i++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			for name := range ch {
// 				if af := c.parseOtherFile(ctxt, name); af != nil {
// 					mu.Lock()
// 					files = append(files, af)
// 					mu.Unlock()
// 				}
// 			}
// 		}()
// 	}
// 	for _, name := range pkgFiles {
// 		ch <- name
// 	}
// 	close(ch)
// 	wg.Wait()
// }

func (c *Config) fieldNameCandidates(typ types.Type, b *candidateCollector) {
	s := typ.Underlying().(*types.Struct)
	for i, n := 0, s.NumFields(); i < n; i++ {
		b.appendObject(s.Field(i))
	}
}

func (c *Config) packageCandidates(pkg *types.Package, b *candidateCollector) {
	c.scopeCandidates(pkg.Scope(), token.NoPos, b)
}

func mergeStrings(a1, a2, scratch []string) []string {
	a := scratch[0:cap(scratch)]
	i, j, k := 0, 0, 0
	for ; i < len(a1) && j < len(a2) && k < len(a); k++ {
		s1 := a1[i]
		s2 := a2[j]
		if s1 == s2 {
			a[k] = s2
			i++
			j++
		} else if s1 < s2 {
			a[k] = s1
			i++
		} else {
			a[k] = s2
			j++
		}
	}
	if i < len(a1) {
		k += copy(a[k:], a1[i:])
	}
	if j < len(a2) {
		k += copy(a[k:], a2[j:])
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

	// WARN: previously we used the scope where the Object was found
	// to look it up by name. Need to make sure that doing this does
	// not break anything.
	//
	// Previous implementation:
	//
	// 	func (c *Config) scopeCandidates(scope *types.Scope, pos token.Pos, b *candidateCollector) {
	// 		seen := make(map[string]bool)
	// 		for scope != nil {
	// 			for _, name := range scope.Names() {
	// 				if seen[name] {
	// 					continue
	// 				}
	// 				seen[name] = true
	// 				_, obj := scope.LookupParent(name, pos)
	// 				if obj != nil {
	// 					b.appendObject(obj)
	// 				}
	// 			}
	// 			scope = scope.Parent()
	// 		}
	// 	}
	//
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

// func min(a, b int) int {
// 	if a <= b {
// 		return a
// 	}
// 	return b
// }

// var packageFileCache struct {
// 	cache map[string]map[string]bool
// 	mu    sync.Mutex
// }

// type packageCacheEntry struct {
// 	match bool
// 	mtime time.Time
// 	size  int64
// 	hash  uint64
// }

/*
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
	m := cache.LoadMatchCache()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range workc {
				dir, name := filepath.Split(path)
				if n := len(dir); n > 0 && os.IsPathSeparator(dir[n-1]) {
					dir = dir[:n-1]
				}
				// TODO(cev): handle error
				pname, ok, _ := m.MatchFile(ctxt, dir, name)
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
*/

func (c *Config) resolveKnownPackageIdent(pkgName string) *types.Package {
	pkgName, ok := knownPackageIdents[pkgName]
	if !ok {
		return nil
	}
	pkg, _ := c.Importer.Import(pkgName)
	return pkg
}

// func pkgNameFor(filename string) string {
// 	file, _ := parser.ParseFile(token.NewFileSet(), filename, nil, parser.PackageClauseOnly)
// 	if file == nil {
// 		return ""
// 	}
// 	return file.Name.Name
// }

// func devCompareObjects(name string, want, got []types.Object) {
// 	o1 := append([]types.Object(nil), want...)
// 	o2 := append([]types.Object(nil), got...)
// 	sort.Slice(o1, func(i, j int) bool {
// 		return o1[i].Name() < o1[j].Name()
// 	})
// 	sort.Slice(o2, func(i, j int) bool {
// 		return o2[i].Name() < o2[j].Name()
// 	})
// 	if !reflect.DeepEqual(o1, o2) {
// 		fmt.Fprintf(os.Stderr, "suggest: %s mismatch:\ngot:  %v\nwant: %v\n",
// 			name, o2, o1)
// 	}
// }

// func (c *Config) scopeCandidates(scope *types.Scope, pos token.Pos, b *candidateCollector) {
// 	other := candidateCollector{
// 		exact:      append([]types.Object(nil), b.exact...),
// 		badcase:    append([]types.Object(nil), b.badcase...),
// 		imports:    append([]*ast.ImportSpec(nil), b.imports...),
// 		localpkg:   b.localpkg,
// 		partial:    b.partial,
// 		filter:     b.filter,
// 		builtin:    b.builtin,
// 		ignoreCase: b.ignoreCase,
// 	}
// 	c.scopeCandidates_OLD(scope, pos, b)
// 	c.scopeCandidates_NEW(scope, pos, &other)
// 	devCompareObjects("Exact", b.exact, other.exact)
// 	devCompareObjects("Badcase", b.badcase, other.badcase)
// }

// func (c *Config) scopeCandidates_OLD(scope *types.Scope, pos token.Pos, b *candidateCollector) {
// 	// seen := make(map[string]bool)
// 	seen := make(map[string]struct{})
// 	for scope != nil {
// 		for _, name := range scope.Names() {
// 			// if seen[name] {
// 			// 	continue
// 			// }
// 			// seen[name] = true
//
// 			if _, ok := seen[name]; ok {
// 				continue
// 			}
// 			seen[name] = struct{}{}
// 			_, obj := scope.LookupParent(name, pos)
// 			if obj != nil {
// 				b.appendObject(obj)
// 			}
// 		}
// 		scope = scope.Parent()
// 	}
// }
