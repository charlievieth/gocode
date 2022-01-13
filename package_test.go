package gocode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIBin(t *testing.T) {
	for _, version := range []string{"go1.17", "go1.18"} {
		t.Run(version, func(t *testing.T) {
			filename := filepath.Join("testdata", version, "subtle.a")
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			m := new_package_file_cache(filename, "subtle")
			m.process_package_data(data)
		})
	}
}
