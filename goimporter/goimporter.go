package goimporter

import (
	"fmt"
	"go/importer"
	"go/types"
	"os"
	"sync"
	"time"

	"golang.org/x/tools/go/gcexportdata"
)

type Package struct {
	Path    string    // import path
	Dirname string    // absolute directory
	Pkgname string    // binary package
	ModTime time.Time // binary package modified time
	Package *types.Package
}

type packageCache struct {
	dir  string
	pkgs map[string]*Package
	mu   sync.RWMutex
}

func newPackageCache(dir string) *packageCache {
	return &packageCache{dir: dir}
}

func (m *packageCache) load(path string) (pkg *Package) {
	m.mu.RLock()
	if m.pkgs != nil {
		pkg = m.pkgs[path]
	}
	m.mu.Unlock()
	return
}

func (m *packageCache) store(pkg *Package) *Package {
	m.mu.Lock()
	if m.pkgs == nil {
		m.pkgs = make(map[string]*Package)
	}
	found, ok := m.pkgs[pkg.Path]
	if !ok || found.ModTime.Before(pkg.ModTime) {
		m.pkgs[pkg.Path] = pkg
	} else {
		pkg = found
	}
	m.mu.Unlock()
	return pkg
}

// TODO: delete if unused
func (m *packageCache) delete(path string) {
	m.mu.Lock()
	if m.pkgs != nil {
		delete(m.pkgs, path)
	}
	m.mu.Unlock()
	return
}

func (m *packageCache) iimport(path string) (*types.Package, error) {
	// FYI: import is reserved - so use iimport
	//
	// TODO: cache global packages together
	//
	// TODO: prevent packages from being loaded more than once
	// due to concurrent calls - that is make it so that caller
	// two and later can wait for the package to be completed
	//
	filename, id := gcexportdata.Find(path, m.dir)
	if filename == "" {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		return nil, fmt.Errorf("can't find import: %q", id)
	}
	fi, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("can't stat package file (%s) for import (%s): %s",
			filename, id, err)
	}
	modtime := fi.ModTime()

	// WARN: do not delete for now - instead replace
	// TODO: instrument cache hits/misses
	if pkg := m.load(path); pkg != nil && pkg.ModTime.Equal(modtime) {
		return pkg.Package, nil
	}

	// TODO: consider reusing importer for packages ???
	imp := importer.Default().(types.ImporterFrom)
	pkg, err := imp.ImportFrom(path, m.dir, 0)
	if err != nil {
		return nil, fmt.Errorf("importing package (%s) from dir (%s): %s",
			path, m.dir, err)
	}

	// store package and return result (in case a newer version was added)
	pkg = m.store(&Package{
		Path:    path,
		Dirname: m.dir,
		Pkgname: pkg.Name(),
		ModTime: modtime,
		Package: pkg,
	}).Package
	return pkg, nil
}

// WARN: the caches may need to be global so that a new importer can be provided
// each time - to avoid weird confilicts
//
// ^^^ This may be totally wrong
type Importer struct {
	// TODO: make stdlib global
	stdLib sync.Map       // read heavy
	stdImp types.Importer // reuse the importer for its internal pkg cache
	pkgs   map[string]*packageCache
	mu     sync.RWMutex // pkg mutex
}

func (m *Importer) loadCache(dir string) *packageCache {
	// TODO: instrument hits/misses
	m.mu.RLock()
	if m.pkgs != nil {
		if p, ok := m.pkgs[dir]; ok {
			m.mu.RUnlock()
			return p
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	if m.pkgs == nil {
		m.pkgs = make(map[string]*packageCache)
	}
	// recheck the cache now that we have the write lock
	p, ok := m.pkgs[dir]
	if !ok {
		p = newPackageCache(dir)
		m.pkgs[dir] = p
	}
	m.mu.Unlock()
	return p
}

func (m *Importer) loadStdLibPkg(path string) (*types.Package, bool) {
	if isStdLibPkg(path) {
		if v, ok := m.stdLib.Load(path); ok {
			return v.(*types.Package), true
		}
		pkg, err := m.stdImp.Import(path)
		if err != nil {
			return nil, false // TODO: handle this
		}
		m.stdLib.Store(path, pkg)
		return pkg, true
	}
	return nil, false
}

func (m *Importer) loadPackage(path, srcDir string) (*types.Package, error) {
	if pkg, ok := m.loadStdLibPkg(path); ok {
		return pkg, nil
	}
	// m.loadCache(dir).iimport(path)
	return nil, nil
}

// TODO: normalize srcDir to the package root -> that is if we're in a project
// with vendored dependencies use the project root!
func (m *Importer) ImportFrom(path, srcDir string, mode types.ImportMode) (*types.Package, error) {
	return nil, nil
}
