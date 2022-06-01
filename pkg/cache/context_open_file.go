package cache

import (
	"bytes"
	"go/build"
	"io"
	"os"
	"sync"
)

// readAll is the same as io.ReadAll but starts with a larger buffer
func readAll(r io.Reader) ([]byte, error) {
	b := make([]byte, 0, 8192)
	for {
		if len(b) == cap(b) {
			// Add more capacity (let append pick how much).
			b = append(b, 0)[:len(b)]
		}
		n, err := r.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return b, err
		}
	}
}

func contextReadFile(ctxt *build.Context) func(path string) ([]byte, error) {
	if fn := ctxt.OpenFile; fn != nil {
		return func(path string) ([]byte, error) {
			rc, err := fn(path)
			if err != nil {
				return nil, err
			}
			data, err := readAll(rc)
			rc.Close()
			return data, err
		}
	}
	return os.ReadFile
}

// WARN: not sure this is worth the effort since we can't use it
// and cache files based off of modification time.
//
// ContextOpenFile returns a caching OpenFile function suitable for use with a
// build.Context.
func ContextOpenFile(ctxt *build.Context) func(path string) (io.ReadCloser, error) {
	var cache map[string][]byte
	var mu sync.Mutex
	readFile := contextReadFile(ctxt)
	return func(path string) (io.ReadCloser, error) {
		mu.Lock()
		data, ok := cache[path]
		mu.Unlock()
		if ok {
			return io.NopCloser(bytes.NewReader(data)), nil
		}

		data, err := readFile(path)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		if cache == nil {
			cache = make(map[string][]byte)
		}
		cache[path] = data
		mu.Unlock()
		return io.NopCloser(bytes.NewReader(data)), nil
	}
}
