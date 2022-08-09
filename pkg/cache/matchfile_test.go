package cache

import (
	"bytes"
	"go/build"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charlievieth/buildutil"
)

func testMatchFile(t *testing.T, m *MatchCache, ctxt *build.Context, dir string) {
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if testing.Short() && len(des) > 64 {
		des = des[:64]
	}

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Context: %+v", *ctxt)
		}
	})
	for _, d := range des {
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".go") {
			continue
		}
		if testing.Short() && strings.HasSuffix(d.Name(), "_test.go") {
			continue
		}
		info, err := d.Info()
		if err != nil {
			t.Fatal(err)
		}
		gotName, gotMatch, err := m.MatchFileInfo(ctxt, dir, info)
		if err != nil {
			t.Fatal(err)
		}
		wantName, err := buildutil.ReadPackageName(filepath.Join(dir, d.Name()), nil)
		if err != nil {
			t.Fatal(err)
		}
		wantMatch, err := ctxt.MatchFile(dir, d.Name())
		if err != nil {
			t.Fatal(err)
		}
		if gotMatch != wantMatch || (wantMatch && gotName != wantName) {
			t.Errorf("MatchFile(%q) = %q, %t; want: %q, %t", d.Name(),
				gotName, gotMatch, wantName, wantMatch)
		}
	}

	if !testing.Short() {
		m.cache.ForEach(func(name string, val interface{}) bool {
			ent := val.(*matchEntry)
			fi, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if !ent.modTime.Equal(fi.ModTime()) {
				t.Errorf("%q: ModTime got: %s want: %s", name, ent.modTime.Time(), fi.ModTime())
			}
			return true
		})
	}
}

func TestMatchFile(t *testing.T) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Skip("skipping: test requires Go source:", err)
	}

	m := new(MatchCache)
	testParallel := func(t *testing.T, ctxt *build.Context) {
		if testing.Short() {
			testMatchFile(t, m, ctxt, dir)
			return
		}
		// Run concurrently to make sure we don't have any data races
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				testMatchFile(t, m, ctxt, dir)
			}()
		}
		wg.Wait()
	}

	t.Run("darwin/amd64", func(t *testing.T) {
		t.Parallel()
		ctxt := build.Default
		ctxt.GOOS = "darwin"
		ctxt.GOARCH = "amd64"
		testParallel(t, &ctxt)
	})

	t.Run("linux/amd64", func(t *testing.T) {
		t.Parallel()
		ctxt := build.Default
		ctxt.GOOS = "linux"
		ctxt.GOARCH = "amd64"
		ctxt.CgoEnabled = true
		testParallel(t, &ctxt)
	})
}

func TestMatchFileChanged(t *testing.T) {
	gopath := filepath.Join(t.TempDir(), "src")
	dir := filepath.Join(gopath, "m")

	writeFile := func(t *testing.T, name, data string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"m_never.go":  "//go:build never\n// +build never\n\npackage main\n",
		"m_always.go": "//go:build !never\n// +build !never\n\npackage main\n",
		"m_darwin.go": "package main\n",
		"m_amd64.go":  "package main\n",
	}
	for name, data := range files {
		writeFile(t, name, data)
	}

	ctxt := build.Default
	ctxt.GOOS = "darwin"
	ctxt.GOARCH = "amd64"
	ctxt.GOPATH = gopath
	ctxt.BuildTags = []string{"never"}

	m := new(MatchCache)
	t.Run("New", func(t *testing.T) {
		testMatchFile(t, m, &ctxt, dir)
	})

	t.Run("ModTimeChanged", func(t *testing.T) {
		// Touch all the files
		for name, data := range files {
			writeFile(t, name, data)
		}
		testMatchFile(t, m, &ctxt, dir)
	})

	t.Run("HashChanged", func(t *testing.T) {
		// Touch all the files
		for name, data := range files {
			// same size change
			data = strings.ReplaceAll(data, "main", "niam")
			writeFile(t, name, data)
		}
		testMatchFile(t, m, &ctxt, dir)
	})

	t.Run("SizeChanged", func(t *testing.T) {
		for name, data := range files {
			writeFile(t, name, data+"\n")
		}
		testMatchFile(t, m, &ctxt, dir)
	})
}

func TestCachingReader(t *testing.T) {
	f, err := os.Open("testdata/bench/read_tags.go")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	sizes := []int{
		0, 1, int(fi.Size() - 1), int(fi.Size()),
	}

	// Append some random sizes
	rr := rand.New(rand.NewSource(time.Now().UnixNano()))
	for len(sizes) < 20 {
		sizes = append(sizes, rr.Intn(int(fi.Size())))
	}
	sort.Ints(sizes)

	for _, size := range sizes {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			if _, err := f.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			cr := &cachingReader{file: f}

			n := rr.Intn(int(fi.Size()))
			buf := make([]byte, n)
			if _, err := io.ReadFull(cr, buf); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(cr.Bytes(), buf) {
				t.Error("read mismatch")
			}
			data, err := io.ReadAll(cr.ReadCloser())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, buf) {
				t.Error("read mismatch")
			}
		})
	}
}

func BenchmarkMatchFile(b *testing.B) {
	dir, err := filepath.Abs("./testdata/bench")
	if err != nil {
		b.Fatal(err)
	}
	dupe := build.Default
	ctxt := &dupe

	m := new(MatchCache)
	m.once.Do(m.initialize)
	setupBench := func(b *testing.B, name string) (*MatchCache, os.FileInfo) {
		m.cache.Clear()
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := m.MatchFileInfo(ctxt, dir, fi); err != nil {
			b.Fatal(err)
		}
		_, ok := m.cache.Get(filepath.Join(dir, name))
		if !ok {
			b.Fatal("missing file:", name)
		}
		b.ResetTimer()

		return m, fi
	}

	b.Run("NoChange", func(b *testing.B) {
		m, fi := setupBench(b, "read_notags.go")
		for i := 0; i < b.N; i++ {
			if _, _, err := m.MatchFileInfo(ctxt, dir, fi); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		m, fi := setupBench(b, "read_notags.go")
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, _, err := m.MatchFileInfo(ctxt, dir, fi); err != nil {
					b.Fatal(err)
				}
			}
		})
	})

	b.Run("ModTime", func(b *testing.B) {
		path := filepath.Join(dir, "read_notags.go")
		m, fi := setupBench(b, "read_notags.go")
		if _, ok := m.cache.Get(path); !ok {
			b.Fatal("missing:", path)
		}
		for i := 0; i < b.N; i++ {
			if _, _, err := m.MatchFileInfo(ctxt, dir, fi); err != nil {
				b.Fatal(err)
			}
			e, _ := m.get(path)
			e.modTime = 0
		}
	})

	for _, bench := range []struct{ benchname, filename string }{
		{"NoTags", "read_notags.go"},
		{"Tags", "read_notags.go"},
	} {
		b.Run(bench.benchname, func(b *testing.B) {
			b.Run("NewEntry", func(b *testing.B) {
				path := filepath.Join(dir, bench.filename)
				m, fi := setupBench(b, bench.filename)
				for i := 0; i < b.N; i++ {
					if _, _, err := m.MatchFileInfo(ctxt, dir, fi); err != nil {
						b.Fatal(err)
					}
					m.cache.Remove(path)
				}
			})
		})
	}
}
