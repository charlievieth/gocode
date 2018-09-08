// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildutil

import (
	"go/build"
	"strings"
)

// goodOSArchFile returns false if the name contains a $GOOS or $GOARCH
// suffix which does not match the current system.
// The recognized name formats are:
//
//     name_$(GOOS).*
//     name_$(GOARCH).*
//     name_$(GOOS)_$(GOARCH).*
//     name_$(GOOS)_test.*
//     name_$(GOARCH)_test.*
//     name_$(GOOS)_$(GOARCH)_test.*
//
// An exception: if GOOS=android, then files with GOOS=linux are also matched.
func goodOSArchFile(ctxt *build.Context, name string, allTags map[string]bool) bool {
	if dot := strings.IndexByte(name, '.'); dot != -1 {
		name = name[:dot]
	}

	// Before Go 1.4, a file called "linux.go" would be equivalent to having a
	// build tag "linux" in that file. For Go 1.4 and beyond, we require this
	// auto-tagging to apply only to files with a non-empty prefix, so
	// "foo_linux.go" is tagged but "linux.go" is not. This allows new operating
	// systems, such as android, to arrive without breaking existing code with
	// innocuous source code in "android.go". The easiest fix: cut everything
	// in the name before the initial _.
	i := strings.IndexByte(name, '_')
	if i < 0 {
		return true
	}
	name = name[i:] // ignore everything before first _

	l := strings.Split(name, "_")
	if n := len(l); n > 0 && l[n-1] == "test" {
		l = l[:n-1]
	}
	n := len(l)
	if n >= 2 && knownOS[l[n-2]] && knownArch[l[n-1]] {
		if allTags != nil {
			allTags[l[n-2]] = true
			allTags[l[n-1]] = true
		}
		if l[n-1] != ctxt.GOARCH {
			return false
		}
		if ctxt.GOOS == "android" && l[n-2] == "linux" {
			return true
		}
		return l[n-2] == ctxt.GOOS
	}
	if n >= 1 && knownOS[l[n-1]] {
		if allTags != nil {
			allTags[l[n-1]] = true
		}
		if ctxt.GOOS == "android" && l[n-1] == "linux" {
			return true
		}
		return l[n-1] == ctxt.GOOS
	}
	if n >= 1 && knownArch[l[n-1]] {
		if allTags != nil {
			allTags[l[n-1]] = true
		}
		return l[n-1] == ctxt.GOARCH
	}
	return true
}

var knownOS = make(map[string]bool)
var knownArch = make(map[string]bool)
var knownReleaseTag = make(map[string]bool)

func init() {
	for _, v := range strings.Fields(goosList) {
		knownOS[v] = true
	}
	for _, v := range strings.Fields(goarchList) {
		knownArch[v] = true
	}
	for _, v := range build.Default.ReleaseTags {
		knownReleaseTag[v] = true
	}
}
