package goimporter

import (
	"fmt"
	"go/importer"
	"go/types"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/tools/go/gcexportdata"
)

type entry struct {
	Filename string    // object (.o) or archive (.a) filename
	ModTime  time.Time // binary package modified time
	Package  *types.Package
}

type goimporter struct {
	pkgs     map[string]*entry
	mu       sync.RWMutex
	compiler string
}

func For(compiler string) types.ImporterFrom {
	return &goimporter{
		pkgs:     make(map[string]*entry),
		compiler: compiler,
	}
}

func Default() types.ImporterFrom {
	return For(runtime.Compiler)
}

func (m *goimporter) load(filename string) (*entry, bool) {
	m.mu.RLock()
	pkg, ok := m.pkgs[filename]
	m.mu.RUnlock()
	return pkg, ok
}

func (m *goimporter) store(pkg *entry) *entry {
	m.mu.Lock()
	found, ok := m.pkgs[pkg.Filename]
	if !ok || found.ModTime.Before(pkg.ModTime) {
		m.pkgs[pkg.Filename] = pkg
	} else {
		pkg = found
	}
	m.mu.Unlock()
	return pkg
}

func (m *goimporter) Import(path string) (*types.Package, error) {
	return m.ImportFrom(path, "", 0)
}

func (m *goimporter) ImportFrom(path, srcDir string, mode types.ImportMode) (*types.Package, error) {
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

	// TODO: instrument cache hits/misses
	if pkg, ok := m.load(filename); ok && pkg.ModTime.Equal(modtime) {
		return pkg.Package, nil
	}

	// TODO: consider reusing importer for packages (stdlib) ???
	imp := importer.Default().(types.ImporterFrom)
	bpkg, err := imp.ImportFrom(path, srcDir, 0)
	if err != nil {
		return nil, fmt.Errorf("importing package (%s) from dir (%s): %s",
			path, srcDir, err)
	}

	// store package and return result (in case a newer version was added)
	pkg := m.store(&entry{
		Filename: filename,
		ModTime:  modtime,
		Package:  bpkg,
	})
	return pkg.Package, nil
}
