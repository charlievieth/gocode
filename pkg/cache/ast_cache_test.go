package cache

import (
	"go/ast"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/charlievieth/buildutil"
	"github.com/stretchr/testify/require"
)

func TestNumWorkers(t *testing.T) {
	for i := -1; i <= 1; i++ {
		n := numWorkers(i)
		if n != 1 {
			t.Errorf("numWorkers(%d) = %d; want: %d", i, n, 1)
		}
	}
}

func TestFilterDirEntries(t *testing.T) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Skip("test requires GOROOT")
	}

	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctxt := build.Default

	filter := func(d fs.DirEntry) bool {
		return d.Name() != "map.go"
	}

	var want []string
	for _, d := range des {
		if !filter(d) {
			continue
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") {
			if buildutil.GoodOSArchFile(&ctxt, name, nil) {
				want = append(want, name)
			}
		}
	}

	des = filterDirEntries(&ctxt, des, filter)
	var got []string
	for _, d := range des {
		got = append(got, d.Name())
	}

	require.Equal(t, want, got)
}

func TestRemoveNullFiles(t *testing.T) {
	test := func(t *testing.T, files []*ast.File) {
		t.Helper()
		var want []*ast.File
		for _, af := range files {
			if af != nil {
				want = append(want, af)
			}
		}
		got := removeNilFiles(append([]*ast.File(nil), files...))
		if len(want) == 0 && got != nil {
			t.Fatalf("removeNullFiles(%v) = %v; want: %v", files, got, nil)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("removeNullFiles(%v) = %v; want: %v", files, got, want)
		}
		for i, af := range got {
			if af == nil {
				t.Errorf("nil file at index: %d", i)
			}
		}
	}

	test(t, nil)
	test(t, []*ast.File{})
	test(t, make([]*ast.File, 0, 2))
	test(t, []*ast.File{nil, new(ast.File)})
	test(t, []*ast.File{new(ast.File), nil})
	test(t, []*ast.File{nil, nil})
	test(t, []*ast.File{
		0: new(ast.File),
		1: new(ast.File),
		2: nil,
		3: new(ast.File),
		4: nil,
	})

	// Make sure we return nil when the result length is zero.
	if removeNilFiles(make([]*ast.File, 2)) != nil {
		t.Error("removeNullFiles should return nil when there are no non-nil files")
	}
	if removeNilFiles([]*ast.File{}) != nil {
		t.Error("removeNullFiles should return nil when there are no non-nil files")
	}
}

// type fakeDirEntry struct {
// 	name string
// }

// func (d fakeDirEntry) Name() string               { return d.name }
// func (d fakeDirEntry) IsDir() bool                { panic("not implemented") }
// func (d fakeDirEntry) Type() fs.FileMode          { panic("not implemented") }
// func (d fakeDirEntry) Info() (fs.FileInfo, error) { panic("not implemented") }

func TestFilterForFile(t *testing.T) {
	test := func(t *testing.T, filename string, want map[string]bool) {
		filter := FilterForFile(filename)
		for name, exp := range want {
			got := filter(name)
			if got != exp {
				t.Errorf("filter(%q) = %t; want: %t", name, got, exp)
			}
		}
	}

	t.Run("ExcludeTest", func(t *testing.T) {
		test(t, "main.go", map[string]bool{
			"foo.go":      true,
			"main.go":     false,
			"foo_test.go": false,
		})
	})

	t.Run("Test", func(t *testing.T) {
		test(t, "main_test.go", map[string]bool{
			"foo.go":       true,
			"main.go":      true,
			"main_test.go": false,
			"foo_test.go":  true,
		})
	})
}

func TestAstCacheParsePackage(t *testing.T) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Skip("test requires GOROOT")
	}

	c := &AstCache{
		Size:        DefaultAstCacheSize,
		FileSetSize: DefaultFileSetMaxSize,
	}
	ctxt := build.Default

	filter := func(name string) bool {
		return name != "map.go"
	}

	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	match := func(name string) bool {
		if ok, _ := ctxt.MatchFile(dir, name); !ok {
			return false
		}
		pkgName, _ := buildutil.ReadPackageName(filepath.Join(dir, name), nil)
		return pkgName == "runtime"
	}

	var wantLen int
	for _, d := range des {
		if d.IsDir() || !filter(d.Name()) {
			continue
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") && match(name) {
			wantLen++
		}
	}
	if wantLen == 0 {
		t.Fatal("Failed to match any files!")
	}

	files, err := c.ParsePackage(&ctxt, dir, "runtime", filter)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != wantLen {
		t.Errorf("ParsePackage(%q, %q) returned %d files; want: %d",
			dir, "runtime", len(files), wantLen)
	}
}

func BenchmarkAstCacheParsePackage(b *testing.B) {
	// dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	dir := filepath.Join(runtime.GOROOT(), "src", "fmt")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		b.Skip("test requires GOROOT")
	}

	c := &AstCache{
		Size:        DefaultAstCacheSize,
		FileSetSize: DefaultFileSetMaxSize,
	}
	ctxt := build.Default

	filter := func(name string) bool {
		return name != "map.go"
	}

	b.Run("Cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			files, err := c.ParsePackage(&ctxt, dir, "runtime", filter)
			if err != nil {
				b.Fatal(err)
			}
			_ = files
		}
	})

	b.Run("NoCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			files, err := c.ParsePackage(&ctxt, dir, "runtime", filter)
			if err != nil {
				b.Fatal(err)
			}
			_ = files
			c.cache.Clear()
			c.Match.cache.Clear()
		}
	})
}
