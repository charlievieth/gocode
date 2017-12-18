package gocode

import (
	"bytes"
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charlievieth/gocode/fs"
	"github.com/golang/groupcache/lru"
)

type DirEntry struct {
	modTime time.Time
	names   []string
}

type DirCache struct {
	cache *lru.Cache
	mu    sync.Mutex
}

func NewDirCache() *DirCache {
	return &DirCache{
		cache: lru.New(200),
	}
}

func (c *DirCache) readdirnames(path string, fi os.FileInfo) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if fi == nil {
		fi, err = f.Stat()
		if err != nil {
			return nil, err
		}
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	c.cache.Add(path, DirEntry{
		modTime: fi.ModTime(),
		names:   names,
	})
	return names, nil
}

func (c *DirCache) Readdirnames(path string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, found := c.cache.Get(path)
	if !found {
		return c.readdirnames(path, nil)
	}
	d, ok := v.(DirEntry)
	if !ok {
		// This should not happen
		if found {
			c.cache.Remove(path)
		}
		return c.readdirnames(path, nil)
	}
	fi, err := fs.Stat(path)
	if err != nil {
		if found {
			c.cache.Remove(path)
		}
		return nil, err
	}
	if fi.ModTime().After(d.modTime) {
		names, err := c.readdirnames(path, fi)
		if err != nil {
			c.cache.Remove(path)
		}
		return names, err
	}
	return d.names, nil
}

var dir_cache = NewDirCache()

func has_go_ext(s string) bool {
	return len(s) >= len("*.go") && s[len(s)-len(".go"):] == ".go"
}

func readdirnames(name string) ([]string, error) {
	return dir_cache.Readdirnames(name)
}

// our own readdir, which skips the files it cannot lstat
func readdir_lstat(name string) ([]os.FileInfo, error) {
	names, err := readdirnames(name)
	if err != nil {
		return nil, err
	}

	out := make([]os.FileInfo, 0, len(names))
	for _, lname := range names {
		s, err := fs.Lstat(name + string(filepath.Separator) + lname)
		if err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func readdir_gofiles_lstat(name string) ([]os.FileInfo, error) {
	names, err := readdirnames(name)
	if err != nil {
		return nil, err
	}

	n := len(names)
	if n > 64 {
		n = 64
	}

	out := make([]os.FileInfo, 0, n)
	for _, lname := range names {
		if has_go_ext(lname) {
			s, err := fs.Lstat(name + string(filepath.Separator) + lname)
			if err == nil {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// our other readdir function, only opens and reads
func readdir(dirname string) []os.FileInfo {
	f, err := os.Open(dirname)
	if err != nil {
		return nil
	}
	fi, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		panic(err)
	}
	return fi
}

// returns truncated 'data' and amount of bytes skipped (for cursor pos adjustment)
func filter_out_shebang(data []byte) ([]byte, int) {
	if len(data) > 2 && data[0] == '#' && data[1] == '!' {
		newline := bytes.IndexByte(data, '\n')
		if newline != -1 && len(data) > newline+1 {
			return data[newline+1:], newline + 1
		}
	}
	return data, 0
}

func file_exists(filename string) bool {
	_, err := fs.Stat(filename)
	return err == nil
}

func is_dir(path string) bool {
	fi, err := fs.Stat(path)
	return err == nil && fi.IsDir()
}

func char_to_byte_offset(s []byte, offset_c int) (offset_b int) {
	for offset_b = 0; offset_c > 0 && offset_b < len(s); offset_b++ {
		if utf8.RuneStart(s[offset_b]) {
			offset_c--
		}
	}
	return offset_b
}

func xdg_home_dir() string {
	xdghome := os.Getenv("XDG_CONFIG_HOME")
	if xdghome == "" {
		xdghome = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return xdghome
}

func has_prefix(s, prefix string, ignorecase bool) bool {
	if ignorecase {
		s = strings.ToLower(s)
		prefix = strings.ToLower(prefix)
	}
	return strings.HasPrefix(s, prefix)
}

func find_bzl_project_root(libpath, path string) (string, error) {
	if libpath == "" {
		return "", fmt.Errorf("could not find project root, libpath is empty")
	}

	pathMap := map[string]struct{}{}
	for _, lp := range strings.Split(libpath, ":") {
		lp := strings.TrimSpace(lp)
		pathMap[filepath.Clean(lp)] = struct{}{}
	}

	path = filepath.Dir(path)
	if path == "" {
		return "", fmt.Errorf("project root is blank")
	}

	start := path
	for path != "/" {
		if _, ok := pathMap[filepath.Clean(path)]; ok {
			return path, nil
		}
		path = filepath.Dir(path)
	}
	return "", fmt.Errorf("could not find project root in %q or its parents", start)
}

// Code taken directly from `gb`, I hope author doesn't mind.
func find_gb_project_root(path string) (string, error) {
	path = filepath.Dir(path)
	if path == "" {
		return "", fmt.Errorf("project root is blank")
	}
	start := path
	for path != "/" {
		root := filepath.Join(path, "src")
		if _, err := fs.Stat(root); err != nil {
			if os.IsNotExist(err) {
				path = filepath.Dir(path)
				continue
			}
			return "", err
		}
		path, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("could not find project root in %q or its parents", start)
}

// vendorlessImportPath returns the devendorized version of the provided import path.
// e.g. "foo/bar/vendor/a/b" => "a/b"
func vendorlessImportPath(ipath string, currentPackagePath string) (string, bool) {
	if strings.Contains(ipath, "vendor/") {
		split := strings.Split(ipath, "vendor/")
		// no vendor in path
		if len(split) == 1 {
			return ipath, true
		}
		// this import path does not belong to the current package
		if currentPackagePath != "" && !strings.Contains(currentPackagePath, split[0]) {
			return "", false
		}
		// Devendorize for use in import statement.
		if i := strings.LastIndex(ipath, "/vendor/"); i >= 0 {
			return ipath[i+len("/vendor/"):], true
		}
		if strings.HasPrefix(ipath, "vendor/") {
			return ipath[len("vendor/"):], true
		}
	}
	return ipath, true
}

//-------------------------------------------------------------------------
// print_backtrace
//
// a nicer backtrace printer than the default one
//-------------------------------------------------------------------------

var g_backtrace_mutex sync.Mutex

func print_backtrace(err interface{}) {
	g_backtrace_mutex.Lock()
	defer g_backtrace_mutex.Unlock()
	fmt.Printf("panic: %v\n", err)
	i := 2
	for {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		f := runtime.FuncForPC(pc)
		fmt.Printf("%d(%s): %s:%d\n", i-1, f.Name(), file, line)
		i++
	}
	fmt.Println("")
}

//-------------------------------------------------------------------------
// File reader goroutine
//
// It's a bad idea to block multiple goroutines on file I/O. Creates many
// threads which fight for HDD. Therefore only single goroutine should read HDD
// at the same time.
//-------------------------------------------------------------------------

type file_reader_type struct {
	gate chan struct{}
}

func new_file_reader() *file_reader_type {
	return &file_reader_type{gate: make(chan struct{}, 50)}
}

func (r *file_reader_type) read_file(filename string) ([]byte, error) {
	r.gate <- struct{}{}
	b, err := ioutil.ReadFile(filename)
	<-r.gate
	return b, err
}

var file_reader = new_file_reader()

//-------------------------------------------------------------------------
// copy of the build.Context without func fields
//-------------------------------------------------------------------------

type go_build_context struct {
	GOARCH        string
	GOOS          string
	GOROOT        string
	GOPATH        string
	CgoEnabled    bool
	UseAllFiles   bool
	Compiler      string
	BuildTags     []string
	ReleaseTags   []string
	InstallSuffix string
}

func pack_build_context(ctx *build.Context) go_build_context {
	return go_build_context{
		GOARCH:        ctx.GOARCH,
		GOOS:          ctx.GOOS,
		GOROOT:        ctx.GOROOT,
		GOPATH:        ctx.GOPATH,
		CgoEnabled:    ctx.CgoEnabled,
		UseAllFiles:   ctx.UseAllFiles,
		Compiler:      ctx.Compiler,
		BuildTags:     ctx.BuildTags,
		ReleaseTags:   ctx.ReleaseTags,
		InstallSuffix: ctx.InstallSuffix,
	}
}

func unpack_build_context(ctx *go_build_context) package_lookup_context {
	return package_lookup_context{
		Context: build.Context{
			GOARCH:        ctx.GOARCH,
			GOOS:          ctx.GOOS,
			GOROOT:        ctx.GOROOT,
			GOPATH:        ctx.GOPATH,
			CgoEnabled:    ctx.CgoEnabled,
			UseAllFiles:   ctx.UseAllFiles,
			Compiler:      ctx.Compiler,
			BuildTags:     ctx.BuildTags,
			ReleaseTags:   ctx.ReleaseTags,
			InstallSuffix: ctx.InstallSuffix,
		},
	}
}
