package cache

import (
	"bytes"
	"fmt"
	"hash/maphash"
	"io/fs"
	"os"
	"strings"
	"sync"
)

var hashSeed = maphash.MakeSeed()

func _newBuffer() interface{} { return new(bytes.Buffer) }

var readBufferPool = [...]struct {
	size int
	pool *sync.Pool
}{
	{4 * 1024, &sync.Pool{New: _newBuffer}},
	{8 * 1024, &sync.Pool{New: _newBuffer}},
	{16 * 1024, &sync.Pool{New: _newBuffer}},
	{32 * 1024, &sync.Pool{New: _newBuffer}},
	{64 * 1024, &sync.Pool{New: _newBuffer}},
	{128 * 1024, &sync.Pool{New: _newBuffer}},
	{512 * 1024, &sync.Pool{New: _newBuffer}},
	{1024 * 1024, &sync.Pool{New: _newBuffer}},
}

func getReadBuffer(size int) (b *bytes.Buffer) {
	for i := 0; i < len(readBufferPool); i++ {
		p := &readBufferPool[i]
		if p.size <= size {
			b = p.pool.Get().(*bytes.Buffer)
			break
		}
	}
	if b == nil {
		b = new(bytes.Buffer)
	}
	b.Reset()
	b.Grow(size)
	return b
}

func putReadBuffer(b *bytes.Buffer) {
	if b == nil {
		return
	}
	n := b.Cap()
	if n > 5*1024*1024 {
		return
	}
	for _, p := range &readBufferPool {
		if p.size <= n {
			p.pool.Put(b)
		}
	}
}

func readFileBuffer(name string) (*bytes.Buffer, fs.FileInfo, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var size int
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size64 := info.Size()
	if int64(int(size64)) == size64 {
		size = int(size64)
	}
	size += bytes.MinRead + 1 // one byte + MinRead for final read at EOF

	buf := getReadBuffer(size)
	if _, err := buf.ReadFrom(f); err != nil {
		putReadBuffer(buf)
		return nil, nil, err
	}
	return buf, info, nil
}

func readdirnames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	return names, err
}

func readGoNames(dir string, includeTest bool) ([]string, error) {
	names, err := readdirnames(dir)
	if len(names) > 0 {
		a := names[:0]
		for _, s := range names {
			if strings.HasSuffix(s, ".go") && (includeTest || !strings.HasSuffix(s, "_test.go")) {
				a = append(a, s)
			}
		}
		names = a
	}
	return names, err
}

// MultiErrorBuilder provides a thread-safe way to build a MultiError
type MultiErrorBuilder struct {
	mu  sync.Mutex
	err *MultiError
}

func (e *MultiErrorBuilder) Add(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	if e.err == nil {
		e.err = new(MultiError)
	}
	e.err.Add(err)
	e.mu.Unlock()
}

func (e *MultiErrorBuilder) ToError() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err == nil {
		return nil
	}
	return e.err.ToError()
}

// MultiError is a list of errors.
//
// TODO: don't include all errors.
type MultiError struct {
	err    error
	errors []error
}

func NewMultiError(errs ...error) *MultiError {
	if len(errs) == 0 {
		return &MultiError{}
	}
	return &MultiError{
		err:    errs[0],
		errors: append([]error(nil), errs[1:]...),
	}
}

func (e *MultiError) Empty() bool {
	return e == nil || e.err == nil
}

func (e MultiError) ToError() error {
	if e.Empty() {
		return nil
	}
	// Return a copy
	return &MultiError{
		err:    e.err,
		errors: append([]error(nil), e.errors...),
	}
}

func (e MultiError) Errors() []error {
	if e.Empty() {
		return nil
	}
	result := make([]error, 1+len(e.errors))
	result[0] = e.err
	copy(result[1:], e.errors)
	return result
}

func (e *MultiError) Error() string {
	if e.Empty() {
		return ""
	}
	if len(e.errors) == 0 {
		return e.err.Error()
	}
	// TODO: limit the number of returned errors
	var w strings.Builder
	fmt.Fprintf(&w, "%s (and %d other errors):", e.err, len(e.errors))
	for _, err := range e.errors {
		w.WriteByte('\n')
		w.WriteString(err.Error())
	}
	return w.String()
}

func (e *MultiError) Add(err error) {
	if err == nil {
		return
	}
	if e.err == nil {
		e.err = err
	} else {
		e.errors = append(e.errors, err)
	}
}

// Unwrap unwraps the first error.
func (e MultiError) Unwrap() error { return e.err }
