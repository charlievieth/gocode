package cache

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
)

func TestFindPkg(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pkgPaths := []string{
		"fmt",
		"github.com/mdempsky/gocode/pkg/cache",
		"does_not_exist",
	}
	for _, path := range pkgPaths {
		gotName, gotID := FindPkg(&build.Default, path, wd)
		wantName, wantID := gcexportdata.Find(path, wd)
		if testing.Verbose() {
			t.Logf("gcexportdata.Find(%q, %q) = %q, %q", path, filepath.Base(wd), wantName, wantID)
		}
		if gotName != wantName || gotID != wantID {
			t.Errorf("FindPkg(%q, %q) = %q, %q; want: %q, %q", path, wd,
				gotName, gotID, wantName, wantID)
		}
	}
}

func BenchmarkFindPkg(b *testing.B) {
	wd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	ctxt := &build.Default
	for i := 0; i < b.N; i++ {
		FindPkg(ctxt, "fmt", wd)
	}
}
