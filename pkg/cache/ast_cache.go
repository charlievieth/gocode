package cache

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charlievieth/buildutil"
	"github.com/mdempsky/gocode/pkg/cache/lru"
)

const (
	// TODO: can we measure memory usage for the stored AST instead of
	// using a fixed number?
	//
	// NOTE: It takes about 100MB to store the trimmed ast of the 1,702
	// Go source files in the go1.18 standard library.
	DefaultAstCacheSize   = 4096
	DefaultFileSetMaxSize = 1e9
)

// TODO: use an unsafe.Pointer
var defaultAstCache atomic.Value

func init() {
	defaultAstCache.Store(&AstCache{
		Size:        DefaultAstCacheSize,
		FileSetSize: DefaultFileSetMaxSize,
	})
}

type astCacheEntry struct {
	file  *ast.File
	err   error
	mtime atomicUnixTime // file modified at
	size  int64
	hash  uint64
}

// TODO: don't export this and instead export a Parser type
//
// WARN: should only be accessed via LoadAstCache() otherwise limits won't be
// respected. We can't enforce them while parsing because for a large directory
// we might remove files that we need.
type AstCache struct {
	once        sync.Once
	cache       *lru.Cache
	fset        *token.FileSet
	Match       *MatchCache
	Size        int // Max number of *ast.Files (<0 disables this)
	FileSetSize int // Max size of the *token.FileSet (<0 disables this)

	// Logf func(format string, v ...interface{}) // Optional logger
}

// func (c *AstCache) logf(format string, v ...interface{}) {
// 	if c.Logf != nil {
// 		c.Logf(format, v...)
// 	}
// }

// copy returns an empty copy of the AstCache, but preserves the MatchCache
func (c *AstCache) copy() *AstCache {
	dupe := &AstCache{
		Match:       c.Match,
		Size:        c.Size,
		FileSetSize: c.FileSetSize,
	}
	dupe.once.Do(dupe.initialize)
	return dupe
}

func SetDefaultAstCacheLimits(size, fileSetSize int) {
	// WARN: this blows away the current cache
	defaultAstCache.Store(&AstCache{
		Size:        size,
		FileSetSize: fileSetSize,
	})
}

func LoadAstCache() *AstCache {
	c, ok := defaultAstCache.Load().(*AstCache)
	if !ok || c == nil {
		c = &AstCache{
			Size:        DefaultAstCacheSize,
			FileSetSize: DefaultFileSetMaxSize,
		}
		c.once.Do(c.initialize)
		// We create a new cache on init so we can ignore
		// the race here
		defaultAstCache.Store(c)
	}

	// If the FileSet size limit is exceeded create and return a new AstCache.
	// This allows for limits to be enforced without interfering with ongoing
	// parse operations that depend on the FileSet.

	// Reset every 1GB of files so fset doesn't overflow.
	if c.FileSet().Base() >= c.FileSetSize {
		// WARN: there is a race condition here
		c = c.copy()
		defaultAstCache.Store(c)
	}

	c.cache.Trim(c.Size)

	return c
}

func (c *AstCache) initialize() {
	if c.Size == 0 {
		c.Size = DefaultAstCacheSize
	}
	if c.FileSetSize == 0 {
		c.FileSetSize = DefaultFileSetMaxSize
	}
	if c.Match == nil {
		c.Match = LoadMatchCache() // Use defaults
	}
	c.cache = lru.New(0) // Unlimited, we trim it in LoadAstCache
	c.fset = token.NewFileSet()
}

func (c *AstCache) FileSet() *token.FileSet {
	c.once.Do(c.initialize)
	return c.fset
}

func (c *AstCache) delete(filename string) {
	c.cache.Remove(filename)
}

func (c *AstCache) get(filename string) (*astCacheEntry, bool) {
	if v, ok := c.cache.Get(filename); ok {
		return v.(*astCacheEntry), true
	}
	return nil, false
}

func (c *AstCache) ParsePackageFile(filename string) (*ast.File, error) {
	c.once.Do(c.initialize)
	ent, ok := c.get(filename)
	if ok {
		if fi, err := os.Stat(filename); err == nil {
			if fi.Size() == ent.size {
				if ent.mtime.Equal(fi.ModTime()) {
					return ent.file, ent.err
				}
				// WARN: if the file changed we hash it twice
				if hash, _ := hashFile(filename); hash == ent.hash {
					// Update ModTime since the file content did not change
					ent.mtime.Set(fi.ModTime())
					return ent.file, ent.err
				}
			}
		}
	}

	data, fi, err := readFile(filename)
	if err != nil && ok {
		c.delete(filename)
		return nil, err
	}
	hash := hashData(data)

	// Check if only modtime changed (unlikely since we checked above)
	if ok && ent.size == fi.Size() && ent.hash == hash {
		// Update modtime
		ent.mtime.Set(fi.ModTime())
		return ent.file, ent.err
	}

	af, err := parser.ParseFile(c.fset, filename, data, 0)
	if af != nil {
		trimAST(af, token.NoPos)
	}

	// WARN(charlie): there is a race here since we don't re-check the condition
	c.cache.Add(filename, &astCacheEntry{
		file:  af,
		err:   err,
		mtime: newAtomicUnixTime(fi.ModTime()),
		size:  fi.Size(),
		hash:  hash,
	})
	return af, err
}

func (c *AstCache) parsePackageFileInfo(filename string, info fs.FileInfo) (*ast.File, error) {
	c.once.Do(c.initialize)

	ent, ok := c.get(filename)
	if ok && info != nil && ent.mtime.Equal(info.ModTime()) {
		return ent.file, ent.err
	}

	buf, fi, err := readFileBuffer(filename)
	if err != nil {
		if ok {
			c.delete(filename)
		}
		return nil, err
	}
	defer putReadBuffer(buf)
	hash := hashData(buf.Bytes())

	// Check if only modtime changed
	if ok && ent.size == fi.Size() && ent.hash == hash {
		// Update modtime
		ent.mtime.Set(fi.ModTime())
		return ent.file, ent.err
	}

	af, err := parser.ParseFile(c.fset, filename, buf, 0)
	if af != nil {
		trimAST(af, token.NoPos)
	}

	// WARN(charlie): there is a race here since we don't re-check the condition
	c.cache.Add(filename, &astCacheEntry{
		file:  af,
		err:   err,
		mtime: newAtomicUnixTime(fi.ModTime()),
		size:  fi.Size(),
		hash:  hash,
	})
	return af, err
}

// TODO: tune this so that each worker has at least N items of work
// where N > 1.
func numWorkers(nitems int) int {
	if nitems <= 1 {
		return 1
	}
	n := runtime.NumCPU()
	if nitems < n {
		n = nitems
	}
	if runtime.GOOS == "darwin" && n > 8 {
		n = 8
	} else if n <= 1 {
		n = 1
	}
	return n
}

func (c *AstCache) ParsePackageFiles(filenames []string) ([]*ast.File, error) {
	switch len(filenames) {
	case 0:
		return nil, nil
	case 1:
		af, err := c.ParsePackageFile(filenames[0])
		if af != nil {
			return []*ast.File{af}, err
		}
		return nil, err
	default:

		files := make([]*ast.File, 0, len(filenames)+1) // +1 for source file
		n := numWorkers(len(filenames))
		ch := make(chan string, n*2)
		var (
			mu    sync.Mutex
			wg    sync.WaitGroup
			first error
		)
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
				for name := range ch {
					af, err := c.ParsePackageFile(name)
					mu.Lock()
					if af != nil {
						files = append(files, af)
					}
					if err != nil && !os.IsNotExist(err) {
						if first == nil {
							first = err
						}
					}
					mu.Unlock()
				}
			}()
		}
		for _, name := range filenames {
			ch <- name
		}
		close(ch)
		wg.Wait()

		return files, first
	}
}

func FilterForFile(filename string) func(base string) bool {
	// NOTE: filterDirEntries() checks the file type and GoodOsArchFile()
	base := filepath.Base(filename)
	includeTest := strings.HasSuffix(base, "_test.go")
	return func(name string) bool {
		return name != base && (includeTest || !strings.HasSuffix(name, "_test.go"))
	}
}

// WARN: remove!
func filterDirEntries(ctxt *build.Context, des []fs.DirEntry, filter func(fs.DirEntry) bool) []fs.DirEntry {
	if len(des) == 0 {
		return des
	}
	a := des[:0]
	for _, d := range des {
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if filter != nil && !filter(d) {
			continue
		}
		if !buildutil.GoodOSArchFile(ctxt, name, nil) {
			continue
		}
		a = append(a, d)
	}
	return a
}

func removeNilFiles(files []*ast.File) []*ast.File {
	if len(files) == 0 {
		return nil
	}
	var i int
	for i = 0; i < len(files) && files[i] != nil; i++ {
	}
	if i >= len(files) {
		return files
	}

	a := files[:i]
	for ; i < len(files); i++ {
		if af := files[i]; af != nil {
			a = append(a, af)
		}
	}
	if len(a) == 0 {
		return nil
	}
	return a
}

func filterFiles(ctxt *build.Context, names []string, filter func(name string) bool) []string {
	if len(names) == 0 {
		return names
	}
	a := names[:0]
	for _, name := range names {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if filter != nil && !filter(name) {
			continue
		}
		if !buildutil.GoodOSArchFile(ctxt, name, nil) {
			continue
		}
		a = append(a, name)
	}
	return a
}

func (c *AstCache) ParsePackage(ctxt *build.Context, dirname, pkgName string,
	filter func(name string) bool) ([]*ast.File, error) {

	names, err := readGoNames(dirname, true)
	if err != nil {
		return nil, NewMultiError(err)
	}
	names = filterFiles(ctxt, names, filter)
	if len(names) == 0 {
		return nil, nil // WARN: nil nil ??
	}

	// Make sure the dirname is clean since it is used as a cache key
	dirname = filepath.Clean(dirname)

	files := make([]*ast.File, len(names), len(names)+1) // Add 1 for the current file
	delta := len(names) / numWorkers(len(names))
	if delta == 0 {
		delta = 1
	}

	c.once.Do(c.initialize)
	m := c.Match

	var wg sync.WaitGroup
	var multiErr MultiErrorBuilder
	for i := 0; i < len(names); i += delta {
		j := i + delta
		if j >= len(names) {
			j = len(names)
		}
		wg.Add(1)
		// TODO: log all errors
		go func(names []string, files []*ast.File, merr *MultiErrorBuilder) {
			defer wg.Done()
			for i, name := range names {
				path := dirname + string(filepath.Separator) + name
				fi, err := os.Stat(path)
				if err != nil {
					if !os.IsNotExist(err) {
						merr.Add(err)
					}
					continue
				}
				if fi.IsDir() {
					continue
				}
				pkg, match, err := m.MatchFileInfo(ctxt, dirname, fi)
				if err != nil {
					merr.Add(err)
					continue
				}
				if !match || pkg != pkgName {
					continue
				}
				af, err := c.parsePackageFileInfo(path, fi)
				if err != nil {
					merr.Add(err) // don't continue here (allow for invalid source)
				}
				files[i] = af
			}
		}(names[i:j], files[i:j], &multiErr)
	}
	wg.Wait()

	return removeNilFiles(files), multiErr.ToError()
}

// ParsePackage parses all of Go source files dirname that have package name
// pkgName, are matched by build.Context ctxt, and are not excluded by the
// filter. Any returned error will be of type MultiError.
func (c *AstCache) ParsePackage_OLD(ctxt *build.Context, dirname, pkgName string,
	filter func(fs.DirEntry) bool) ([]*ast.File, error) {

	des, err := os.ReadDir(dirname)
	if err != nil {
		return nil, NewMultiError(err)
	}

	des = filterDirEntries(ctxt, des, filter)
	if len(des) == 0 {
		return nil, nil // WARN: nil nil ??
	}

	// Make sure the dirname is clean since it is used as a cache key
	dirname = filepath.Clean(dirname)

	files := make([]*ast.File, len(des), len(des)+1) // Add 1 for the current file
	delta := len(des) / numWorkers(len(des))
	if delta == 0 {
		delta = 1
	}

	c.once.Do(c.initialize)
	m := c.Match

	var wg sync.WaitGroup
	var multiErr MultiErrorBuilder
	for i := 0; i < len(des); i += delta {
		j := i + delta
		if j >= len(des) {
			j = len(des)
		}
		wg.Add(1)
		// TODO: log all errors
		go func(des []fs.DirEntry, files []*ast.File, merr *MultiErrorBuilder) {
			defer wg.Done()
			for i, d := range des {
				fi, err := d.Info()
				if err != nil {
					if !os.IsNotExist(err) {
						merr.Add(err)
					}
					continue
				}
				pkg, match, err := m.MatchFileInfo(ctxt, dirname, fi)
				if err != nil {
					merr.Add(err)
					continue
				}
				if !match || pkg != pkgName {
					continue
				}
				path := dirname + string(os.PathSeparator) + d.Name()
				af, err := c.parsePackageFileInfo(path, fi)
				if err != nil {
					merr.Add(err)
					continue
				}
				files[i] = af
			}
		}(des[i:j], files[i:j], &multiErr)
	}
	wg.Wait()

	return removeNilFiles(files), multiErr.ToError()
}

// TODO(charlie): move this to a shared location
//
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

// TODO(charlie): move this to a shared location
func isEllipsisArray(n ast.Expr) bool {
	at, ok := n.(*ast.ArrayType)
	if !ok {
		return false
	}
	_, ok = at.Len.(*ast.Ellipsis)
	return ok
}
