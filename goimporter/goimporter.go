package goimporter

import (
	"fmt"
	"go/importer"
	"go/types"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/gcexportdata"
)

type Package struct {
	Path     string    // import path
	Filename string    // object (.o) or archive (.a) filename
	ModTime  time.Time // binary package modified time
	Package  *types.Package
}

type stat int64

func (s *stat) inc() { atomic.AddInt64((*int64)(s), 1) }

type Importer struct {
	// TODO: investigate global std lib
	pkgs map[string]*Package
	mu   sync.RWMutex // pkg mutex
}

func (m *Importer) load(filename string) (pkg *Package) {
	m.mu.RLock()
	if m.pkgs != nil {
		pkg = m.pkgs[filename]
	}
	m.mu.RUnlock()
	return
}

func (m *Importer) store(pkg *Package) *Package {
	m.mu.Lock()
	if m.pkgs == nil {
		m.pkgs = make(map[string]*Package)
	}
	found, ok := m.pkgs[pkg.Filename]
	if !ok || found.ModTime.Before(pkg.ModTime) {
		m.pkgs[pkg.Filename] = pkg
	} else {
		pkg = found
	}
	m.mu.Unlock()
	return pkg
}

func (m *Importer) Import(path string) (*types.Package, error) {
	return m.ImportFrom(path, "", 0)
}

func (m *Importer) ImportFrom(path, srcDir string, mode types.ImportMode) (*types.Package, error) {
	// FYI: import is reserved - so use iimport
	//
	// TODO: cache global packages together
	//
	// TODO: prevent packages from being loaded more than once
	// due to concurrent calls - that is make it so that caller
	// two and later can wait for the package to be completed
	//
	filename, id := gcexportdata.Find(path, srcDir)
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
	if pkg := m.load(filename); pkg != nil && pkg.ModTime.Equal(modtime) {
		return pkg.Package, nil
	}

	// TODO: consider reusing importer for packages ???
	imp := importer.Default().(types.ImporterFrom)
	bpkg, err := imp.ImportFrom(path, srcDir, 0)
	if err != nil {
		return nil, fmt.Errorf("importing package (%s) from dir (%s): %s",
			path, srcDir, err)
	}

	// store package and return result (in case a newer version was added)
	pkg := m.store(&Package{
		Path:     path,
		Filename: filename,
		ModTime:  modtime,
		Package:  bpkg,
	})
	return pkg.Package, nil
}
