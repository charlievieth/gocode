package gocode

import (
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var g_debug = false

type Candidate struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class"`
}

type Config struct {
	GOROOT        string
	GOPATH        string
	InstallSuffix string
	Builtins      bool // propose builtin functions
}

var gocodeDaemon = newDaemon()

type daemon struct {
	autocomplete *auto_complete_context
	declcache    *decl_cache
	pkgcache     package_cache
	context      build.Context
	mu           sync.Mutex // daemon mutex
}

func newDaemon() *daemon {
	ctx := build.Default
	ctx.GOPATH = os.Getenv("GOPATH")
	ctx.GOROOT = runtime.GOROOT()
	d := daemon{
		context:   ctx,
		pkgcache:  new_package_cache(),
		declcache: new_decl_cache(ctx),
		mu:        sync.Mutex{},
	}
	d.autocomplete = new_auto_complete_context(d.pkgcache, d.declcache)
	return &d
}

var NoCandidates = []Candidate{}

func (d *daemon) complete(file []byte, name string, cursor int, conf *Config) []Candidate {
	// TODO: Should conf be a val or ptr ???
	d.update(conf)
	list, _ := d.autocomplete.apropos(file, name, cursor)
	if list == nil {
		return NoCandidates
	}
	res := make([]Candidate, len(list))
	for i, c := range list {
		res[i] = Candidate{
			Name:  c.Name,
			Type:  c.Type,
			Class: c.Class.String(),
		}
	}
	return res
}

func (d *daemon) Xupdate(conf *Config) {
	if d.same(conf) {
		if g_config.ProposeBuiltins != conf.Builtins {
			d.mu.Lock()
			if g_config.ProposeBuiltins != conf.Builtins {
				g_config.ProposeBuiltins = conf.Builtins
			}
			d.mu.Unlock()
		}
		return
	}
	d.mu.Lock()
	if !d.same(conf) {
		d.context.GOPATH = conf.GOPATH
		d.context.GOROOT = conf.GOROOT
		d.context.InstallSuffix = conf.InstallSuffix
		d.pkgcache = new_package_cache()
		d.declcache = new_decl_cache(d.context)
		d.autocomplete = new_auto_complete_context(d.pkgcache, d.declcache)

		g_config.LibPath = d.libPath() // global config
		g_config.ProposeBuiltins = conf.Builtins
	}
	d.mu.Unlock()
}

func (d *daemon) update(conf *Config) {
	if d.same(conf) {
		if g_config.ProposeBuiltins != conf.Builtins {
			d.mu.Lock()
			if g_config.ProposeBuiltins != conf.Builtins {
				g_config.ProposeBuiltins = conf.Builtins
			}
			d.mu.Unlock()
		}
	} else {
		d.mu.Lock()
		if !d.same(conf) {
			d.context.GOPATH = conf.GOPATH
			d.context.GOROOT = conf.GOROOT
			d.context.InstallSuffix = conf.InstallSuffix
			d.pkgcache = new_package_cache()
			d.declcache = new_decl_cache(d.context)
			d.autocomplete = new_auto_complete_context(d.pkgcache, d.declcache)

			g_config.LibPath = d.libPath() // global config
			g_config.ProposeBuiltins = conf.Builtins
		}
		d.mu.Unlock()
	}
}

func (d *daemon) same(conf *Config) bool {
	return d.context.GOPATH == conf.GOPATH &&
		d.context.GOROOT == conf.GOROOT &&
		d.context.InstallSuffix == conf.InstallSuffix
}

// libPath, returns the OS and Arch specific pkg paths for the current GOROOT
// and GOPATH.
func (d *daemon) libPath() string {
	var all []string
	pkg := d.pkgDir()
	if d.context.GOROOT != "" {
		all = append(all, filepath.Join(d.context.GOROOT, pkg))
	}
	if d.context.GOPATH != "" {
		all = append(all, d.pkgpaths(pkg)...)
	}
	return strings.Join(all, string(filepath.Separator))
}

// pkgpaths, returns all GOPATH pkg paths for Arch arch.
func (d *daemon) pkgpaths(arch string) []string {
	paths := filepath.SplitList(d.context.GOPATH)
	n := 0
	for _, p := range paths {
		if p != d.context.GOROOT && p != "" && p[0] != '~' {
			paths[n] = filepath.Join(p, arch)
			n++
		}
	}
	return paths[:n]
}

// osArch returns the os and arch specific package directory
func (d *daemon) pkgDir() string {
	var s string
	if d.context.InstallSuffix == "" {
		s = d.context.GOOS + "_" + d.context.GOARCH
	} else {
		s = d.context.GOOS + "_" + d.context.GOARCH + "_" + d.context.InstallSuffix
	}
	return filepath.Join("pkg", s)
}
