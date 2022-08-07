package cache

import (
	"encoding/json"
	"fmt"
	"go/build"
	goimporter "go/importer"
	"go/types"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type iimporterImportFromTest struct {
	files              map[string]string
	srcDir             string
	importPath         string
	resolvedImportPath string
	isBinary           bool
}

func (m *iimporterImportFromTest) ExpectedImportPath() string {
	if m.resolvedImportPath != "" {
		return m.resolvedImportPath
	}
	return m.importPath
}

var iimporterImportFromTests = []iimporterImportFromTest{
	0: {
		files: map[string]string{
			"m/main.go": "package main\n\n" + `import "m/p"` + "\n\nfunc main() { _ = p.Const; }\n",
			"m/p/p.go":  "package p\n\nconst Const = 1\n",
		},
		srcDir:     "m",
		importPath: "m/p",
	},
	1: {
		files: map[string]string{
			"m/main.go": "package main\n\n" + `import "m/p"` + "\n\nfunc main() { _ = p.Const; }\n",
			"m/go.mod":  "module m\n\ngo 1.18\n",
			"m/p/p.go":  "package p\n\nconst Const = 1\n",
		},
		srcDir:     "m",
		importPath: "m/p",
	},
	2: {
		files: map[string]string{
			"p/p.go":          "package p\n\n" + `import "v"` + "\n\nconst X = v.X\n",
			"p/vendor/v/v.go": "package v\n\nconst X = 1\n",
		},
		srcDir:             "p",
		importPath:         "v",
		resolvedImportPath: "p/vendor/v",
	},
	3: {
		files: map[string]string{
			"p/p.go":             "package p\n\n" + `import "go-v"` + "\n\nconst X = v.X\n",
			"p/vendor/go-v/v.go": "package v\n\nconst X = 1\n",
		},
		srcDir:             "p",
		importPath:         "go-v",
		resolvedImportPath: "p/vendor/go-v",
	},
	4: {
		files: map[string]string{
			"p/p.go": "package p\n\n" + `import "fmt"` + "\n\nvar X = fmt.Sprint(123)\n",
		},
		srcDir:     "p",
		importPath: "fmt",
		isBinary:   true,
	},
}

func touchFile(t testing.TB, name string) {
	fi1, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if fi1.Size() == 0 {
		t.Fatal("empty file:", name)
	}
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	n, err := f.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(b[:n], 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatal("touch: mod time not changed")
	}
}

func testIImporterImportFrom(t *testing.T, test *iimporterImportFromTest) {
	gopath, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("TEMPDIR: %s", gopath)
		} else {
			os.RemoveAll(gopath)
		}
	})

	cmdEnv := func() []string {
		env := []string{"GOPATH=" + gopath}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GOPATH=") {
				env = append(env, e)
			}
		}
		return env
	}

	writeFile := func(name, content string) {
		name = filepath.Join(gopath, "src", name)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range test.files {
		writeFile(name, content)
	}
	srcDir := filepath.Join(gopath, "src", test.srcDir)

	if fi, err := os.Stat(srcDir); err != nil {
		t.Fatal(err)
	} else if !fi.IsDir() {
		t.Fatal("Not a directory: ", srcDir)
	}

	ctxt := build.Default
	ctxt.GOPATH = gopath
	m := newIImporter(&ctxt, t.Logf)

	isBinary := func(t *testing.T, importPath string) bool {
		pkgFile, _ := FindPkg(m.ctxt, importPath, srcDir)
		_, ok := m.ii.getPkg(pkgFile)
		return ok
	}

	isSource := func(t *testing.T, importPath string) bool {
		_, path := FindPkg(m.ctxt, importPath, srcDir)
		_, ok := m.ii.getSrc(m.ContextCacheKey(), path)
		return ok
	}

	pkg, err := m.ImportFrom(test.importPath, srcDir, 0)
	if err != nil {
		t.Error(err)
	}
	if pkg != nil {
		if pkg.Path() != test.ExpectedImportPath() {
			t.Errorf("ImportFrom(%q, %q); pkg.Path() == %q; want: %q",
				test.importPath, test.srcDir, pkg.Path(), test.ExpectedImportPath())
		}
	}
	if !test.isBinary {
		if !isSource(t, test.importPath) {
			t.Errorf("package %q should be imported as source", test.importPath)
		}
	} else {
		if !isBinary(t, test.importPath) {
			t.Errorf("package %q should be imported from binary", test.importPath)
		}
	}

	cmd := exec.Command("go", "install", "-i", "./...")
	cmd.Env = cmdEnv()
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, strings.TrimSpace(string(out)))
	}

	pkg, err = m.ImportFrom(test.importPath, srcDir, 0)
	if err != nil {
		t.Error(err)
	}
	if !isBinary(t, test.importPath) {
		t.Errorf("package %q should be imported from binary", test.importPath)
	}
	if pkg != nil {
		wantName := path.Base(test.importPath)
		if strings.HasPrefix(wantName, "go-") {
			wantName = strings.TrimPrefix(wantName, "go-")
		}
		if pkg.Name() != wantName {
			t.Errorf("ImportFrom(%q, %q); pkg.Name() == %q; want: %q",
				test.importPath, test.srcDir, pkg.Name(), wantName)
		}
		if pkg.Path() != test.ExpectedImportPath() {
			t.Errorf("ImportFrom(%q, %q); pkg.Path() == %q; want: %q",
				test.importPath, test.srcDir, pkg.Path(), test.ExpectedImportPath())
		}
	}

	// Make sure we removed the source cache entry
	if isSource(t, test.importPath) {
		t.Errorf("package %q should have been removed from the source cache", test.importPath)
	}

	// Touch the file to make sure we return the cached entry
	pkgFile, _ := FindPkg(&ctxt, test.importPath, srcDir)
	touchFile(t, pkgFile)

	ent, ok := m.ii.getPkg(pkgFile)
	if !ok || ent == nil {
		t.Fatalf("missing cache entry for: %q", pkgFile)
	}
	// Set cache times to be in the past
	ent.htime.Set(ent.htime.Time().Add(time.Minute * -60))
	ent.mtime.Set(ent.mtime.Time().Add(time.Minute * -60))

	pkg2, err := m.ImportFrom(test.importPath, srcDir, 0)
	if err != nil {
		t.Error(err)
	}
	if pkg2 != pkg {
		t.Fatal("Failed to use cached entry")
	}
}

func TestIImporterImportFrom(t *testing.T) {
	if testing.Short() {
		t.Skip("short test")
	}
	for i := range iimporterImportFromTests {
		i := i
		t.Run("", func(t *testing.T) {
			t.Parallel()
			testIImporterImportFrom(t, &iimporterImportFromTests[i])
		})
	}
}

// WARN: this test requires our dependencies to be vendored
func TestIImporterImportFrom_Reference(t *testing.T) {
	t.Skip("DELETE ME: this test is not worth it")
	if testing.Short() {
		t.Skip("short test")
	}

	// const pkgName = "golang.org/x/sync/singleflight"
	const pkgName = "golang.org/x/tools/go/gcexportdata"

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want, err := goimporter.Default().(types.ImporterFrom).ImportFrom(
		pkgName, wd, 0)
	if err != nil {
		t.Fatal(err)
	}

	ctxt := build.Default
	m := newIImporter(&ctxt, t.Logf)

	got, err := m.ImportFrom(pkgName, wd, 0)
	if err != nil {
		t.Fatal(err)
	}

	if want.Path() != got.Path() {
		t.Errorf("Path() = %q; want: %q", got.Path(), want.Path())
	}
	if want.Name() != got.Name() {
		t.Errorf("Name() = %q; want: %q", got.Name(), want.Name())
	}
	if want.Complete() != got.Complete() {
		t.Errorf("Complete() = %t; want: %t", got.Complete(), want.Complete())
	}
	if !reflect.DeepEqual(got.Imports(), want.Imports()) {
		t.Errorf("Imports():\n## Got:\n%s\n## Want:\n%s\n",
			got.Imports(), want.Imports())
	}
	if !reflect.DeepEqual(got.Scope(), want.Scope()) {
		t.Errorf("Scope():\n## Got:\n%s\n## Want:\n%s\n",
			got.Scope(), want.Scope())
	}
}

func TestHashGoPkg(t *testing.T) {
	tempdir := t.TempDir()

	filenames := make([]string, 8)
	for i := range filenames {
		name := filepath.Join(tempdir, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(name, []byte("package p\n"), 0644); err != nil {
			t.Fatal(err)
		}
		filenames[i] = name
	}

	h1, err := hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hashGoPkg is not idempotent; got: %d want: %d", h1, h2)
	}

	notGo := filepath.Join(tempdir, "a.txt")
	if err := os.WriteFile(notGo, []byte("foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h2, err = hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hashGoPkg should ignore non-Go files; got: %d want: %d", h1, h2)
	}

	subDir := filepath.Join(tempdir, "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	h2, err = hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hashGoPkg should ignore directories files; got: %d want: %d", h1, h2)
	}

	f, err := os.OpenFile(filenames[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	h2, err = hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Errorf("hashGoPkg failed to detect change to file: %d", h2)
	}
	h1 = h2

	newGo := filepath.Join(tempdir, "new.go")
	if err := os.WriteFile(newGo, []byte("package p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h2, err = hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Errorf("hashGoPkg failed to detect a new Go source file: %d", h2)
	}
	h1 = h2

	dirWithGoExtension := filepath.Join(tempdir, "dir.go")
	if err := os.MkdirAll(dirWithGoExtension, 0755); err != nil {
		t.Fatal(err)
	}
	h2, err = hashGoPkg(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hashGoPkg should ignore directories with a \".go\" extention; got: %d want: %d", h1, h2)
	}

	// Test that we include non-Go source files that are included in Go builds
	for _, ext := range []string{
		".F", ".c", ".cc", ".cpp", ".cxx", ".f", ".f90", ".for", ".h",
		".hh", ".hpp", ".hxx", ".m", ".s", ".swig", ".swigcxx", ".syso"} {

		name := filepath.Join(tempdir, "file"+ext)
		if err := os.WriteFile(name, []byte("// content\n"), 0644); err != nil {
			t.Fatal(err)
		}
		h2, err = hashGoPkg(tempdir)
		if err != nil {
			t.Fatal(err)
		}
		if h2 == h1 {
			t.Errorf("hashGoPkg failed to detect non-Go source file with extention %q", ext)
		}
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}

		// Test that removing the file results in the prior hash
		h2, err = hashGoPkg(tempdir)
		if err != nil {
			t.Fatal(err)
		}
		if h2 != h1 {
			t.Errorf("hashGoPkg failed to detect removal of a %q source file: %d", ext, h2)
		}
	}
}

func TestDirCacheEntry(t *testing.T) {
	tempdir := t.TempDir()
	names := map[string]string{
		"f1.go": "package f",
		"f2.go": "package f",
		"c.c":   "#include \"h.h\"",
		"h.h":   "#include <stdio.h>",
	}
	var filenames []string
	for name, data := range names {
		path := filepath.Join(tempdir, name)
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		filenames = append(filenames, path)
	}

	appendToFile := func(t *testing.T, name, data string) {
		f, err := os.OpenFile(filenames[0], os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(data); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	e, err := newDirCacheEntry(tempdir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Version() != 1 {
		t.Errorf("Version() = %d; want: %d", e.Version(), 1)
	}
	wantHash := e.Hash()

	check := func(t *testing.T) {
		t.Helper()
		e.ctime.Set(time.Now().Add(GoPkgCacheTTL * -4))
		if err := e.Check(); err != nil {
			t.Fatal(err)
		}
	}

	check(t)
	if got := e.Hash(); got != wantHash {
		t.Errorf("Hash() = %d; want: %d", got, wantHash)
	}

	// Append to file
	appendToFile(t, filenames[0], "\n// FOO\n")

	check(t)
	if e.Version() != 2 {
		t.Errorf("Version() = %d; want: %d", e.Version(), 2)
	}
	if got := e.Hash(); got == wantHash {
		t.Errorf("Hash() = %d; want: %d", got, wantHash)
	}

	var wg sync.WaitGroup
	var ready sync.WaitGroup
	start := make(chan struct{})
	ready.Add(4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			for i := 0; i < 4; i++ {
				check(t)
			}
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()
}

func BenchmarkDirCacheEntryCheck(b *testing.B) {
	tempdir := b.TempDir()
	names := map[string]string{
		"f1.go": "package f",
		"f2.go": "package f",
		"c.c":   "#include \"h.h\"",
		"h.h":   "#include <stdio.h>",
	}
	for name, data := range names {
		path := filepath.Join(tempdir, name)
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			b.Fatal(err)
		}
	}

	e, err := newDirCacheEntry(tempdir)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	ctime := time.Now().Add(GoPkgCacheTTL * -10)
	b.Run("Serial", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			e.ctime.Set(ctime)
			if err := e.Check(); err != nil {
				b.Fatal(err)
			}
		}
	})

	// Parallel should be much faster since we use singleflight
	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				e.ctime.Set(ctime)
				if err := e.Check(); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

func BenchmarkHashGoFiles(b *testing.B) {
	tempdir := b.TempDir()

	for i := 0; i < 8; i++ {
		name := filepath.Join(tempdir, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(name, []byte("package p\n"), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hashGoPkg(tempdir)
	}
}

func writeJSON(t testing.TB, name string, v interface{}) {
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// func TestGoImport(t *testing.T) {
// 	wd, err := os.Getwd()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	want, err := build.Default.Import("github.com/charlievieth/fastwalk", wd, build.FindOnly)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	// const wd = "/Users/cvieth/go/src/repl/repl-28"
// 	dupe := build.Default
// 	ctxt := &dupe
// 	ctxt.IsDir = func(name string) bool {
// 		fi, err := os.Lstat(name)
// 		return err == nil && fi.IsDir()
// 	}
// 	got, err := ctxt.Import("github.com/charlievieth/fastwalk", wd, build.FindOnly)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	writeJSON(t, "want.json", want)
// 	writeJSON(t, "got.json", got)
// 	assert.Equal(t, want, got)
// }

// func BenchmarkGoImport(b *testing.B) {
// 	wd, err := os.Getwd()
// 	if err != nil {
// 		b.Fatal(err)
// 	}
// 	// const wd = "/Users/cvieth/go/src/repl/repl-28"
// 	dupe := build.Default
// 	ctxt := &dupe
// 	ctxt.IsDir = func(name string) bool {
// 		fi, err := os.Lstat(name)
// 		return err == nil && fi.IsDir()
// 	}
// 	for i := 0; i < b.N; i++ {
// 		_, err := ctxt.Import("github.com/charlievieth/buildutil", wd, build.FindOnly)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// func timespecToTime(ts syscall.Timespec) time.Time {
// 	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
// }

// func modtime1(name string) (time.Time, error) {
// 	var t time.Time
// 	fi, err := os.Stat(name)
// 	if err == nil {
// 		t = fi.ModTime()
// 	}
// 	return t, err
// }

// func modtime2(name string) (time.Time, error) {
// 	var stat syscall.Stat_t
// 	if err := syscall.Stat(name, &stat); err != nil {
// 		return time.Time{}, err
// 	}
// 	ts := stat.Mtimespec
// 	return time.Unix(int64(ts.Sec), int64(ts.Nsec)), nil
// }

// func BenchmarkModTime(b *testing.B) {
// 	names := loadRuntimeBenchFiles(b, &build.Default)

// 	// const name = "./importer_test.go"
// 	b.Run("os.Stat", func(b *testing.B) {
// 		for i := 0; i < b.N; i++ {
// 			for _, name := range names {
// 				_, _ = modtime1(name)
// 			}
// 			// fi, err := os.Stat(name)
// 			// if err != nil {
// 			// 	b.Fatal(err)
// 			// }
// 			// _ = fi.ModTime()
// 		}
// 	})
// 	b.Run("Stat", func(b *testing.B) {
// 		// var stat syscall.Stat_t
// 		for i := 0; i < b.N; i++ {
// 			for _, name := range names {
// 				_, _ = modtime2(name)
// 			}
// 			// if err := syscall.Stat(name, &stat); err != nil {
// 			// 	b.Fatal(err)
// 			// }
// 			// ts := stat.Mtimespec
// 			// _ = time.Unix(int64(ts.Sec), int64(ts.Nsec))
// 		}
// 	})
// }
