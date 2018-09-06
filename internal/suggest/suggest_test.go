package suggest

import (
	"bytes"
	"encoding/json"
	"go/build"
	"go/importer"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mdempsky/gocode/internal/gbimporter"
)

func TestRegress(t *testing.T) {
	t.Skip("DONT CARE")
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

	cfg := Config{
		Importer: importer.Default(),
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
	NiceFormat(&out, candidates, prefixLen)

	want, _ := ioutil.ReadFile(filepath.Join(testDir, "out.expected"))
	if got := out.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("%s:\nGot:\n%s\nWant:\n%s\n", testDir, got, want)
		return
	}
}

func BenchmarkParseOtherPackageFiles(b *testing.B) {
	goroot := runtime.GOROOT()
	if goroot == "" {
		b.Skip("GOROOT must be set for this benchmark")
	}
	filename := filepath.Join(goroot, "src", "os", "file.go")
	if _, err := os.Stat(filename); err != nil {
		b.Skipf("cannot stat 'os/file.go': %s", err)
	}
	packed := gbimporter.PackContext(&build.Default)
	conf := Config{Context: &packed}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := conf.parseOtherPackageFiles(token.NewFileSet(), filename, "os")
		if err != nil {
			b.Fatal(err)
		}
	}
}
