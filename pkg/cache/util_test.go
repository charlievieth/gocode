package cache

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestReadGoNames(t *testing.T) {
	tempdir := t.TempDir()
	for _, name := range []string{"x.txt", "f1.go", "f2.go", "f2_test.go"} {
		path := filepath.Join(tempdir, name)
		if err := os.WriteFile(path, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for includeTest, want := range map[bool][]string{
		true:  {"f1.go", "f2.go", "f2_test.go"},
		false: {"f1.go", "f2.go"},
	} {
		got, err := readGoNames(tempdir, includeTest)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("readGoNames(%t) = %q; want: %q", includeTest, got, want)
		}
	}
}

func TestNewMultiError(t *testing.T) {
	t.Run("One", func(t *testing.T) {
		want := errors.New("foo")
		e := NewMultiError(want)
		assert.ErrorIs(t, e, want)
		assert.Nil(t, e.errors)
		assert.Equal(t, want.Error(), e.Error())
	})

	t.Run("Mutliple", func(t *testing.T) {
		want := make([]error, 4)
		for i := range want {
			want[i] = fmt.Errorf("err_%d", i)
		}
		e := NewMultiError(want...)
		assert.Equal(t, want, e.Errors())

		const errMsg = `err_0 (and 3 other errors):
err_1
err_2
err_3`
		assert.Equal(t, errMsg, e.Error())
	})
}

func TestMultiErrorBuilder(t *testing.T) {
	assert.Nil(t, (*MultiErrorBuilder)(nil).ToError())

	var m MultiErrorBuilder
	var wg sync.WaitGroup
	err := errors.New("error")
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Add(err)
		}()
	}
	wg.Wait()

	merr := m.ToError().(*MultiError)
	assert.Len(t, merr.Errors(), 16)
}

func TestStringInterner(t *testing.T) {
	var x stringInterner
	s1 := "s"
	i1 := x.Intern(s1)
	p1 := (*reflect.StringHeader)(unsafe.Pointer(&i1)).Data
	p2 := (*reflect.StringHeader)(unsafe.Pointer(&s1)).Data
	if p1 == p2 {
		t.Error("Intern did not make a copy of the string")
	}
	i2 := x.Intern(s1)
	p2 = (*reflect.StringHeader)(unsafe.Pointer(&i2)).Data
	if p1 != p2 {
		t.Error("Intern did not return the interned string")
	}
}

func BenchmarkStringInterner(b *testing.B) {
	var strs = [8]string{
		"client",
		"cache",
		"operations",
		"logging",
		"config",
		"models",
		"test",
		"util",
	}

	b.Run("Serial", func(b *testing.B) {
		var x stringInterner
		for i := 0; i < b.N; i++ {
			x.Intern(strs[i%len(strs)])
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		var x stringInterner
		b.RunParallel(func(pb *testing.PB) {
			for i := 0; pb.Next(); i++ {
				x.Intern(strs[i%len(strs)])
			}
		})
	})
}

func xhashFile(name string) (uint64, error) {
	f, err := os.Open(name)
	if err != nil {
		return 0, err
	}
	h := sha1.New()
	p := hashBufferPool.Get().(*[]byte)
	_, err = io.CopyBuffer(h, f, *p)
	f.Close()
	return binary.LittleEndian.Uint64(h.Sum(nil)), nil

	// data, err := os.ReadFile(name)
	// if err != nil {
	// 	return 0, err
	// }
	// var h maphash.Hash
	// h.SetSeed(hashSeed)
	// h.Write(data)
	// return h.Sum64(), nil
}

func BenchmarkHashFile(b *testing.B) {
	if testing.Short() {
		b.Skip("short test")
	}
	sizes := []int{
		// 4,
		// 8,
		// 128,
		512,
		1024,
		1024 * 10,
	}

	data := make([]byte, sizes[len(sizes)-1]*1024)
	rr := rand.New(rand.NewSource(1234))
	for i := range data {
		data[i] = byte(rr.Intn(256))
	}
	tempdir := b.TempDir()
	for _, size := range sizes {
		name := filepath.Join(tempdir, fmt.Sprintf("%d.txt", size))
		if err := os.WriteFile(name, data[:size*1024], 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for _, size := range sizes {
		var name string
		if size < 1024 {
			name = fmt.Sprintf("%dKb", size)
		} else {
			name = fmt.Sprintf("%dMb", size/1024)
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(tempdir, fmt.Sprintf("%d.txt", size))
			b.SetBytes(int64(size) * 1024)
			for i := 0; i < b.N; i++ {
				if _, err := xhashFile(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
