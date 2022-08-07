package cache

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/charlievieth/buildutil"
	"github.com/mdempsky/gocode/pkg/cache/lru"
)

type matchCache struct {
	ids      [3]ContextKeyID
	matches  [3]bool
	overflow map[ContextKeyID]bool
}

func newMatchCache(id ContextKeyID, match bool) matchCache {
	var m matchCache
	m.ids[0] = id
	m.matches[0] = match
	return m
}

func (x *matchCache) Set(id ContextKeyID, match bool) {
	// Lock must be held
	for i := 0; i < len(x.ids); i++ {
		if x.ids[i] == 0 {
			x.ids[i] = id
			x.matches[i] = match
			return
		}
	}
	if x.overflow == nil {
		x.overflow = make(map[ContextKeyID]bool)
	}
	x.overflow[id] = match
}

func (x *matchCache) Get(id ContextKeyID) (match, found bool) {
	// Lock must be held
	for i := 0; i < len(x.ids); i++ {
		if x.ids[i] == id {
			return x.matches[i], true
		}
	}
	if x.overflow != nil {
		match, found = x.overflow[id]
	}
	return
}

type fileCacheEntry struct {
	mu      sync.RWMutex
	match   matchCache
	pkgName string
	file    *ast.File
	err     error          // parse or match error
	mtime   atomicUnixTime // file modified at
	size    int64
	hash    uint64
}

func (e *fileCacheEntry) GetMatch(id ContextKeyID) (match, found bool) {
	if e != nil {
		e.mu.RLock()
		match, found = e.match.Get(id)
		e.mu.RUnlock()
	}
	return
}

func (e *fileCacheEntry) SetMatch(id ContextKeyID, match bool) {
	if e != nil {
		e.mu.Lock()
		e.match.Set(id, match)
		e.mu.Unlock()
	}
}

func (e *fileCacheEntry) GetFile() (file *ast.File, err error) {
	if e != nil {
		e.mu.RLock()
		file = e.file
		err = e.err
		e.mu.RUnlock()
	}
	return
}

var defaultFileCache = new(fileCache)

type fileCache struct {
	mu          sync.Mutex
	files       *lru.Cache
	fset        *token.FileSet
	matches     *MatchCache
	initialized bool
	MaxEntries  int
}

func (c *fileCache) FileSet() *token.FileSet {
	c.mu.Lock()
	if !c.initialized {
		c.initialize()
	}
	fset := c.fset
	c.mu.Unlock()
	return fset
}

func (c *fileCache) initialize() {
	if c.initialized {
		return
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = DefaultAstCacheSize
	}
	if c.MaxEntries < 0 {
		c.MaxEntries = 0 // unlimited - not recommended
	}
	c.files = lru.New(c.MaxEntries)
	c.fset = token.NewFileSet()
}

func (c *fileCache) get(filename string) (*fileCacheEntry, bool) {
	if v, ok := c.files.Get(filename); ok {
		return v.(*fileCacheEntry), true
	}
	return nil, false
}

func (c *fileCache) ParseDir(ctxt *build.Context, fset *token.FileSet,
	dir, pkgName string, filter func(fs.DirEntry) bool) ([]*ast.File, error) {

	panic("BROKEN")

	dir = filepath.Clean(dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	des = filterDirEntries(ctxt, des, filter)
	if len(des) == 0 {
		return nil, nil // WARN: do we want to return nil, nil ???
	}

	n := numWorkers(len(des))
	ch := make(chan fs.DirEntry, n*2)
	cc := fileCacheContext{
		files:   make([]*ast.File, 0, len(des)+1),
		pkgName: pkgName,
		dir:     dir,
		ctxtID:  ContextCacheKeyID(ContextCacheKey(ctxt)),
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go cc.doWork(fset, ch, &wg)
	}
	for _, d := range des {
		ch <- d
	}
	close(ch)
	wg.Wait()

	return cc.files, cc.first
}

func (c *fileCache) Parser(ctxt *build.Context) *Parser {
	c.mu.Lock()
	if !c.initialized {
		c.initialize()
	}
	p := &Parser{
		ctxt:  ctxt,
		cache: c.files,
		fset:  c.fset,
	}
	c.mu.Unlock()
	return p
}

func LoadParser(ctxt *build.Context) *Parser {
	// WARN: allow nil Context?
	if ctxt == nil {
		dupe := build.Default
		ctxt = &dupe
	}
	return defaultFileCache.Parser(ctxt)
}

type Parser struct {
	ctxt  *build.Context
	cache *lru.Cache
	fset  *token.FileSet
}

func (p *Parser) get(filename string) (*fileCacheEntry, bool) {
	if v, ok := p.cache.Get(filename); ok {
		return v.(*fileCacheEntry), true
	}
	return nil, false
}

func (p *Parser) ParseDir(dir, pkgName string, filter func(fs.DirEntry) bool) ([]*ast.File, error) {

	dir = filepath.Clean(dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	des = filterDirEntries(p.ctxt, des, filter)
	if len(des) == 0 {
		return nil, nil // WARN: do we want to return nil, nil ???
	}

	n := numWorkers(len(des))
	ch := make(chan fs.DirEntry, n*2)
	cc := fileCacheContext{
		parser:  p,
		files:   make([]*ast.File, 0, len(des)+1),
		pkgName: pkgName,
		dir:     dir,
		ctxtID:  ContextCacheKeyID(ContextCacheKey(p.ctxt)),
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go cc.doWork(p.fset, ch, &wg)
	}
	for _, d := range des {
		ch <- d
	}
	close(ch)
	wg.Wait()

	return cc.files, cc.first
}

type fileCacheContext struct {
	parser  *Parser
	mu      sync.Mutex // file/error mutex
	files   []*ast.File
	first   error
	pkgName string // WARN: need this
	dir     string
	ctxtID  ContextKeyID
}

func (c *fileCacheContext) parseEntry(fset *token.FileSet, d fs.DirEntry) (*fileCacheEntry, error) {
	var (
		buf  *bytes.Buffer
		hash uint64
		info fs.FileInfo
	)
	defer putReadBuffer(buf)

	filename := c.dir + string(os.PathSeparator) + d.Name()
	ent, ok := c.parser.get(filename)
	if ok {
		fi, err := d.Info()
		if err != nil {
			return nil, err // WARN: remove entry
		}
		if fi.Size() != ent.size {
			goto NotFound
		}

		// Check if the file content changed
		if !ent.mtime.Equal(fi.ModTime()) {
			buf, info, err = readFileBuffer(filename)
			if err != nil {
				return nil, err // WARN: remove entry
			}
			if hash = hashData(buf.Bytes()); hash != ent.hash {
				goto NotFound
			}
		}

		// Cached file entry is valid

		// If the package names don't match ignore the file
		if ent.pkgName != c.pkgName {
			return nil, nil
		}

		// Check if the Context matches the file
		match, found := ent.GetMatch(c.ctxtID)
		if !found {
			// Check if the current Context matches
			if buf == nil {
				buf, info, err = readFileBuffer(filename)
				if err != nil {
					return nil, err // WARN: remove entry
				}
			}
			_, match, err = buildutil.MatchFile(c.parser.ctxt, c.dir, d.Name(), buf)
			if err != nil {
				return nil, err // WARN: remove entry
			}
			ent.SetMatch(c.ctxtID, match)
		}
		if !match {
			return nil, nil // excluded
		}

		// If cached entry did not match the Context when created
		// the ast.File will be nil.
		if file, err := ent.GetFile(); file != nil || err != nil {
			return ent, nil
		}
	}

NotFound:
	if buf == nil {
		var err error
		buf, info, err = readFileBuffer(filename)
		if err != nil {
			return nil, err // WARN: remove entry
		}
	}
	if hash == 0 {
		hash = hashData(buf.Bytes())
	}

	// WARN WARN WARN WARN WARN
	// Use the existing entry, if any
	// WARN WARN WARN WARN WARN
	//
	// If we previously matched the file, but didn't parse it the
	// entry will be non nil.
	if ent == nil {
		ent = &fileCacheEntry{
			mtime: newAtomicUnixTime(info.ModTime()),
			size:  info.Size(),
			hash:  hash,
		}
	}

	var file *ast.File
	pkgName, match, perr := buildutil.MatchFile(c.parser.ctxt, c.dir, d.Name(), buf.Bytes())
	if match {
		file, perr = parser.ParseFile(fset, filename, buf.Bytes(), 0)
	}
	c.parser.cache.Add(filename, &fileCacheEntry{
		match:   newMatchCache(c.ctxtID, match),
		file:    file,
		err:     perr,
		pkgName: pkgName,
		mtime:   newAtomicUnixTime(info.ModTime()),
		size:    info.Size(),
		hash:    hash,
	})

	return ent, nil
}

func (c *fileCacheContext) doWork(fset *token.FileSet, ch <-chan fs.DirEntry, wg *sync.WaitGroup) {
	for d := range ch {
		ent, err := c.parseEntry(fset, d)
		c.mu.Lock()
		if af, _ := ent.GetFile(); af != nil {
			c.files = append(c.files, ent.file)
		}
		if err != nil {
			c.parser.cache.Remove(c.dir + string(os.PathSeparator) + d.Name())
			if c.first == nil {
				c.first = err
			}
		}
		c.mu.Unlock()
	}
}

// type xfileCache struct {
// 	mu          sync.Mutex
// 	initialized bool
// 	files       *lru.Cache
// 	matches     *lru.Cache
// 	fset        *token.FileSet
// 	MaxEntries  int
// }

// func (c *xfileCache) MatchFile() {
// }
