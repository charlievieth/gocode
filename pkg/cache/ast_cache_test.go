package cache

import (
	"go/build"
	"go/parser"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loadRuntimeBenchFiles(t testing.TB, ctxt *build.Context) []string {
	t.Helper()
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Skip("test requires GOROOT")
	}

	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	filenames := make([]string, 0, len(des))
	for _, d := range des {
		name := d.Name()
		if d.IsDir() || strings.HasSuffix(name, "_test.go") || !strings.HasSuffix(name, ".go") {
			continue
		}
		if ok, _ := ctxt.MatchFile(dir, name); ok {
			filenames = append(filenames, filepath.Join(dir, name))
		}
		if len(filenames) == 64 {
			break
		}
	}

	return filenames
}

func BenchmarkParsePackageFiles(b *testing.B) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		b.Skip("test requires GOROOT")
	}

	ctxt := build.Default
	filenames := loadRuntimeBenchFiles(b, &ctxt)

	c := LoadAstCache()
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.ParsePackageFiles(filenames)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParsePackageFilesParallel(b *testing.B) {
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		b.Skip("test requires GOROOT")
	}

	ctxt := build.Default
	filenames := loadRuntimeBenchFiles(b, &ctxt)

	c := LoadAstCache()
	runtime.GC()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := c.ParsePackageFiles(filenames)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkParseSource(b *testing.B) {
	filename := filepath.Join(runtime.GOROOT(), "src", "runtime", "map.go")
	data, err := os.ReadFile(filename)
	if err != nil {
		b.Skip("test requires GOROOT:", err)
	}

	c := LoadAstCache()
	// Populate the cache
	for _, name := range loadRuntimeBenchFiles(b, &build.Default) {
		src, err := os.ReadFile(name)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := c.ParseSource(name, src, parser.AllErrors); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.ParseSource(filename, data, parser.AllErrors)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// func TestAtomicTime(t *testing.T) {
// 	var a atomicTime
// 	if !a.Time().IsZero() {
// 		t.Errorf("got: %s want: %s", a.Time(), time.Time{})
// 	}
// 	now := time.Now()
// 	a.Store(now)
// 	got := a.Time()
// 	if !got.Equal(now) {
// 		t.Errorf("got: %s want: %s", got, now)
// 	}
// }

// func TestAtomicTimeRace(t *testing.T) {
// 	a := &atomicTime{}
// 	var wg sync.WaitGroup
// 	var ready sync.WaitGroup
// 	start := make(chan struct{})
// 	for i := 0; i < 4; i++ {
// 		wg.Add(1)
// 		ready.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			ready.Done()
// 			<-start
// 			for i := 0; i < 1000; i++ {
// 				a.Store(time.Now())
// 				if a.Time().IsZero() {
// 					t.Error("zero time")
// 					return
// 				}
// 			}
// 		}()
// 	}
// 	ready.Wait()
// 	close(start)
// 	wg.Wait()
// }
