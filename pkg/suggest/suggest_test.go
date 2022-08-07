package suggest

import (
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

type findOtherPackageFilesTest struct {
	filename string
	pkgName  string
	files    map[string]string
	want     []string
}

var findOtherPackageFilesTests = []findOtherPackageFilesTest{
	{
		"p1_test.go",
		"p_test",
		map[string]string{
			"p1_test.go": "package p_test\n",
			"p2_test.go": "package p_test\n",
			"p_test.go":  "package p\n",
			"p.go":       "package p\n",
		},
		[]string{"p2_test.go"},
	},
	{
		"p1_api_test.go",
		"p",
		map[string]string{
			"p1_test.go": "package p_test\n",
			"p2_test.go": "package p_test\n",
			"p_test.go":  "package p\n",
			"p.go":       "package p\n",
		},
		[]string{"p.go", "p_test.go"},
	},
	{
		"p.go",
		"p",
		map[string]string{
			"p_darwin_arm64.go": "package p\n",
			"p_darwin_amd64.go": "package p\n",
			"p_darwin.go":       "package p\n",
			"p_arm64.go":        "package p\n",
			"p_amd64.go":        "package p\n",
		},
		[]string{"p_arm64.go", "p_darwin.go", "p_darwin_arm64.go"},
	},
	{
		"build_tag.go",
		"p",
		map[string]string{
			"okay.go":           "package p\n",
			"build_tag_test.go": "package p\n",
			"never.go":          "//go:build never\n// +build never\n\npackage p\n",
		},
		[]string{"okay.go"},
	},
	{
		"other_files.go",
		"p",
		map[string]string{
			"okay.go":    "package p\n",
			"lib.c":      "",
			".ignore.go": "package p\n",
			"_ignore.go": "package p\n",
			"dir.go":     "DIR",
		},
		[]string{"okay.go"},
	},
}

func TestFindOtherPackageFilesTests(t *testing.T) {
	for i, test := range findOtherPackageFilesTests {
		if !sort.StringsAreSorted(test.want) {
			t.Errorf("%d: want strings are not sorted: %q", i, test.want)
		}
	}
}

// func testFindOtherPackageFiles(t *testing.T, conf *Config, test findOtherPackageFilesTest) {
// 	dir := filepath.Join(t.TempDir())
// 	if err := os.MkdirAll(dir, 0755); err != nil {
// 		t.Fatal(err)
// 	}
// 	for name, content := range test.files {
// 		name = filepath.Join(dir, name)
// 		switch content {
// 		case "DIR":
// 			if err := os.MkdirAll(name, 0755); err != nil {
// 				t.Fatal(err)
// 			}
// 		default:
// 			if err := os.WriteFile(name, []byte(content), 0644); err != nil {
// 				t.Fatal(err)
// 			}
// 		}
// 	}

// 	filename := filepath.Join(dir, test.filename)
// 	got := conf.findOtherPackageFiles(conf.Context, filename, test.pkgName)
// 	for i, s := range got {
// 		p, err := filepath.Rel(dir, s)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		got[i] = p
// 	}
// 	if !sort.StringsAreSorted(got) {
// 		t.Error("strings are not sorted")
// 	}
// 	if !reflect.DeepEqual(got, test.want) {
// 		t.Errorf("findOtherPackageFiles(%q, %q) = %q; want: %q",
// 			test.filename, test.pkgName, got, test.want)
// 	}
// }

// func TestFindOtherPackageFiles(t *testing.T) {
// 	ctxt := build.Default
// 	ctxt.GOOS = "darwin"
// 	ctxt.GOARCH = "arm64"

// 	conf := &Config{
// 		Context: &ctxt,
// 		Logf:    t.Fatalf,
// 	}
// 	for i, test := range findOtherPackageFilesTests {
// 		name := fmt.Sprintf("%d/%s", i, test.filename)
// 		t.Run(name, func(t *testing.T) {
// 			testFindOtherPackageFiles(t, conf, test)
// 		})
// 	}
// }

var mergeStringsTests = []struct {
	s1, s2, want []string
}{
	{[]string{}, []string{}, []string{}},
	{[]string{"a"}, []string{}, []string{"a"}},
	{[]string{}, []string{"a"}, []string{"a"}},
	{[]string{"a"}, []string{"a"}, []string{"a"}},
	{[]string{"a"}, []string{"a", "b"}, []string{"a", "b"}},
	{[]string{"a", "d", "z"}, []string{"a", "b"}, []string{"a", "b", "d", "z"}},
	{[]string{"a", "b", "c"}, []string{"x", "y", "z"}, []string{"a", "b", "c", "x", "y", "z"}},
}

func TestMergeStrings(t *testing.T) {
	for _, test := range mergeStringsTests {
		scratch := make([]string, len(test.s1)+len(test.s2))
		got := mergeStrings(test.s1, test.s2, scratch)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("mergeStrings(%q, %q) = %q; want: %q", test.s1, test.s2, got, test.want)
		}
	}
}

func benchmarkMergeStrings(b *testing.B, s1, s2 []string) {
	scratch := make([]string, len(s1)+len(s1))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mergeStrings(s1, s2, scratch)
	}
}

func BenchmarkMergeStrings(b *testing.B) {
	b.Run("Merge1", func(b *testing.B) {
		s1 := []string{
			"a",
		}
		s2 := []string{
			"Errorf", "Formatter", "Fprint", "Fprintf", "Fprintln", "Fscan",
			"Fscanf", "Fscanln", "GoStringer", "Print", "Printf", "Println",
			"Scan", "ScanState", "Scanf", "Scanln", "Scanner", "Sprint",
			"Sprintf", "Sprintln", "Sscan", "Sscanf", "Sscanln", "State",
			"Stringer",
		}
		benchmarkMergeStrings(b, s1, s2)
	})

	b.Run("Many", func(b *testing.B) {
		s1 := []string{
			".inittask", "Errorf", "Formatter", "Fprint", "Fprintf",
			"Fprintln", "Fscan", "Fscanf", "Fscanln", "GoStringer", "Print",
			"Printf", "Println", "Scan", "ScanState", "Scanf", "Scanln",
			"Scanner", "Sprint", "Sprintf", "Sprintln", "Sscan", "Sscanf",
			"Sscanln", "State", "Stringer", "stringReader",
		}
		s2 := []string{
			"any", "append", "bool", "byte", "cap", "close", "comparable",
			"complex", "complex128", "complex64", "copy", "delete", "error",
			"false", "float32", "float64", "imag", "int", "int16", "int32",
			"int64", "int8", "iota", "len", "make", "new", "nil", "panic",
			"print", "println", "real", "recover", "rune", "string", "true",
			"uint", "uint16", "uint32", "uint64", "uint8", "uintptr",
		}
		benchmarkMergeStrings(b, s1, s2)
	})
}

// func BenchmarkFindOtherPackageFiles(b *testing.B) {
// 	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
// 	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
// 		b.Skip("test requires GOROOT")
// 	}

// 	ctxt := build.Default
// 	ctxt.GOOS = "darwin"
// 	ctxt.GOARCH = "arm64"
// 	conf := &Config{
// 		Context: &ctxt,
// 	}
// 	filename := filepath.Join(dir, "map.go")
// 	for i := 0; i < b.N; i++ {
// 		conf.findOtherPackageFiles(&ctxt, filename, "runtime")
// 	}
// }

func BenchmarkAnalyzePackage(b *testing.B) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		b.Skip("test requires GOROOT")
	}

	ctxt := build.Default
	ctxt.GOOS = "darwin"
	ctxt.GOARCH = "arm64"
	conf := &Config{
		Context: &ctxt,
		Logf:    b.Logf,
	}
	filename := filepath.Join(dir, "map.go")
	data, err := os.ReadFile(filename)
	if err != nil {
		b.Fatal(err)
	}

	cursor := len(data) - 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conf.analyzePackage(&ctxt, filename, data, cursor)
	}
}
