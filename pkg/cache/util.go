package cache

import (
	"bytes"
	"hash/maphash"
	"io"
	"io/fs"
	"os"
	"sync"
)

var hashSeed = maphash.MakeSeed()

func hashData(b []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	_, _ = h.Write(b)
	return h.Sum64()
}

var hashBufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func hashFile(name string) (uint64, error) {
	f, err := os.Open(name)
	if err != nil {
		return 0, err
	}
	var h maphash.Hash
	h.SetSeed(hashSeed)
	p := hashBufferPool.Get().(*[]byte)
	_, err = io.CopyBuffer(&h, f, *p)
	f.Close()
	hashBufferPool.Put(p)
	return h.Sum64(), err
}

func readFile(name string) ([]byte, fs.FileInfo, error) {
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
	size++ // one byte for final read at EOF

	// If a file claims a small size, read at least 512 bytes.
	// In particular, files in Linux's /proc claim size 0 but
	// then do not work right if read in small pieces,
	// so an initial read of 1 byte would not work correctly.
	if size < 512 {
		size = 512
	}

	data := make([]byte, 0, size)
	for {
		if len(data) >= cap(data) {
			d := append(data[:cap(data)], 0)
			data = d[:len(data)]
		}
		n, err := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, info, err
		}
	}
}

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
	_, err = buf.ReadFrom(f)
	return buf, info, err
}
