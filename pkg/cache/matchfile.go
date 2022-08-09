package cache

import (
	"bytes"
	"go/build"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charlievieth/buildutil"
	"github.com/mdempsky/gocode/pkg/cache/lru"
)

const DefaultMatchCacheSize = 16 * 1024

var defaultMatchCache = NewMatchCache(DefaultMatchCacheSize)

func LoadMatchCache() *MatchCache {
	return defaultMatchCache
}

type matchEntry struct {
	pkgName string
	expr    *buildutil.Constraint
	modTime unixTime
}

type MatchCache struct {
	once  sync.Once
	cache *lru.Cache
	// strings stringInterner // interned package names

	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Negative means no limit and zero uses
	// the default of DefaultMatchCacheSize.
	MaxEntries int
}

func NewMatchCache(maxEntries int) *MatchCache {
	m := &MatchCache{MaxEntries: maxEntries}
	m.once.Do(m.initialize)
	return m
}

func (c *MatchCache) initialize() {
	if c.cache != nil {
		return
	}
	// This cache is pretty lightweight so we can make it large.
	if c.MaxEntries == 0 {
		c.MaxEntries = DefaultMatchCacheSize
	}
	if c.MaxEntries < 0 {
		c.MaxEntries = 0 // unlimited
	}
	c.cache = lru.New(c.MaxEntries)
}

func (c *MatchCache) Size() int {
	c.once.Do(c.initialize)
	return c.cache.Len()
}

func (c *MatchCache) get(filename string) (*matchEntry, bool) {
	if v, ok := c.cache.Get(filename); ok {
		return v.(*matchEntry), true
	}
	return nil, false
}

func (c *MatchCache) matchFile(ctxt *build.Context, filename string) (pkgName string, match bool, err error) {
	// TODO: use ctxt.OpenFile ???
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
	buf := getReadBuffer(4096)
	defer putReadBuffer(buf)
	rc := cachingReader{file: f, buf: buf}

	pkgName, err = buildutil.ReadPackageName(filename, &rc)
	if err != nil {
		return "", false, err
	}

	expr, err := buildutil.ParseConstraint(ctxt, filename, rc.Bytes())
	if err != nil {
		return "", false, err
	}

	// A nil buildutil.Constraint is safe to use
	if expr.Empty() {
		expr = nil
	}

	// WARN: there is a race here since we don't re-check the cache
	// but the odds of triggering it are low.
	c.cache.Add(filename, &matchEntry{
		pkgName: pkgName,
		expr:    expr,
		modTime: newUnixTime(fi.ModTime()),
	})

	return pkgName, expr.Eval(ctxt), nil
}

func (c *MatchCache) MatchFileInfo(ctxt *build.Context, dir string, info fs.FileInfo) (pkgName string, match bool, err error) {
	name := info.Name()
	// No point caching these
	if !strings.HasSuffix(name, ".go") || !buildutil.GoodOSArchFile(ctxt, name, nil) {
		return "", false, nil
	}

	c.once.Do(c.initialize)

	filename := filepath.Clean(dir + string(os.PathSeparator) + name)
	ent, ok := c.get(filename)
	if ok {
		if ent.modTime.Equal(info.ModTime()) {
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
	buf  *bytes.Buffer
}

func (r *cachingReader) Close() (err error) { return r.file.Close() }

func (r *cachingReader) Bytes() []byte             { return r.buf.Bytes() }
func (r *cachingReader) ReadCloser() io.ReadCloser { return io.NopCloser(r.buf) }

func (r *cachingReader) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	if n > 0 {
		if r.buf == nil {
			r.buf = new(bytes.Buffer)
		}
		r.buf.Write(p[:n])
	}
	return n, err
}
