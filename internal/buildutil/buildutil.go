// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package buildutil exposes some useful internal functions of the go/build.
package buildutil

import (
	"bytes"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// TODO (CEV): rename
func ShortImport(ctxt *build.Context, path string) (string, bool) {
	if !goodOSArchFile(ctxt, filepath.Base(path), nil) {
		return "", false
	}
	var f io.ReadCloser
	var err error
	if fn := ctxt.OpenFile; fn != nil {
		f, err = fn(path)
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		return "", false
	}
	data, err := readImportsFast(f)
	f.Close()
	if err != nil {
		return "", false
	}
	if !shouldBuild(ctxt, data, nil) {
		return "", false
	}
	name, err := readPackageName(data)
	return name, err == nil
}

var slashslash = []byte("//")

// TODO (CEV): Add support for binary only packages
//
// shouldBuild reports whether it is okay to use this file,
// The rule is that in the file's leading run of // comments
// and blank lines, which must be followed by a blank line
// (to avoid including a Go package clause doc comment),
// lines beginning with '// +build' are taken as build directives.
//
// The file is accepted only if each such line lists something
// matching the file.  For example:
//
//	// +build windows linux
//
// marks the file as applicable only on Windows and Linux.
//
func shouldBuild(ctxt *build.Context, content []byte, allTags map[string]bool) bool {
	// Pass 1. Identify leading run of // comments and blank lines,
	// which must be followed by a blank line.
	end := 0
	p := content
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 { // Blank line
			end = len(content) - len(p)
			continue
		}
		if !bytes.HasPrefix(line, slashslash) { // Not comment line
			break
		}
	}
	content = content[:end]

	// Pass 2.  Process each line in the run.
	p = content
	allok := true
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, slashslash) {
			line = bytes.TrimSpace(line[len(slashslash):])
			if len(line) > 0 && line[0] == '+' {
				// Looks like a comment +line.
				f := strings.Fields(string(line))
				if f[0] == "+build" {
					ok := false
					for _, tok := range f[1:] {
						if match(ctxt, tok, allTags, false) {
							ok = true
						}
					}
					if !ok {
						allok = false
					}
				}
			}
		}
	}

	return allok
}

// match reports whether the name is one of:
//
//	$GOOS
//	$GOARCH
//	cgo (if cgo is enabled)
//	!cgo (if cgo is disabled)
//	ctxt.Compiler
//	!ctxt.Compiler
//	tag (if tag is listed in ctxt.BuildTags or ctxt.ReleaseTags)
//	!tag (if tag is not listed in ctxt.BuildTags or ctxt.ReleaseTags)
//	a comma-separated list of any of these
//
func match(ctxt *build.Context, name string, allTags map[string]bool, negated bool) bool {
	if name == "" {
		if allTags != nil {
			allTags[name] = true
		}
		return false
	}
	if i := strings.IndexByte(name, ','); i >= 0 {
		// comma-separated list
		ok1 := match(ctxt, name[:i], allTags, false)
		ok2 := match(ctxt, name[i+1:], allTags, false)
		return ok1 && ok2
	}
	if strings.HasPrefix(name, "!!") { // bad syntax, reject always
		return false
	}
	if strings.HasPrefix(name, "!") { // negation
		return len(name) > 1 && !match(ctxt, name[1:], allTags, true)
	}

	if allTags != nil {
		allTags[name] = !negated
	}

	// Tags must be letters, digits, underscores or dots.
	// Unlike in Go identifiers, all digits are fine (e.g., "386").
	for _, c := range name {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '.' {
			return false
		}
	}

	// special tags
	if ctxt.CgoEnabled && name == "cgo" {
		return true
	}
	if name == ctxt.GOOS || name == ctxt.GOARCH || name == ctxt.Compiler {
		return true
	}
	if ctxt.GOOS == "android" && name == "linux" {
		return true
	}

	// other tags
	for _, tag := range ctxt.BuildTags {
		if tag == name {
			return true
		}
	}
	for _, tag := range ctxt.ReleaseTags {
		if tag == name {
			return true
		}
	}

	return false
}

var knownOSList = sortStrings(strings.Fields(goosList))
var knownArchList = sortStrings(strings.Fields(goarchList))

func sortStrings(a []string) []string {
	sort.Strings(a)
	return a
}

// KnownOSList returns the known operating system values, sorted.
func KnownOSList() []string {
	s := make([]string, len(knownOSList))
	copy(s, knownOSList)
	return s
}

// KnownArchList returns the known architecture values, sorted.
func KnownArchList() []string {
	s := make([]string, len(knownArchList))
	copy(s, knownArchList)
	return s
}
