package gocode

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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

func has_prefix(s, prefix string, ignorecase bool) bool {
	if strings.HasPrefix(s, prefix) {
		return true
	}
	if ignorecase {
		strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
	}
	return false
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
	return &file_reader_type{gate: make(chan struct{}, 100)}
}

func (r *file_reader_type) read_file(filename string) ([]byte, error) {
	r.gate <- struct{}{}
	b, err := ioutil.ReadFile(filename)
	<-r.gate
	return b, err
}

var file_reader = new_file_reader()
