package cache

import (
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charlievieth/buildutil"
	"github.com/mdempsky/gocode/pkg/cache/lru"
)

// TODO: consider using one entry for all Contexts
// type matchCacheEntryContext struct {
// 	matches map[string]bool // Context cache key => match
// 	// TODO: consider using access time instead
// 	ctime int64 // entry created at
// 	mtime int64 // file modified at
// 	size  int64
// 	hash  uint64
// }

type matchCacheEntry struct {
	match   bool
	pkgName string // package name
	// TODO: consider using access time instead
	ctime unixTime       // entry created at
	mtime atomicUnixTime // file modified at
	size  int64
	hash  uint64
}

type MatchCache struct {
	mu    sync.RWMutex
	cache map[string]*matchCacheEntry
}

func (c *MatchCache) Size() int {
	c.mu.RLock()
	n := len(c.cache)
	c.mu.RUnlock()
	return n
}

func (c *MatchCache) clear() {
	c.mu.Lock()
	c.cache = nil
	c.mu.Unlock()
}

func (c *MatchCache) hasExpiredEntries(expireTime time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ent := range c.cache {
		if ent.ctime.Before(expireTime) {
			return true
		}
	}
	return false
}

// TODO: use access time instead of created at time since that
// will be more accurate for cases where we're only working on
// one file in a directory.
func (c *MatchCache) cleanup(expireTime time.Time) {
	if !c.hasExpiredEntries(expireTime) {
		return
	}

	c.mu.RLock()
	// Find the newest file in each directory
	dirs := make(map[string]unixTime)
	for path, ent := range c.cache {
		dir := filepath.Dir(path)
		if dirs[dir] < ent.ctime {
			dirs[dir] = ent.ctime
		}
	}
	c.mu.RUnlock()

	// Remove any directories that have files newer than
	// the expiration time
	for dir, ctime := range dirs {
		if ctime.After(expireTime) {
			delete(dirs, dir)
		}
	}

	c.mu.Lock()
	n := len(c.cache)
	for path := range c.cache {
		dir := filepath.Dir(path)
		if _, ok := dirs[dir]; ok {
			delete(c.cache, path)
		}
	}
	switch {
	case len(c.cache) == 0:
		c.cache = nil
	case n >= 128 && len(c.cache) <= n/4:
		// shrink cache
		cache := make(map[string]*matchCacheEntry, len(c.cache))
		for k, v := range c.cache {
			cache[k] = v
		}
		c.cache = cache
	}
	c.mu.Unlock()
}

func (c *MatchCache) delete(path string) {
	c.mu.Lock()
	if c.cache != nil {
		delete(c.cache, path)
	}
	c.mu.Unlock()
}

func (c *MatchCache) MatchFile(ctxt *build.Context, dir, name string) (pkgName string, match bool, err error) {
	// No point caching these
	if !buildutil.GoodOSArchFile(ctxt, name, nil) {
		return "", false, nil
	}
	if !strings.HasSuffix(name, ".go") {
		return "", false, nil
	}

	path := filepath.Join(dir, name)
	c.mu.RLock()
	ent, ok := c.cache[path]
	c.mu.RUnlock()

	if ok {
		if fi, err := os.Stat(path); err == nil {
			if fi.Size() == ent.size {
				if ent.mtime.Equal(fi.ModTime()) {
					return ent.pkgName, ent.match, nil
				}
				if hash, _ := hashFile(path); hash == ent.hash {
					// Update ModTime since the file content did not change
					ent.mtime.Set(fi.ModTime())
					return ent.pkgName, ent.match, nil
				}
			}
		}
	}

	// TODO(charlie): storing the hash might not be worth it
	// considering how fast this already is.

	buf, fi, err := readFileBuffer(path)
	if err != nil {
		c.delete(path)
		return "", false, err
	}
	defer putReadBuffer(buf)
	hash := hashData(buf.Bytes())

	// Check if only modtime changed (unlikely since we checked above)
	if ok && ent.size == fi.Size() && ent.hash == hash {
		// Update modtime
		ent.mtime.Set(fi.ModTime())
		return ent.pkgName, ent.match, nil
	}

	pkgName, match, err = buildutil.MatchFile(ctxt, dir, name, buf.Bytes())
	if err != nil {
		c.delete(path)
		return pkgName, match, err
	}

	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]*matchCacheEntry)
	}
	// Re-check the cache
	old := c.cache[path]
	if old != nil && old != ent && old.mtime.After(fi.ModTime()) {
		match = old.match // a newer cache entry was added
		pkgName = old.pkgName
	} else {
		c.cache[path] = &matchCacheEntry{
			match:   match,
			pkgName: pkgName,
			ctime:   newUnixTime(time.Now()),
			mtime:   newAtomicUnixTime(fi.ModTime()),
			size:    fi.Size(),
			hash:    hash,
		}
	}
	c.mu.Unlock()

	return pkgName, match, nil
}

var matchCaches sync.Map
var initCleanupExpiredMatchEntriesOnce sync.Once

func cleanupExpiredMatchEntries() {
	const MaxAge = time.Hour * -1
	go func() {
		tick := time.NewTicker(time.Minute * 5)
		defer tick.Stop()
		for range tick.C {
			matchCaches.Range(func(k, v interface{}) bool {
				c := v.(*MatchCache)
				c.cleanup(time.Now().Add(MaxAge)) // 1 hour old
				if c.Size() == 0 {
					matchCaches.Delete(k)
				}
				return true
			})
		}
	}()
}

func LoadMatchCache(ctxt *build.Context) *MatchCache {
	initCleanupExpiredMatchEntriesOnce.Do(cleanupExpiredMatchEntries)
	key := ContextCacheKey(ctxt)
	if v, ok := matchCaches.Load(key); ok {
		return v.(*MatchCache)
	}
	v, _ := matchCaches.LoadOrStore(key, new(MatchCache))
	return v.(*MatchCache)
}

type stringInterner struct {
	mu      sync.RWMutex      // strings mutex
	strings map[string]string // interned package names
}

func (n *stringInterner) Intern(s string) string {
	if s == "" {
		return ""
	}
	n.mu.RLock()
	ii, ok := n.strings[s]
	n.mu.RUnlock()
	if ok {
		return ii
	}

	n.mu.Lock()
	// recheck condition
	if ii, ok = n.strings[s]; !ok {
		if n.strings == nil {
			n.strings = make(map[string]string)
		}
		ii = strings.Clone(s)
		n.strings[ii] = ii
	}
	n.mu.Unlock()
	return ii
}

type xmatchEntry struct {
	pkgName string
	expr    *buildutil.Constraint
	modTime unixTime
}

type XMatchCache struct {
	once       sync.Once         // init once
	cache      *lru.LockingCache // WARN: use or remove
	strings    stringInterner    // interned package names
	MaxEntries int
}

func (c *XMatchCache) initialize() {
	if c.cache != nil {
		return
	}
	// This cache is pretty lightweight so we can make it large.
	if c.MaxEntries == 0 {
		c.MaxEntries = 8192 // TODO: make this a global const
	}
	if c.MaxEntries < 0 {
		c.MaxEntries = 0 // unlimited
	}
	c.cache = lru.NewLocking(c.MaxEntries)
}

func (c *XMatchCache) get(filename string) (*xmatchEntry, bool) {
	if v, ok := c.cache.Get(filename); ok {
		return v.(*xmatchEntry), true
	}
	return nil, false
}

func (c *XMatchCache) matchFile(ctxt *build.Context, filename string) (pkgName string, match bool, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}

	// Cache the data read by ParseConstraint so we can reuse it
	// to read the package name without having to reset the read
	// offset of the file.
	rc := cachingReader{file: f}

	pkgName, err = buildutil.ReadPackageName(filename, &rc)
	if err != nil {
		return "", false, err
	}

	expr, err := buildutil.ParseConstraint(ctxt, filename, rc.data)
	if err != nil {
		return "", false, err
	}

	// A nil buildutil.Constraint is safe to use
	if expr.Empty() {
		expr = nil
	}
	c.cache.Add(filename, &xmatchEntry{
		pkgName: c.strings.Intern(pkgName),
		expr:    expr,
		modTime: newUnixTime(fi.ModTime()),
	})

	return pkgName, expr.Eval(ctxt), nil
}

func (c *XMatchCache) MatchFile(ctxt *build.Context, dir, name string) (pkgName string, match bool, err error) {
	// No point caching these
	if !strings.HasSuffix(name, ".go") || !buildutil.GoodOSArchFile(ctxt, name, nil) {
		return "", false, nil
	}

	c.once.Do(c.initialize)

	filename := filepath.Join(dir, name)
	ent, ok := c.get(filename)
	if ok {
		fi, err := os.Stat(filename)
		if err != nil {
			c.cache.Remove(filename)
			return "", false, err
		}
		if ent.modTime.Equal(fi.ModTime()) {
			return ent.pkgName, ent.expr.Eval(ctxt), nil
		}
	}

	pkgName, match, err = c.matchFile(ctxt, filename)
	if err != nil && ok {
		c.cache.Remove(filename)
	}
	return pkgName, match, err
}

var _ io.ReadCloser = (*cachingReader)(nil)

type cachingReader struct {
	file *os.File
	data []byte
}

func (r *cachingReader) Close() error { return r.file.Close() }

func (r *cachingReader) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	if n > 0 {
		if r.data == nil {
			r.data = make([]byte, n)
			copy(r.data, p[:n])
		} else {
			r.data = append(r.data, p[:n]...)
		}
	}
	return n, err
}

/*
var pkgNameCache struct {
	sync.RWMutex
	names map[string]string
}

func internPackageName(name string) string {
	if name == "" {
		return ""
	}
	c := &pkgNameCache
	c.RLock()
	s := c.names[name]
	c.RUnlock()
	if s != "" {
		return s
	}

	c.Lock()
	// re-check condition
	s = c.names[name]
	if s == "" {
		if c.names == nil {
			c.names = make(map[string]string)
		}
		s = strings.Clone(name)
		c.names[s] = s
	}
	c.Unlock()
	return s
}
*/
