package utils

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestReaddirnames(t *testing.T) {
	tempdir := t.TempDir()

	want := []string{
		"1.txt", "2.txt", "3.txt", "4.txt",
	}
	for _, name := range want {
		if err := os.WriteFile(tempdir+"/"+name, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	testReaddirnames := func(t *testing.T, want []string) {
		t.Helper()
		got, err := Readdirnames(tempdir)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got: %q; want: %q", got, want)
		}

		// Test that we return the cached entry
		got2, err := Readdirnames(tempdir)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got2)
		if &got[0] != &got2[0] {
			t.Errorf("got: %p; want: %p", &got[0], &got2[0])
		}
	}

	// Remove file
	if err := os.Remove(filepath.Join(tempdir, want[0])); err != nil {
		t.Fatal(err)
	}
	want = want[1:]
	testReaddirnames(t, want)

	// Add file
	if err := os.WriteFile(tempdir+"/"+"foo.txt", []byte("foo.txt"), 0644); err != nil {
		t.Fatal(err)
	}
	want = append(want, "foo.txt")
	testReaddirnames(t, want)

	// Modify file
	if err := os.WriteFile(tempdir+"/"+want[0], []byte("NEW DATA"), 0644); err != nil {
		t.Fatal(err)
	}
	testReaddirnames(t, want)
}

func BenchmarkReaddirnames(b *testing.B) {
	wd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	dir, err := filepath.Abs(filepath.Join(wd, "testdata/bench/readdirnames"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_, err := Readdirnames(dir)
		if err != nil {
			b.Fatal(err)
		}
	}
}
