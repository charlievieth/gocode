package buildutil

import (
	"go/build"
	"runtime"
	"testing"
)

var (
	thisOS    = runtime.GOOS
	thisArch  = runtime.GOARCH
	otherOS   = anotherOS()
	otherArch = anotherArch()
)

func anotherOS() string {
	if thisOS != "darwin" {
		return "darwin"
	}
	return "linux"
}

func anotherArch() string {
	if thisArch != "amd64" {
		return "amd64"
	}
	return "386"
}

type GoodFileTest struct {
	name   string
	result bool
}

var tests = []GoodFileTest{
	{"file.go", true},
	{"file.c", true},
	{"file_foo.go", true},
	{"file_" + thisArch + ".go", true},
	{"file_" + otherArch + ".go", false},
	{"file_" + thisOS + ".go", true},
	{"file_" + otherOS + ".go", false},
	{"file_" + thisOS + "_" + thisArch + ".go", true},
	{"file_" + otherOS + "_" + thisArch + ".go", false},
	{"file_" + thisOS + "_" + otherArch + ".go", false},
	{"file_" + otherOS + "_" + otherArch + ".go", false},
	{"file_foo_" + thisArch + ".go", true},
	{"file_foo_" + otherArch + ".go", false},
	{"file_" + thisOS + ".c", true},
	{"file_" + otherOS + ".c", false},
}

func TestGoodOSArch(t *testing.T) {
	ctxt := build.Default
	for _, test := range tests {
		if goodOSArchFile(&ctxt, test.name, make(map[string]bool)) != test.result {
			t.Fatalf("goodOSArchFile(%q) != %v", test.name, test.result)
		}
	}
}

var benchmark = [...]string{
	"file.go",
	"file_foo.go",
	"file_" + thisArch + ".go",
	"file_" + otherArch + ".go",
	"file_" + thisOS + ".go",
	"file_" + otherOS + ".go",
	"file_" + thisOS + "_" + thisArch + ".go",
	"file_" + otherOS + "_" + thisArch + ".go",
	"file_" + thisOS + "_" + otherArch + "_test.go",
	"file_" + otherOS + "_" + otherArch + "_test.go",
	"file_foo_" + thisArch + ".go",
	"file_foo_" + otherArch + ".go",
	"file_foo_" + thisArch + "_test.go",
	"file_foo_" + otherArch + "_test.go",
	"long_file_name_foo_bar_" + otherArch + "_test.go",
	"long_file_name_foo_bar_" + otherArch + ".go",
}

func BenchmarkGoodOSArch(b *testing.B) {
	ctxt := build.Default
	for i := 0; i < b.N; i++ {
		goodOSArchFile(&ctxt, benchmark[i%len(benchmark)], nil)
	}
}
