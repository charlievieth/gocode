package suggest_test

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charlievieth/gocode/goimporter"
	"github.com/charlievieth/gocode/suggest"
)

func TestRegress(t *testing.T) {
	testDirs, err := filepath.Glob("testdata/test.*")
	if err != nil {
		t.Fatal(err)
	}

	for _, testDir := range testDirs {
		testDir := testDir // capture
		name := strings.TrimPrefix(testDir, "testdata/")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRegress(t, testDir)
		})
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
		Importer: new(goimporter.Importer),
	}
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

	want, _ := ioutil.ReadFile(filepath.Join(testDir, "out.expected"))
	if got := out.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("%s:\nGot:\n%s\nWant:\n%s\n", testDir, got, want)
		return
	}
}

type suggestBenchmark struct {
	filename string
	data     []byte
	cursor   int
}

var loadSuggestBenchmarksOnce sync.Once
var suggestBenchmarks []*suggestBenchmark

func newBenchmark(b *testing.B, testDir string) *suggestBenchmark {
	testDir, err := filepath.Abs(testDir)
	if err != nil {
		b.Errorf("Abs failed: %v", err)
		return nil
	}

	filename := filepath.Join(testDir, "test.go.in")
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		b.Errorf("ReadFile failed: %v", err)
		return nil
	}

	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		b.Errorf("Missing @")
		return nil
	}
	data = append(data[:cursor], data[cursor+1:]...)

	return &suggestBenchmark{
		filename: filename,
		data:     data,
		cursor:   cursor,
	}
}

func loadBenchmarks(b *testing.B) {
	testDirs, err := filepath.Glob("testdata/test.*")
	if err != nil {
		b.Fatal(err)
	}
	for _, testDir := range testDirs {
		suggestBenchmarks = append(suggestBenchmarks, newBenchmark(b, testDir))
	}
}

func BenchmarkOne(b *testing.B) {
	loadSuggestBenchmarksOnce.Do(func() {
		loadBenchmarks(b)
	})
	test := suggestBenchmarks[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := suggest.Config{
			Importer: new(goimporter.Importer),
		}
		cfg.Suggest(test.filename, test.data, test.cursor)
	}
}

func BenchmarkTen(b *testing.B) {
	loadSuggestBenchmarksOnce.Do(func() {
		loadBenchmarks(b)
	})
	tests := suggestBenchmarks[0:10]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := suggest.Config{
			Importer: new(goimporter.Importer),
		}
		for _, x := range tests {
			cfg.Suggest(x.filename, x.data, x.cursor)
		}
	}
}

func BenchmarkAll(b *testing.B) {
	loadSuggestBenchmarksOnce.Do(func() {
		loadBenchmarks(b)
	})
	tests := suggestBenchmarks
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := suggest.Config{
			// Importer: new(goimporter.Importer),
		}
		for _, x := range tests {
			cfg.Suggest(x.filename, x.data, x.cursor)
		}
	}
}

func BenchmarkParallel(b *testing.B) {
	loadSuggestBenchmarksOnce.Do(func() {
		loadBenchmarks(b)
	})
	tests := suggestBenchmarks
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		cfg := suggest.Config{
			Importer: new(goimporter.Importer),
		}
		for pb.Next() {
			for _, x := range tests {
				cfg.Suggest(x.filename, x.data, x.cursor)
			}
		}
	})
}
