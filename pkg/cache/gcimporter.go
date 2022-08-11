package cache

import (
	"fmt"
	"go/build"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/buildutil"
)

// TODO:
//	1. fix versioned packages
//	2. use "golang.org/x/tools/go/packages" to find the build artifacts (slow)

// FindPkg is the same as golang.org/x/tools/go/gcexportdata.FindPkg
// but uses the provided build.Context instead of build.Default.
//
// FindPkg returns the filename and unique package id for an import
// path based on package information provided by build.Import (using
// the build.Default build.Context). A relative srcDir is interpreted
// relative to the current working directory.
// If no file was found, an empty filename is returned.
//
//
func FindPkg(ctxt *build.Context, path, srcDir string) (filename, id string) {
	// Example:
	// 	FindPkg("fmt", "$PWD") = "$GOROOT/pkg/darwin_arm64/fmt.a", "fmt"
	if path == "" {
		return
	}

	var noext string
	switch {
	case build.IsLocalImport(path):
		// "./x" -> "/this/directory/x.ext", "/this/directory/x"
		noext = filepath.Join(srcDir, path)
		id = noext

	case buildutil.IsAbsPath(ctxt, path):
		// for completeness only - go/build.Import
		// does not support absolute imports
		// "/x" -> "/x.ext", "/x"
		noext = path
		id = path

	// TODO: don't shadow noext and support "gccgo"
	case isStdLibPkg(path):
		if ctxt.Compiler == "gc" && ctxt.GOROOT != "" {
			suffix := ""
			if ctxt.InstallSuffix != "" {
				suffix = "_" + ctxt.InstallSuffix
			}
			noext := filepath.Join(ctxt.GOROOT, "pkg/"+ctxt.GOOS+"_"+ctxt.GOARCH+suffix, path)

			// try extensions
			for _, ext := range []string{".a", ".o"} {
				filename := noext + ext
				if isFile(ctxt, filename) {
					return filename, path
				}
			}
		}
		fallthrough

	default:
		// TODO(charlie): don't support this since we don't use from the
		// WD that we're called from.
		//
		// "x" -> "$GOPATH/pkg/$GOOS_$GOARCH/x.ext", "x"
		// Don't require the source files to be present.
		if abs, err := filepath.Abs(srcDir); err == nil { // see issue 14282
			srcDir = abs
		}
		bp, _ := ctxt.Import(path, srcDir, build.FindOnly|build.AllowBinary)
		if bp.PkgObj == "" {
			id = path // make sure we have an id to print in error message
			return
		}
		noext = strings.TrimSuffix(bp.PkgObj, ".a")
		id = bp.ImportPath
	}

	if false { // for debugging
		if path != id {
			fmt.Printf("%s -> %s\n", path, id)
		}
	}

	// try extensions
	for _, ext := range []string{".a", ".o"} {
		filename = noext + ext
		if isFile(ctxt, filename) {
			return
		}
	}

	filename = "" // not found
	return
}

func isFile(ctxt *build.Context, name string) bool {
	if fn := ctxt.OpenFile; fn != nil {
		f, err := fn(name)
		if err != nil {
			return false
		}
		f.Close()
		return true
	}
	fi, err := os.Stat(name)
	return err == nil && !fi.IsDir()
}

func isVersion(s string) bool {
	if len(s) < len("v2") || s[0] != 'v' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// removeImportPathVersion removes the version ("v2"), if any, from import path s
// and returns the un-versioned import path and if a version was found.
//
// This handles the problem of packages like "foo/bar/v2" being exported as
// "foo/bar.a".
func removeImportPathVersion(s string) (string, bool) {
	if !strings.Contains(s, "/v") {
		return s, false
	}
	if dir, base := path.Split(s); isVersion(base) {
		return strings.TrimSuffix(dir, "/"), true
	}
	a := strings.Split(s, "/")
	for i := len(a) - 1; i > 0; i-- {
		if isVersion(a[i]) {
			return strings.Join(append(a[:i], a[i+1:]...), "/"), true
		}
	}
	return s, false
}

// func FindPkg(ctxt *build.Context, path, srcDir string) (filename, id string) {
// 	filename, id = findPkg(ctxt, path, srcDir)
// 	if filename != "" {
// 		return
// 	}
// 	if p, ok := fixVersionedPkgName(path); ok {
// 		if filename, id := findPkg(ctxt, p, srcDir); filename != "" {
// 			return filename, id
// 		}
// 	}
// 	return filename, id
// }
