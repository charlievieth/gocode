package gocode

import "testing"

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
		ok := is_version(s)
		if ok != exp {
			t.Errorf("%q: got: %t want: %t", s, ok, exp)
		}
	}
}

func TestFixVersionedPkgName(t *testing.T) {
	tests := map[string]string{
		"github.com/posener/complete/v2":                "github.com/posener/complete",
		"github.com/posener/complete/v2/predict":        "github.com/posener/complete/predict",
		"github.com/posener/complete/v2/predict/v3/ugh": "github.com/posener/complete/v2/predict/ugh",
		"github.com/posener/complete":                   "github.com/posener/complete",
		"v1":                                            "v1",
		"":                                              "",
		".":                                             ".",
	}
	for pkg, exp := range tests {
		got, _ := fix_versioned_pkg_name(pkg)
		if got != exp {
			t.Errorf("%q: got: %q want: %q", pkg, got, exp)
		}
	}
}
