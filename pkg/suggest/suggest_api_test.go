package suggest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/build"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charlievieth/gocode"
	"github.com/mdempsky/gocode/pkg/cache"
	"github.com/mdempsky/gocode/pkg/suggest"
	"github.com/stretchr/testify/assert"
)

var testDirFlag = flag.String("testdir", "", "specify a directory to run the test on")

func TestCompleteV2(t *testing.T) {
	// Add support for "v2" and other non-v1 packages
	// TODO: we could try stripping out the version
	if testing.Verbose() {
		t.Fatalf("TODO: add support for v2 (%q) packages/imports",
			"github.com/posener/complete/v2")
	} else {
		t.Skipf("TODO: add support for v2 (%q) packages/imports",
			"github.com/posener/complete/v2")
	}
}

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

func testingSuggestConfig(t testing.TB, ctxt *build.Context) *suggest.Config {
	if ctxt == nil {
		dupe := build.Default
		ctxt = &dupe
	}
	var logf func(string, ...interface{})
	if testing.Verbose() {
		logf = t.Logf
	} else {
		logf = func(string, ...interface{}) {}
	}
	return &suggest.Config{
		Context:  ctxt,
		Importer: cache.NewIImporter(ctxt, nil),
		Logf:     logf,
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

	cfg := testingSuggestConfig(t, nil)
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
		assert.Equal(t, string(want), string(got))
		return
	}
}

type suggestTest struct {
	files   map[string]string
	target  string
	replace string
	want    []string
}

// TODO: test "replace" directive
var suggestTests = []suggestTest{
	{
		files: map[string]string{
			"m/m.go": `
package main

import (
	"fmt"

	"mvdan.cc/xurls/v2"
)

func main() {
	fmt.Println(xurls.PseudoTLDs)
}
`,
			"m/go.mod": "module m\n\ngo 1.18\n\nrequire mvdan.cc/xurls/v2 v2.4.0",
			"m/go.sum": `
github.com/kr/pretty v0.1.0/go.mod h1:dAy3ld7l9f0ibDNOQOHHMYYIIbhfbHSm3C4ZsoJORNo=
github.com/kr/pty v1.1.1/go.mod h1:pFQYn66WHrOpPYNljwOMqo10TkYh1fy3cYio2l3bCsQ=
github.com/kr/text v0.1.0/go.mod h1:4Jbv+DJW3UT/LiOwJeYQe1efqtUx/iVham/4vfdArNI=
github.com/pkg/diff v0.0.0-20210226163009-20ebb0f2a09e/go.mod h1:pJLUxLENpZxwdsKMEsNbx1VGcRFpLqf3715MtcvvzbA=
github.com/rogpeppe/go-internal v1.8.1/go.mod h1:JeRgkft04UBgHMgCIwADu4Pn6Mtm5d4nPKWu0nJ5d+o=
golang.org/x/sync v0.0.0-20210220032951-036812b2e83c/go.mod h1:RxMgew5VJxzue5/jJTE5uejpjVlOe/izrB70Jof72aM=
gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127/go.mod h1:Co6ibVJAznAaIkqp8huTwlJQCZ016jof/cbN4VW5Yz0=
gopkg.in/errgo.v2 v2.1.0/go.mod h1:hNsd1EY+bozCKY1Ytp96fpM3vjJbqLJn88ws8XvfDNI=
mvdan.cc/xurls/v2 v2.4.0 h1:tzxjVAj+wSBmDcF6zBB7/myTy3gX9xvi8Tyr28AuQgc=
mvdan.cc/xurls/v2 v2.4.0/go.mod h1:+GEjq9uNjqs8LQfM9nVnM8rff0OQ5Iash5rzX+N1CSg=
`,
		},
		target:  "m/m.go",
		replace: "PseudoTLDs",
		want: []string{
			"func Relaxed() *regexp.Regexp",
			"func Strict() *regexp.Regexp",
			"func StrictMatchingScheme(exp string) (*regexp.Regexp, error)",
			"var AnyScheme string",
			"var PseudoTLDs []string",
			"var Schemes []string",
			"var SchemesNoAuthority []string",
			"var SchemesUnofficial []string",
			"var TLDs []string",
		},
	},
	// {
	// 	files: map[string]string{
	// 		"m/m.go":   "package main\n\nimport (\n\t\"fmt\"\n\t\"p\"\n)\n\nfunc main() {\n\tfmt.Println(p.V1)\n}'",
	// 		"p/p.go":   "package p\n\nconst V1 = 1\nconst V2 = 2\n",
	// 		"p/go.mod": "module p2\n\ngo 1.18",
	// 	},
	// },
}

func testCommandEnv(gopath string) []string {
	env := []string{"GOPATH=" + gopath}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOPATH=") {
			env = append(env, e)
		}
	}
	return env
}

func removeTempModules(t testing.TB, gopath string) {
	pkgDir := filepath.Join(gopath, "pkg")
	if fi, err := os.Stat(pkgDir); err != nil || !fi.IsDir() {
		return
	}
	cmd := exec.Command("go", "clean", "-modcache")
	cmd.Env = testCommandEnv(gopath)
	cmd.Dir = gopath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("error: %v: %s", err, string(bytes.TrimSpace(out)))
	}
}

func runSuggestTest(t *testing.T, test suggestTest) {
	gopath := t.TempDir()
	t.Cleanup(func() { removeTempModules(t, gopath) })

	writeFile := func(name, content string) {
		name = filepath.Join(gopath, "src", name)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for name, data := range test.files {
		writeFile(name, strings.TrimLeft(data, "\n\t "))
	}

	filename := filepath.Join(gopath, "src", test.target)

	// for _, args := range [][]string{
	// 	{"build", "-i"},
	// } {
	// 	cmd := exec.Command("go", args...)
	// 	cmd.Dir = filepath.Dir(filename)
	// 	cmd.Env = testCommandEnv(gopath)
	// 	if out, err := cmd.CombinedOutput(); err != nil {
	// 		t.Fatalf("error: %v: %s", err, string(bytes.TrimSpace(out)))
	// 	}
	// }

	ctxt := build.Default
	ctxt.GOPATH = gopath

	source := strings.Replace(test.files[test.target], test.replace, "@", 1)
	cursor := strings.IndexByte(source, '@')
	if cursor < 0 {
		t.Errorf("Missing @")
		return
	}
	data := []byte(source[:cursor] + source[cursor+1:])

	cfg := testingSuggestConfig(t, &ctxt)

	candidates, _ := cfg.Suggest(filename, data, cursor)
	if len(candidates) == 0 {
		t.Fatal("failed to find any candidates")
	}
	var got []string
	for _, c := range candidates {
		got = append(got, c.String())
	}
	assert.Equal(t, test.want, got)

	// WARN
	// t.Logf("candidates: %q", candidates)
}

func TestSuggestModules(t *testing.T) {
	if testing.Short() {
		t.Skip("short test")
	}
	for _, test := range suggestTests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()
			runSuggestTest(t, test)
		})
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

	cfg = testingSuggestConfig(b, nil)
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

// TODO: use this to benchmark how long it takes to initialize the
// caches when importing ALL stdlib packages.
func BenchmarkCompleteAllStdlib(b *testing.B) {
	data, err := os.ReadFile("testdata/bench/all.go")
	if err != nil {
		b.Fatal(err)
	}
	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		b.Fatal("invalid cursor:", cursor)
	}
	data = append(data[:cursor], data[cursor+1:]...)

	// Pretend the filename is local to us
	filename, err := filepath.Abs("./all.go")
	if err != nil {
		b.Fatal(err)
	}

	ctxt := build.Default
	ctxt.BuildTags = []string{"never"}
	cfg := testingSuggestConfig(b, &ctxt)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cs, _ := cfg.Suggest(filename, data, cursor)
		if len(cs) == 0 {
			b.Fatal("no candidates")
		}
	}
}

// func BenchmarkCRC32(b *testing.B) {
// 	data, err := os.ReadFile("suggest.go")
// 	if err != nil {
// 		b.Fatal(err)
// 	}
// 	b.ResetTimer()
// 	b.Run("Maphash", func(b *testing.B) {
// 		b.SetBytes(int64(len(data)))
// 		seed := maphash.MakeSeed()
// 		var h maphash.Hash
// 		h.SetSeed(seed)
// 		for i := 0; i < b.N; i++ {
// 			h.Reset()
// 			h.Write(data)
// 			_ = h.Sum64()
// 		}
// 	})
// 	b.Run("IEEE", func(b *testing.B) {
// 		b.SetBytes(int64(len(data)))
// 		for i := 0; i < b.N; i++ {
// 			crc32.ChecksumIEEE(data)
// 		}
// 	})
// 	b.Run("Castagnoli", func(b *testing.B) {
// 		b.SetBytes(int64(len(data)))
// 		for i := 0; i < b.N; i++ {
// 			crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
// 		}
// 	})
// }
