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
		"github.com/charlievieth/buildutil",
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

func TestIsVersion(t *testing.T) {
	tests := map[string]bool{
		"":      false,
		"v1":    true,
		"v123":  true,
		"v123b": false,
		"v":     false,
		"a12":   false,
	}
	for s, exp := range tests {
		ok := isVersion(s)
		if ok != exp {
			t.Errorf("%q: got: %t want: %t", s, ok, exp)
		}
	}
}

func TestRemoveImportPathVersion(t *testing.T) {
	var tests = []struct {
		path, want string
	}{
		{"github.com/posener/complete/v2", "github.com/posener/complete"},
		{"github.com/posener/complete/v2/predict", "github.com/posener/complete/predict"},
		{"github.com/posener/complete/v2/predict/v3/ugh", "github.com/posener/complete/v2/predict/ugh"},
		{"github.com/posener/complete", "github.com/posener/complete"},
		{"v1", "v1"},
		{"", ""},
		{".", "."},
	}
	for _, test := range tests {
		wantChange := test.path != test.want
		got, changed := removeImportPathVersion(test.path)
		if got != test.want || changed != wantChange {
			t.Errorf("fixVersionedPkgName(%q) = %q, %t: want: %q, %t",
				test.path, got, changed, test.want, wantChange)
		}
	}
}

func BenchmarkFixVersionedPkgName(b *testing.B) {
	// "github.com/posener/complete/v2"
	// "github.com/posener/complete/v2/predict"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// versionRe.MatchString("github.com/posener/complete/v2")
		_, _ = removeImportPathVersion("github.com/posener/complete/v2")
		// _, _ = fixVersionedPkgName("github.com/posener/complete")
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
