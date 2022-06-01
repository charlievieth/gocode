package cache

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/buildutil"
)

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
	if path == "" {
		return
	}

	var noext string
	switch {
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
