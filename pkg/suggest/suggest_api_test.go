package suggest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/build"
	"go/importer"
	"hash/crc32"
	"hash/maphash"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charlievieth/gocode"
	"github.com/mdempsky/gocode/pkg/suggest"
)

var testDirFlag = flag.String("testdir", "", "specify a directory to run the test on")

func TestRegress(t *testing.T) {
	testDirs, err := filepath.Glob("testdata/test.*")
	if err != nil {
		t.Fatal(err)
	}
	if *testDirFlag != "" {
		t.Run(*testDirFlag, func(t *testing.T) {
			testRegress(t, "testdata/test."+*testDirFlag)
		})
	} else {
		for _, testDir := range testDirs {
			// Skip test.0011 for Go <= 1.11 because a method was added to reflect.Value.
			// TODO(rstambler): Change this when Go 1.12 comes out.
			if !strings.HasPrefix(runtime.Version(), "devel") && strings.HasSuffix(testDir, "test.0011") {
				continue
			}
			testDir := testDir // capture
			name := strings.TrimPrefix(testDir, "testdata/")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				testRegress(t, testDir)
			})
		}
	}
}

func testRegress(t *testing.T, testDir string) {
	testDir, err := filepath.Abs(testDir)
	if err != nil {
		t.Errorf("Abs failed: %v", err)
		return
	}

	filename := filepath.Join(testDir, "test.go.in")
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Errorf("ReadFile failed: %v", err)
		return
	}

	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		t.Errorf("Missing @")
		return
	}
	data = append(data[:cursor], data[cursor+1:]...)

	cfg := suggest.Config{
		Importer: importer.Default(),
		Logf:     func(string, ...interface{}) {},
	}
	cfg.Logf = func(string, ...interface{}) {}
	if testing.Verbose() {
		cfg.Logf = t.Logf
	}
	if cfgJSON, err := os.Open(filepath.Join(testDir, "config.json")); err == nil {
		if err := json.NewDecoder(cfgJSON).Decode(&cfg); err != nil {
			t.Errorf("Decode failed: %v", err)
			return
		}
	} else if !os.IsNotExist(err) {
		t.Errorf("Open failed: %v", err)
		return
	}
	candidates, prefixLen := cfg.Suggest(filename, data, cursor)

	var out bytes.Buffer
	suggest.NiceFormat(&out, candidates, prefixLen)
	want, err := ioutil.ReadFile(filepath.Join(testDir, "out.expected"))
	if err != nil {
		t.Fatal(err)
	}
	want = ReplaceInterfaceWithAny(want)
	if got := out.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("%s:\nGot:\n%s\nWant:\n%s\n", testDir, got, want)
		return
	}
}

//////////////////////////////////////////////////////////////
//
// TODO: bench the following tests:
// 	* test.0007: `syscall.var` - 4.5x slower
// 	* test.0032: slower
//
// Since it imports the world
//
//////////////////////////////////////////////////////////////

func BenchmarkRegress(b *testing.B) {
	testDirs, err := filepath.Glob("testdata/test.*")
	if err != nil {
		b.Fatal(err)
	}
	// if *testDirFlag != "" {
	// 	t.Run(*testDirFlag, func(t *testing.T) {
	// 		testRegress(t, "testdata/test."+*testDirFlag)
	// 	})
	// } else {
	for _, testDir := range testDirs {
		// Skip test.0011 for Go <= 1.11 because a method was added to reflect.Value.
		// TODO(rstambler): Change this when Go 1.12 comes out.
		if !strings.HasPrefix(runtime.Version(), "devel") && strings.HasSuffix(testDir, "test.0011") {
			continue
		}
		testDir := testDir // capture
		name := strings.TrimPrefix(testDir, "testdata/")
		b.Run(name, func(b *testing.B) {
			benchRegress(b, testDir)
			// benchRegressGocode(b, testDir)
		})
	}
	// }
}

/*
conf := gocode.Config{
		GOROOT:        g.GOROOT(),
		GOPATH:        g.GOPATH(),
		InstallSuffix: g.InstallSuffix,
		Builtins:      g.Builtins,
	}
	return conf.Complete(src, filename, offset)
*/

func setupBenchTest(b *testing.B, testDir string) (cfg *suggest.Config, filename string, data []byte, cursor int) {
	testDir, err := filepath.Abs(testDir)
	if err != nil {
		b.Fatalf("Abs failed: %v", err)
	}

	filename = filepath.Join(testDir, "test.go.in")
	data, err = ioutil.ReadFile(filename)
	if err != nil {
		b.Fatalf("ReadFile failed: %v", err)
	}

	cursor = bytes.IndexByte(data, '@')
	if cursor < 0 {
		b.Fatalf("Missing @")
	}
	data = append(data[:cursor], data[cursor+1:]...)

	cfg = &suggest.Config{
		Importer: importer.Default(),
		Logf:     func(string, ...interface{}) {},
	}
	if cfgJSON, err := os.Open(filepath.Join(testDir, "config.json")); err == nil {
		if err := json.NewDecoder(cfgJSON).Decode(&cfg); err != nil {
			b.Fatalf("Decode failed: %v", err)
		}
	} else if !os.IsNotExist(err) {
		b.Fatalf("Open failed: %v", err)
	}

	return cfg, filename, data, cursor
}

func benchRegress(b *testing.B, testDir string) {
	cfg, filename, data, cursor := setupBenchTest(b, testDir)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg.Suggest(filename, data, cursor)
	}
}

func benchRegressGocode(b *testing.B, testDir string) {
	_, filename, data, cursor := setupBenchTest(b, testDir)
	ctxt := build.Default
	conf := gocode.Config{
		GOROOT:        runtime.GOROOT(),
		GOPATH:        ctxt.GOPATH,
		InstallSuffix: ctxt.InstallSuffix,
		Builtins:      true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conf.Complete(data, filename, cursor)
	}
}

func contains(haystack []string, needle string) bool {
	for _, x := range haystack {
		if needle == x {
			return true
		}
	}
	return false
}

func BenchmarkCRC32(b *testing.B) {
	data, err := os.ReadFile("suggest.go")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.Run("Maphash", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		seed := maphash.MakeSeed()
		var h maphash.Hash
		h.SetSeed(seed)
		for i := 0; i < b.N; i++ {
			h.Reset()
			h.Write(data)
			_ = h.Sum64()
		}
	})
	b.Run("IEEE", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for i := 0; i < b.N; i++ {
			crc32.ChecksumIEEE(data)
		}
	})
	b.Run("Castagnoli", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for i := 0; i < b.N; i++ {
			crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
		}
	})
}
