package cache

import (
	"encoding/binary"
	"fmt"
	"go/build"
	goimporter "go/importer"
	"go/token"
	"go/types"
	"hash/maphash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/buildutil/contextutil"
	"github.com/mdempsky/gocode/pkg/cache/lru"
	"github.com/mdempsky/gocode/pkg/internal/buildid"
	"github.com/mdempsky/gocode/pkg/internal/srcimporter"
	"golang.org/x/sync/singleflight"
	"golang.org/x/tools/go/gcexportdata"
)

// TODO: determine if we need this and if the TTL should be longer (probably)
const (
	ArchiveFileTTL = time.Second * 5
	GoPkgCacheTTL  = time.Minute
)

// TODO(charlie): don't use build.Default since
//
// We need to mangle go/build.Default to make gcimporter work as
// intended, so use a lock to protect against concurrent accesses.
var buildDefaultLock sync.Mutex

// Mu must be held while using the cache importer.
var Mu sync.Mutex

// TODO(charlie): use an LRU cache?
var importCache = importerCache{
	fset:    token.NewFileSet(),
	imports: make(map[string]importCacheEntry),
}

func NewImporter(ctx *PackedContext, filename string, fallbackToSource bool, logger func(string, ...interface{})) types.ImporterFrom {
	importCache.clean()

	imp := &importer{
		ctx:              ctx,
		importerCache:    &importCache,
		fallbackToSource: fallbackToSource,
		logf:             logger,
	}
	// TODO(charlie): do we need GetGbProjectPaths() and/or should it also
	// handle go.mod/go.work files and whatnot?
	gbroot, gbvendor := GetGbProjectPaths(ctx, filename)
	if gbroot != "" {
		imp.gbroot, imp.gbvendor = gbroot, gbvendor
	}
	return imp
}

type importer struct {
	*importerCache
	gbroot, gbvendor string
	ctx              *PackedContext
	fallbackToSource bool
	logf             func(string, ...interface{})
}

type importerCache struct {
	fset    *token.FileSet // TODO: use per-package token.FileSet
	imports map[string]importCacheEntry
}

// TODO: use "build id"
type importCacheEntry struct {
	pkg        *types.Package
	importPath string // WARN: remove if not used
	mtime      atomicUnixTime
	htime      atomicUnixTime // time last hashed
	size       int64
	buildID    string
	fset       *token.FileSet // WARN: I don't think we need to persist this
}

func (i *importer) Import(importPath string) (*types.Package, error) {
	return i.ImportFrom(importPath, "", 0)
}

func (i *importer) ImportFrom(importPath, srcDir string, mode types.ImportMode) (*types.Package, error) {
	// TODO(charlie): don't lock the entire time and use singleflight
	buildDefaultLock.Lock()
	defer buildDefaultLock.Unlock()

	origDef := build.Default
	defer func() { build.Default = origDef }()

	def := &build.Default
	// The gb root of a project can be used as a $GOPATH because it contains pkg/.
	def.GOPATH = i.ctx.GOPATH
	if i.gbroot != "" {
		def.GOPATH = i.gbroot
	}
	def.GOARCH = i.ctx.GOARCH
	def.GOOS = i.ctx.GOOS
	def.GOROOT = i.ctx.GOROOT
	def.CgoEnabled = i.ctx.CgoEnabled
	def.UseAllFiles = i.ctx.UseAllFiles
	def.Compiler = i.ctx.Compiler
	def.BuildTags = i.ctx.BuildTags
	def.ToolTags = i.ctx.ToolTags
	def.ReleaseTags = i.ctx.ReleaseTags
	def.InstallSuffix = i.ctx.InstallSuffix
	def.SplitPathList = i.splitPathList
	def.JoinPath = i.joinPath

	i.logf("importing: %v, srcdir: %v", importPath, srcDir)
	// TODO: use our version of FindPkg
	filename, path := gcexportdata.Find(importPath, srcDir)
	// TODO: the cache key should be the filename
	entry, ok := i.imports[path]
	if filename == "" {
		i.logf("no gcexportdata file for %s", path)
		// If there is no export data, check the cache.
		// TODO(rstambler): Develop a better heuristic for entry eviction.
		if ok && time.Since(entry.mtime.Time()) <= time.Minute*20 {
			return entry.pkg, nil
		}
		// If there is no cache entry and the user has configured the correct
		// setting, import and cache using the source importer.
		var pkg *types.Package
		var err error
		if i.fallbackToSource {
			i.logf("cache: falling back to the source importer for %s", path)
			pkg, err = goimporter.For("source", nil).Import(path)
		} else {
			i.logf("cache: falling back to the source default for %s", path)
			pkg, err = goimporter.Default().Import(path)
		}
		if pkg == nil {
			i.logf("failed to fall back to another importer for %s: %v", path, err)
			return nil, err
		}
		now := time.Now()
		entry = importCacheEntry{
			pkg:   pkg,
			mtime: newAtomicUnixTime(now),
			htime: newAtomicUnixTime(now),
			fset:  nil, // WARN: nil
		}
		i.imports[path] = entry
		return entry.pkg, nil
	}

	// If there is export data for the package.
	fi, err := os.Stat(filename)
	if err != nil {
		i.logf("could not stat %s", filename)
		return nil, err
	}
	if !entry.mtime.Equal(fi.ModTime()) {
		f, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		in, err := gcexportdata.NewReader(f)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		// pkg, err := gcexportdata.Read(in, i.fset, make(map[string]*types.Package), path)
		pkg, err := gcexportdata.Read(in, fset, make(map[string]*types.Package), path)
		if err != nil {
			return nil, err
		}
		entry = importCacheEntry{
			pkg:   pkg,
			mtime: newAtomicUnixTime(fi.ModTime()),
			htime: newAtomicUnixTime(time.Now()),
			fset:  fset, // WARN: nil
		}
		i.imports[path] = entry
	}

	return entry.pkg, nil
}

// Delete random files to keep the cache at most 100 entries.
// Only call while holding the importer's mutex.
func (i *importerCache) clean() {
	for k := range i.imports {
		if len(i.imports) <= 100 {
			break
		}
		delete(i.imports, k)
	}
}

func (i *importer) splitPathList(list string) []string {
	res := filepath.SplitList(list)
	if i.gbroot != "" {
		res = append(res, i.gbroot, i.gbvendor)
	}
	return res
}

func (i *importer) joinPath(elem ...string) string {
	res := filepath.Join(elem...)

	if i.gbroot != "" {
		// Want to rewrite "$GBROOT/(vendor/)?pkg/$GOOS_$GOARCH(_)?"
		// into "$GBROOT/pkg/$GOOS-$GOARCH(-)?".
		// Note: gb doesn't use vendor/pkg.
		if gbrel, err := filepath.Rel(i.gbroot, res); err == nil {
			gbrel = filepath.ToSlash(gbrel)
			gbrel, _ = match(gbrel, "vendor/")
			if gbrel, ok := match(gbrel, fmt.Sprintf("pkg/%s_%s", i.ctx.GOOS, i.ctx.GOARCH)); ok {
				gbrel, hasSuffix := match(gbrel, "_")

				// Reassemble into result.
				if hasSuffix {
					gbrel = "-" + gbrel
				}
				gbrel = fmt.Sprintf("pkg/%s-%s/", i.ctx.GOOS, i.ctx.GOARCH) + gbrel
				gbrel = filepath.FromSlash(gbrel)
				res = filepath.Join(i.gbroot, gbrel)
			}
		}
	}
	return res
}

func match(s, prefix string) (string, bool) {
	rest := strings.TrimPrefix(s, prefix)
	return rest, len(rest) < len(s)
}

// TODO: remove
//
// GetGbProjectPaths checks whether we'are in a gb project and returns
// gbroot and gbvendor
func GetGbProjectPaths(ctx *PackedContext, filename string) (string, string) {
	slashed := filepath.ToSlash(filename)
	i := strings.LastIndex(slashed, "/vendor/src/")
	if i < 0 {
		i = strings.LastIndex(slashed, "/src/")
	}
	if i > 0 {
		gbroot := filepath.FromSlash(slashed[:i])
		gbvendor := filepath.Join(gbroot, "vendor")

		paths := filepath.SplitList(ctx.GOPATH)
		if len(paths) == 0 {
			return "", ""
		}

		// If there is a slash at end of GOROOT or GOPATH, we'll
		// consider this file is inside a gb project wrongly.
		if trimmedGoroot := strings.TrimRight(ctx.GOROOT, "\\/"); SamePath(gbroot, trimmedGoroot) {
			return "", ""
		}
		for _, path := range paths {
			trimmed := strings.TrimRight(path, "\\/")
			if SamePath(trimmed, gbroot) || SamePath(trimmed, gbvendor) {
				return "", ""
			}
		}

		return gbroot, gbvendor
	}

	return "", ""
}

// TODO: is there a better name for this? It's more a dir/pkg fingerprint
type dirCacheEntry struct {
	path      string
	hash      uint64
	version   uint64         // incremented each time the dir changes
	ctime     atomicUnixTime // time last checked
	createdAt time.Time
	// mu        sync.Mutex // Locked when rehashing
}

func (e *dirCacheEntry) Hash() uint64 {
	return atomic.LoadUint64(&e.hash)
}

func (e *dirCacheEntry) Version() uint64 {
	return atomic.LoadUint64(&e.version)
}

var dirCacheGroup singleflight.Group

func (e *dirCacheEntry) Check() error {
	if e.ctime.Since() <= GoPkgCacheTTL {
		return nil
	}
	_, err, _ := dirCacheGroup.Do(e.path, func() (interface{}, error) {
		// Re-check condition
		if e.ctime.Since() <= GoPkgCacheTTL {
			return nil, nil
		}

		hash, err := hashGoPkg(e.path)
		if err != nil {
			return nil, err
		}
		e.ctime.Set(time.Now())

		if x := e.Hash(); x != hash {
			if atomic.CompareAndSwapUint64(&e.hash, x, hash) {
				atomic.AddUint64(&e.version, 1)
			}
		}
		return nil, nil
	})
	return err
}

func newDirCacheEntry(path string) (*dirCacheEntry, error) {
	hash, err := hashGoPkg(path)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &dirCacheEntry{
		path:      path,
		hash:      hash,
		version:   1,
		ctime:     newAtomicUnixTime(now),
		createdAt: now,
	}
	return e, nil
}

var _globalImporterCache = lru.New(32)

func getImporterCache(ctxtKey string) *iimporterCache {
	v, ok := _globalImporterCache.Get(ctxtKey)
	if !ok {
		v, _ = _globalImporterCache.GetOrAdd(ctxtKey, new(iimporterCache))
	}
	return v.(*iimporterCache)
}

// TODO: this can be global
type iimporterCache struct {
	once sync.Once
	// filename (.a) => *types.Package
	pkgCache *lru.Cache

	// WARN: this is not used
	// TODO: use this when parsing source pkgs
	// dirname => *dirCachEntry
	dirCache *lru.Cache

	// TODO: we might need to hash the source files to detect changes
	// TODO: remove files from sourceCache when added to the pkgCache
	//
	// Context Key => import path => *types.Package
	mu          sync.Mutex
	sourceCache map[string]map[string]*sourceImportCacheEntry
	fset        *token.FileSet // TODO: do we need to persist this?
}

func (c *iimporterCache) doInit() {
	if c.pkgCache != nil {
		return
	}
	c.pkgCache = lru.New(0) // Unlimited
	c.dirCache = lru.New(0) // TODO: this does not need to be unlimited??
	c.dirCache.OnEvicted = func(key string, _ interface{}) {
		c.removeSrcAll(key)
	}
	c.fset = token.NewFileSet()
	c.sourceCache = make(map[string]map[string]*sourceImportCacheEntry)
}

func (c *iimporterCache) initialize() { c.once.Do(c.doInit) }

func (c *iimporterCache) removePkg(filename string) {
	c.initialize()
	c.pkgCache.Remove(filename)
}

func (c *iimporterCache) addPkg(filename string, ent *importCacheEntry) {
	c.initialize()
	c.pkgCache.Add(filename, ent)
}

func (c *iimporterCache) addSrc(ctxtKey, importPath string, ent *sourceImportCacheEntry) {
	c.initialize()
	c.mu.Lock()
	if m := c.sourceCache[ctxtKey]; m != nil {
		m[importPath] = ent
	} else {
		c.sourceCache[ctxtKey] = map[string]*sourceImportCacheEntry{
			importPath: ent,
		}
	}
	c.mu.Unlock()
}

func (c *iimporterCache) getPkg(filename string) (*importCacheEntry, bool) {
	c.initialize()
	if v, ok := c.pkgCache.Get(filename); ok {
		return v.(*importCacheEntry), true
	}
	return nil, false
}

func (c *iimporterCache) removeSrc(ctxtKey, importPath string) {
	c.initialize()
	c.mu.Lock()
	delete(c.sourceCache[ctxtKey], importPath)
	c.mu.Unlock()
}

// TODO: rename
func (c *iimporterCache) removeSrcAll(importPath string) {

	// TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO
	//
	// Compare versions not timestamps
	//
	// TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO TODO

	now := time.Now()

	if c.mu.TryLock() {
		for key, m := range c.sourceCache {
			delete(m, importPath)
			if len(m) == 0 {
				delete(c.sourceCache, key)
			}
		}
		c.mu.Unlock()
		return
	}

	// Have a goroutine wait until it can acquire the lock then delete
	// the entries (if any).
	go func(importPath string, now time.Time) {
		c.mu.Lock()
		defer c.mu.Unlock()
		for key, m := range c.sourceCache {
			if e := m[importPath]; e != nil && e.ctime.Before(now) {
				delete(m, importPath)
				if len(m) == 0 {
					delete(c.sourceCache, key)
				}
			}
		}
	}(importPath, now)
}

// func (c *iimporterCache) removeSrcAll_X(importPath string) {
// 	if c.mu.TryLock() {
// 		for _, m := range c.sourceCache {
// 			delete(m, importPath)
// 		}
// 		c.mu.Unlock()
// 	} else {
// 		// Assume our caller is holding the lock, but to be safe only read
// 		var x map[*sourceImportCacheEntry]bool
// 		for _, m := range c.sourceCache {
// 			if e := m[importPath]; e != nil {
// 				if x == nil {
// 					x = make(map[*sourceImportCacheEntry]bool)
// 				}
// 				x[e] = true
// 			}
// 		}
// 		if len(x) != 0 {
// 			go func() {
// 				c.mu.Lock()
// 				defer c.mu.Unlock()
// 				for _, m := range c.sourceCache {
// 					if x[m[importPath]] {
// 						delete(m, importPath)
// 					}
// 				}
// 			}()
// 		}
// 	}
// }

func (c *iimporterCache) getSrc(ctxtKey, importPath string) (*sourceImportCacheEntry, bool) {
	c.initialize()
	c.mu.Lock()
	ent, ok := c.sourceCache[ctxtKey][importPath]
	c.mu.Unlock()
	return ent, ok
}

// func (c *iimporterCache) get(filename, path string) (*importCacheEntry, bool) {
// 	c.initialize()
// 	if v, ok := c.pkgCache.Get(filename); ok {
// 		return v.(*importCacheEntry), true
// 	}
// 	c.mu.Lock()
// 	c.mu.Unlock()
// 	return nil, false
// }

// TOOD: use a unified key and one cache
// type packageCacheKey struct {
// 	filename   string
// 	importPath string
// 	contextKey string
// }

type iimporter struct {
	ctxt        *build.Context
	ctxtKeyOnce sync.Once
	ctxtKey     string
	ii          *iimporterCache

	logf func(string, ...interface{})

	// WARN: remove if not used
	// TODO: attempt to build the package
	FallbackToSource bool
}

func noopLogger(_ string, _ ...interface{}) {}

// WARN: do not allow Context.Dir to be set !!!
// WARN: don't return *iimporter
// types.ImporterFrom
func newIImporter(ctxt *build.Context, logger func(string, ...interface{})) *iimporter {
	if ctxt == nil {
		orig := build.Default
		ctxt = &orig
	}
	// WARN: we should only do this in one place
	if ctxt.HasSubdir == nil {
		ctxt.HasSubdir = contextutil.HasSubdirFunc(ctxt)
	}
	if logger == nil {
		logger = noopLogger
	}
	// TODO: can we make this lazy ???
	key := ContextCacheKey(ctxt)
	return &iimporter{
		ctxt:    ctxt,
		ctxtKey: key,
		ii:      getImporterCache(key),
		logf:    logger,
	}
}

// TODO: clean this up
func NewIImporter(ctxt *build.Context, logger func(string, ...interface{})) types.ImporterFrom {
	return newIImporter(ctxt, logger)
}

func (m *iimporter) ContextCacheKey() string {
	m.ctxtKeyOnce.Do(func() {
		if m.ctxtKey == "" {
			m.ctxtKey = ContextCacheKey(m.ctxt)
		}
	})
	return m.ctxtKey
}

func (m *iimporter) addPkg(filename, importPath string, ent *importCacheEntry) {
	m.ii.addPkg(filename, ent)
	m.ii.removeSrc(m.ContextCacheKey(), importPath)
}

// func (m *iimporter) get(filename, importPath string) (*types.Package, bool) {
// 	ent, ok := m.ii.getPkg(filename)
// 	if !ok {
// 		ent, ok = m.ii.getSrc(m.ContextCacheKey(), importPath)
// 	}
// 	if !ok || ent == nil {
// 		return nil, false
// 	}
// 	// fi, err := os.Stat(name)
// 	return nil, false
// }

// WARN WARN WARN WARN WARN
type sourceImportCacheEntry struct {
	pkg        *types.Package
	importPath string         // Import path
	dir        string         // package file path
	ctime      time.Time      // created at
	utime      atomicUnixTime // last time the hash was updated
	hash       uint64
	fset       *token.FileSet // WARN: I don't think we need to persist this
}

func (e *sourceImportCacheEntry) updateHash() bool {
	hash, err := hashGoPkg(e.dir)
	if err == nil && hash == e.hash {
		e.utime.Set(time.Now())
		return true
	}
	return false
}

// type hashReader struct {
// 	file        *os.File
// 	hash        maphash.Hash
// 	eof         bool
// 	initialized bool
// }

// func (h *hashReader) Sum() (uint64, error) {
// 	if !h.eof {
// 		if _, err := io.Copy(io.Discard, h); err != nil {
// 			return 0, err
// 		}
// 		h.eof = true
// 	}
// 	return h.Sum()
// }

// func (h *hashReader) Read(p []byte) (int, error) {
// 	if !h.initialized {
// 		h.hash.SetSeed(sourceHashSeed)
// 		h.initialized = true
// 	}
// 	n, err := h.file.Read(p)
// 	if n > 0 {
// 		h.hash.Write(p[:n])
// 	}
// 	if err != nil && err == io.EOF {
// 		h.eof = true
// 	}
// 	return n, err
// }

// func readBuildID_NEW(filename string) (*os.File, string, error) {
// 	f, err := os.Open(filename)
// 	if err != nil {
// 		return nil, "", err
// 	}
// 	id, err := buildid.Read(filename, f)
// 	if err == nil {
// 		if _, err := f.Seek(0, 0); err != nil {
//
// 		}
// 	}
// 	if err != nil {
// 		f.Close()
// 		return nil, "", err
// 	}
// 	// Fallback to hashing the file
// 	u, err := hashFile(filename)
// 	if err != nil {
// 		return "", err
// 	}
// 	return "maphash." + strconv.FormatUint(u, 10), nil
// }

func (m *iimporter) importBinary(importPath, pkgFile, resolvedPkgPath string) (*types.Package, error) {
	f, err := os.Open(pkgFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	ent, ok := m.ii.getPkg(pkgFile)
	if ok {
		if ent.size == fi.Size() {
			// The modification time of these files changes frequently and the
			// files themselves can be large so avoid constantly rehashing them
			// if the size does not change.
			if ent.mtime.Equal(fi.ModTime()) || time.Since(ent.htime.Time()) <= ArchiveFileTTL {
				m.logf("cache: using cached gcexportdata file (TTL): %s", pkgFile)
				return ent.pkg, nil
			}
		}
	}

	id, err := buildid.Read(pkgFile, f)
	if err != nil {
		return nil, err // WARN
	}

	if ent != nil && ent.size == fi.Size() && ent.buildID == id {
		ent.mtime.Set(fi.ModTime())
		ent.htime.Set(time.Now())
		m.logf("cache: using cached gcexportdata file (same build id): %s", pkgFile)
		return ent.pkg, nil
	}

	// Reset read position
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	in, err := gcexportdata.NewReader(f)
	if err != nil {
		return nil, err
	}

	// WARN: should we use the importer's fset or the one we just created ???
	fset := token.NewFileSet()

	// WARN: which fset to use?
	// pkg, err := gcexportdata.Read(in, i.fset, make(map[string]*types.Package), path)
	pkg, err := gcexportdata.Read(in, fset, make(map[string]*types.Package), resolvedPkgPath)
	if err != nil {
		return nil, err
	}

	// TODO: use the archive file as the cache key ???
	m.logf("cache: caching gcexportdata file: %s", pkgFile)
	m.addPkg(pkgFile, resolvedPkgPath, &importCacheEntry{
		pkg:        pkg,
		importPath: importPath,
		mtime:      newAtomicUnixTime(fi.ModTime()),
		htime:      newAtomicUnixTime(time.Now()),
		size:       fi.Size(),
		buildID:    id,
		fset:       fset,
	})

	return pkg, nil
}

func (m *iimporter) importSource(importPath, srcDir, resolvedImportPath string) (*types.Package, error) {
	if ent, ok := m.ii.getSrc(m.ContextCacheKey(), resolvedImportPath); ok {
		if time.Since(ent.utime.Time()) < time.Minute {
			m.logf("cache: using cached source package (TTL): %q => %q", importPath, resolvedImportPath)
			return ent.pkg, nil
		}
		if ent.updateHash() {
			m.logf("cache: using cached source package (hash): %q => %q", importPath, resolvedImportPath)
			return ent.pkg, nil
		}
		m.ii.removeSrc(m.ContextCacheKey(), resolvedImportPath)
	}

	// Dir must be set otherwise build.Import() uses the current working
	// directory.
	ctxt := *m.ctxt
	ctxt.Dir = srcDir

	// WARN: use this fset or the iimporter's ???
	fset := token.NewFileSet()
	pkg, err := srcimporter.New(&ctxt, fset, make(map[string]*types.Package)).
		ImportFrom(resolvedImportPath, srcDir, 0)
	if err != nil {
		return nil, err
	}

	// TODO: we only use Import() to get the package dir here
	bp, err := ctxt.Import(resolvedImportPath, srcDir, build.FindOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to import parsed package %q: %w", resolvedImportPath, err)
	}
	hash, err := hashGoPkg(bp.Dir)
	if err != nil {
		return nil, err
	}

	m.logf("cache: caching source package: %q => %q", importPath, resolvedImportPath)
	m.ii.addSrc(m.ContextCacheKey(), resolvedImportPath, &sourceImportCacheEntry{
		pkg:        pkg,
		importPath: importPath,
		dir:        bp.Dir,
		ctime:      time.Now(), // WARN: remove if not used
		utime:      newAtomicUnixTime(time.Now()),
		hash:       hash,
		fset:       fset,
	})
	return pkg, nil
}

func (m *iimporter) ImportFrom(importPath, srcDir string, mode types.ImportMode) (*types.Package, error) {
	m.logf("cache: importing: %q, srcdir: %q", importPath, srcDir)

	filename, path := FindPkg(m.ctxt, importPath, srcDir)
	if path == "" {
		return nil, fmt.Errorf("cache: cannot find package %q in: %q", importPath, srcDir)
	}
	if filename != "" {
		m.logf("cache: found gcexportdata file: %s", filename)
		// TODO: check for os.PathError here!
		if pkg, err := m.importBinary(importPath, filename, path); err == nil {
			return pkg, nil
		} else {
			m.logf("cache: error importing gcexportdata for %s: %v", filename, err)
		}
		m.ii.removePkg(filename)
	} else {
		m.logf("cache: no gcexportdata file for %s", path)
	}

	pkg, err := m.importSource(importPath, srcDir, path)
	if err != nil {
		m.logf("cache: error importing source for %s: %v", importPath, err)
		m.ii.removeSrc(m.ContextCacheKey(), path)
		return nil, err
	}
	return pkg, nil
}

func (m *iimporter) Import(importPath string) (*types.Package, error) {
	return m.ImportFrom(importPath, "", 0)
}

// TODO: on Windows we should just use os.ReadDir since the Info of the
// returned DirEntrys is populated.
func hashGoPkg(dirname string) (uint64, error) {
	names, err := readdirnames(dirname)
	if err != nil {
		return 0, err
	}
	if len(names) > 0 {
		a := names[:0]
		for _, s := range names {
			switch filepath.Ext(s) {
			case ".go":
				// Skip test files since we only care about the exported
				// package API.
				if !strings.HasSuffix(s, "_test.go") {
					a = append(a, s)
				}
			case ".F", ".c", ".cc", ".cpp", ".cxx", ".f", ".f90", ".for", ".h",
				".hh", ".hpp", ".hxx", ".m", ".s", ".swig", ".swigcxx", ".syso":
				a = append(a, s)
			}
		}
		names = a
	}
	if len(names) == 0 {
		return 0, nil // WARN: should we return NoGoError or something?
	}

	fis := make([]os.FileInfo, len(names))
	// TODO: OSes other than macOS benefit from more workers.
	n := len(names)/2 + 1
	var wg sync.WaitGroup
	for i := 0; i < len(names); i += n {
		j := i + n
		if j > len(names) {
			j = len(names)
		}
		wg.Add(1)
		go func(names []string, fis []fs.FileInfo) {
			defer wg.Done()
			for i, name := range names {
				fi, _ := os.Stat(dirname + string(os.PathSeparator) + name)
				if fi != nil && !fi.IsDir() {
					fis[i] = fi
				}
			}
		}(names[i:j], fis[i:j])
	}
	wg.Wait()

	sort.Slice(fis, func(i, j int) bool {
		fi1 := fis[i]
		fi2 := fis[j]
		return fi1 == nil || (fi2 != nil && fi1.Name() < fi2.Name())
	})

	var h maphash.Hash
	h.SetSeed(hashSeed)
	buf := make([]byte, 8)
	for _, fi := range fis {
		if fi == nil {
			continue
		}
		_, _ = h.WriteString(fi.Name())
		binary.LittleEndian.PutUint64(buf, uint64(fi.Size()))
		_, _ = h.Write(buf)
		binary.LittleEndian.PutUint64(buf, uint64(fi.ModTime().UnixNano()))
		_, _ = h.Write(buf)
		_ = h.WriteByte('|')
	}
	return h.Sum64(), nil
}

// Source hashing
/*

func HashGoFiles(dirname string) (uint64, error) {
	des, err := os.ReadDir(dirname)
	if err != nil {
		return 0, err
	}
	var first error
	var h maphash.Hash
	h.SetSeed(hashSeed)
	buf := make([]byte, 8)
	for _, d := range des {
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fi, err := d.Info()
		if err != nil {
			if first == nil && !os.IsNotExist(err) {
				first = err
			}
			continue
		}
		h.WriteString(name)
		binary.LittleEndian.PutUint64(buf, uint64(fi.Size()))
		binary.LittleEndian.PutUint64(buf, uint64(fi.ModTime().UnixNano()))
		h.WriteByte('|')
	}
	return h.Sum64(), first
}
*/

// var iicache sync.Map

// func ImporterForContext(ctxt *build.Context) types.ImporterFrom {
// 	return nil
// }
