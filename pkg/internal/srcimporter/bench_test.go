package srcimporter

import (
	"go/build"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func BenchmarkImport(b *testing.B) {
	if !HasSrc() {
		b.Skip("no source code available")
	}
	ctxt := build.Default
	pkgs := make(map[string]*types.Package)
	fset := token.NewFileSet()
	p := New(&ctxt, fset, pkgs)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.Import("runtime")
		if err != nil {
			b.Fatal(err)
		}
		for k := range p.packages {
			delete(p.packages, k)
		}
		// delete(p.packages, "runtime")
	}
	// b.Errorf("%+v\n", p.packages)
}

func BenchmarkParseFiles(b *testing.B) {
	if !HasSrc() {
		b.Skip("no source code available")
	}
	dir := filepath.Join(runtime.GOROOT(), "src", "runtime")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		b.Skip("not a directory:", dir)
	}

	ctxt := build.Default
	bp, err := ctxt.Import("runtime", ".", 0)
	if err != nil {
		b.Fatal(err)
	}
	var filenames []string
	filenames = append(filenames, bp.GoFiles...)
	filenames = append(filenames, bp.CgoFiles...)

	pkgs := make(map[string]*types.Package)
	fset := token.NewFileSet()
	p := New(&ctxt, fset, pkgs)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := p.parseFiles(dir, filenames)
		if err != nil {
			b.Fatal(err)
		}
	}
}
