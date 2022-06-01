package cache

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdempsky/gocode/pkg/cache/lru"
)

const (
	DefaultAstCacheSize    = 500
	DefaultSourceCacheSize = 32
	DefaultFileSetMaxSize  = 1e9
)

// TODO: use an unsafe.Pointer
var defaultAstCache atomic.Value

func init() {
	defaultAstCache.Store(new(AstCache))
}

type astCacheEntry struct {
	file *ast.File
	err  error
	// TODO: consider using access time instead
	ctime unixTime       // entry created at
	mtime atomicUnixTime // file modified at
	size  int64
	hash  uint64
}

type sourceCacheEntry struct {
	file *ast.File
	mode parser.Mode
	err  error
}

// TODO: don't export this and instead export a Parser type
//
// WARN: should only be accessed via LoadAstCache() otherwise limits won't be
// respected. We can't enforce them while parsing because for a large directory
// we might remove files that we need.
type AstCache struct {
	mu          sync.Mutex
	initialized bool
	cache       map[string]*astCacheEntry
	sourceCache *lru.LockingCache
	fset        *token.FileSet
	Size        int // Max number of *ast.Files (<0 disables this)
	SourceSize  int // Max number of *ast.Files parsed from source
	FileSetSize int // Max size of the *token.FileSet (<0 disables this)
}

// copy returns an empy copy of the AstCache
func (c *AstCache) copy() *AstCache {
	return &AstCache{
		Size:        c.Size,
		SourceSize:  c.SourceSize,
		FileSetSize: c.FileSetSize,
	}
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
		// We create a new cache on init so we can ignore
		// the race here
		n := new(AstCache)
		defaultAstCache.Store(n)
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

	// TODO(charlie): use a heap or something to remove the oldest entries first
	//
	// Delete random files to keep the cache from growing too large
	c.mu.Lock()
	if len(c.cache) > c.Size {
		for k := range c.cache {
			if len(c.cache) <= c.Size {
				break
			}
			delete(c.cache, k)
		}
	}
	c.mu.Unlock()

	return c
}

func (c *AstCache) initialize() {
	if c.initialized {
		return
	}
	if c.Size == 0 {
		c.Size = DefaultAstCacheSize
	}
	if c.SourceSize == 0 {
		c.SourceSize = DefaultSourceCacheSize
	}
	if c.SourceSize < 0 {
		c.SourceSize = 0 // unlimited
	}
	if c.FileSetSize == 0 {
		c.FileSetSize = DefaultFileSetMaxSize
	}
	c.cache = make(map[string]*astCacheEntry)
	c.fset = token.NewFileSet()
	c.sourceCache = lru.NewLocking(c.SourceSize)
	c.initialized = true
}

func (c *AstCache) FileSet() *token.FileSet {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		c.initialize()
	}
	return c.fset
}

func (c *AstCache) delete(filename string) {
	c.mu.Lock()
	if c.cache != nil {
		delete(c.cache, filename)
	}
	c.mu.Unlock()
}

func (c *AstCache) ParseSource(filename string, data []byte, mode parser.Mode) (*ast.File, error) {
	fset := c.FileSet() // this initializes the AstCache
	if v, ok := c.sourceCache.GetB(data); ok {
		if ent, _ := v.(*sourceCacheEntry); ent != nil && ent.mode&mode == mode {
			return ent.file, ent.err
		}
	}
	af, err := parser.ParseFile(fset, filename, data, mode)
	c.sourceCache.Add(string(data), &sourceCacheEntry{
		file: af,
		err:  err,
		mode: mode,
	})
	return af, err
}

func (c *AstCache) ParsePackageFile(filename string) (*ast.File, error) {
	c.mu.Lock()
	if !c.initialized {
		c.initialize()
	}
	ent, ok := c.cache[filename]
	c.mu.Unlock()

	if ok {
		if fi, err := os.Stat(filename); err == nil {
			if fi.Size() == ent.size {
				if ent.mtime.Equal(fi.ModTime()) {
					return ent.file, ent.err
				}
				if hash, _ := hashFile(filename); hash == ent.hash {
					// Update ModTime since the file content did not change
					ent.mtime.Set(fi.ModTime())
					return ent.file, ent.err
				}
			}
		}
	}

	data, fi, err := readFile(filename)
	if err != nil {
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

	c.mu.Lock()
	// WARN(charlie): there is a race here since we don't re-check the condition
	c.cache[filename] = &astCacheEntry{
		file:  af,
		err:   err,
		ctime: newUnixTime(time.Now()),
		mtime: newAtomicUnixTime(fi.ModTime()),
		size:  fi.Size(),
		hash:  hash,
	}
	c.mu.Unlock()
	return af, err
}

func numWorkers(nitems int) int {
	n := runtime.NumCPU()
	if n < nitems {
		n = nitems
	}
	if runtime.GOOS == "darwin" && n > 8 {
		n = 8
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

/*
type FileCache struct {
	mu      sync.Mutex
	cache   map[string]*FileCacheEntry
	MaxSize int
	New     func(filename string) (interface{}, error)

	// TODO: do we want this?
	// SingleFlight bool
}

// func (c *FileCache) doLoad(filename string) (*FileCacheEntry, bool) {
// 	c.mu.Lock()
// 	return nil, nil
// }

func (c *FileCache) storeError(filename string, err error) {
}

func (c *FileCache) swap(filename string, old, new *FileCacheEntry) bool {
	return true
}

func (c *FileCache) Load(filename string) (*FileCacheEntry, error) {
	c.mu.Lock()
	ent, ok := c.cache[filename]
	c.mu.Unlock()
	if ok {
		fi, err := os.Stat(filename)
		if err != nil {
			// Remove
		}
		if ent.ModTime().Equal(fi.ModTime()) {
			return ent, nil
		}
	}
	// TODO: stat the file after opening to avoid a race condition
	data, err := os.ReadFile(filename)
	if err != nil {
		// Remove
	}
	_ = data

	// hash := hashData(data)
	// if ent.Size() {
	// }
	return nil, nil
}

type atomicTime struct {
	time *time.Time
}

func (a *atomicTime) Store(t time.Time) {
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&a.time)),
		(unsafe.Pointer)(unsafe.Pointer(&t)))
}

func (a *atomicTime) load() *time.Time {
	return (*time.Time)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&a.time))))
}

func (a *atomicTime) Load() (time.Time, bool) {
	if t := a.load(); t != nil {
		return *t, true
	}
	return time.Time{}, false
}

func (a *atomicTime) Time() time.Time {
	t, _ := a.Load()
	return t
}

func (a *atomicTime) String() string {
	if t := a.load(); t != nil {
		return t.String()
	}
	return "<nil>"
}

type FileCacheEntry struct {
	path    string
	modTime atomicTime
	size    int64
	hash    uint64
	value   interface{}
	err     error
}

func (e *FileCacheEntry) CheckValid() (bool, error) {
	if e == nil {
		return false, nil
	}

	// TODO: do we want to return the Entry's error?
	// if e.Error() != nil {
	// 	return false, e.Error()
	// }

	fi, err := os.Stat(e.Path())
	if err != nil {
		return false, err
	}
	if fi.ModTime().Equal(e.ModTime()) && fi.Size() == e.Size() {
		return true, nil
	}
	// Check if only the modtime changed
	if fi.Size() == e.Size() {
		hash, err := hashFile(e.Path())
		if err != nil {
			return false, err
		}
		if hash == e.Hash() {
			// Update modtime: file content did not change
			e.modTime.Store(fi.ModTime())
			return true, nil
		}
	}
	return false, nil
}

func (e *FileCacheEntry) Path() string {
	if e != nil {
		return e.path
	}
	return ""
}

func (e *FileCacheEntry) ModTime() time.Time {
	if e != nil {
		return e.modTime.Time()
	}
	return time.Time{}
}

func (e *FileCacheEntry) Size() int64 {
	if e != nil {
		return e.size
	}
	return 0
}

func (e *FileCacheEntry) Hash() uint64 {
	if e != nil {
		return e.hash
	}
	return 0
}

func (e *FileCacheEntry) Value() interface{} {
	if e != nil {
		return e.value
	}
	return nil
}

func (e *FileCacheEntry) Error() error {
	if e != nil {
		return e.err
	}
	return nil
}
*/
