package gocode

import (
	"go/build"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

const g_debug = false

type Candidate struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class"`
}

func (c Candidate) String() string {
	b := make([]byte, len(c.Name)+len(c.Type)+len(c.Class)+2)
	n := copy(b, c.Class)
	n += copy(b[n:], []byte{' '})
	n += copy(b[n:], c.Name)
	switch c.Class {
	case "func":
		if strings.HasPrefix(c.Type, "func") {
			n += copy(b[n:], c.Type[len("func"):])
		}
		return string(b[:n])
	case "package":
		return string(b[:n])
	default:
		if c.Type != "" {
			n += copy(b[n:], []byte{' '})
			n += copy(b[n:], c.Type)
		}
		return string(b[:n])
	}
}

type Config struct {
	GOROOT        string
	GOPATH        string
	GOOS          string
	InstallSuffix string
	AutoBuild     bool
	Builtins      bool // propose builtin functions
	daemon        *daemon
	state         uint32
}

func (c *Config) lazyInit() {
	if atomic.CompareAndSwapUint32(&c.state, 0, 1) {
		if c.daemon != nil {
			panic("gocode.Config: unexpected init state")
		}
		c.daemon = newDaemon()
	}
}

func (c *Config) Complete(file []byte, name string, cursor int) []Candidate {
	c.lazyInit()
	return c.daemon.complete(file, name, cursor, c)
}

type daemon struct {
	autocomplete *auto_complete_context
	declcache    *decl_cache
	pkgcache     package_cache
	context      package_lookup_context
	mu           sync.Mutex
}

func newDaemon() *daemon {
	ctxt := build.Default
	ctxt.GOPATH = os.Getenv("GOPATH")
	ctxt.GOROOT = runtime.GOROOT()
	d := &daemon{
		context:  package_lookup_context{Context: ctxt},
		pkgcache: new_package_cache(),
	}
	d.declcache = new_decl_cache(&d.context)
	d.autocomplete = new_auto_complete_context(d.pkgcache, d.declcache, d)
	return d
}

var NoCandidates = []Candidate{}

func (d *daemon) complete(file []byte, name string, cursor int, conf *Config) (res []Candidate) {
	defer func() {
		if e := recover(); e != nil {
			if g_debug {
				log.Printf("gocode: panic (%+v)\n", e)
			}
			if len(res) == 0 {
				res = NoCandidates
			}
		}
	}()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.update(conf)
	list, _ := d.autocomplete.apropos(file, name, cursor)
	if list == nil || len(list) == 0 {
		return NoCandidates
	}
	res = make([]Candidate, len(list))
	for i, c := range list {
		res[i] = Candidate{
			Name:  c.Name,
			Type:  c.Type,
			Class: c.Class.String(),
		}
	}
	return res
}

func (d *daemon) update(conf *Config) {
	g_config.SetProposeBuiltins(conf.Builtins)
	g_config.SetAutoBuild(conf.AutoBuild)
	if !d.same(conf) {
		d.context.GOPATH = conf.GOPATH
		d.context.GOROOT = conf.GOROOT
		if conf.GOOS != "" {
			d.context.GOOS = conf.GOOS
		}
		d.context.InstallSuffix = conf.InstallSuffix
		d.pkgcache = new_package_cache()
		d.declcache = new_decl_cache(&d.context)
		d.autocomplete = new_auto_complete_context(d.pkgcache, d.declcache, d)

		g_config.SetLibPath(d.libPath()) // global config
	}
}

func (d *daemon) same(conf *Config) bool {
	return d.context.GOPATH == conf.GOPATH &&
		d.context.GOROOT == conf.GOROOT &&
		d.context.GOOS == conf.GOOS &&
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
