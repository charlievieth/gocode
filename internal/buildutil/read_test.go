// Copyright 2012 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildutil

import (
	"bytes"
	"go/build"
	"io"
	"strings"
	"testing"
)

const quote = "`"

type readTest struct {
	// Test input contains ℙ where readImports should stop.
	in  string
	err string
}

var readImportsTests = []readTest{
	{
		`package p`,
		"",
	},
	{
		`package p; import "x"`,
		"",
	},
	{
		`package p; import . "x"`,
		"",
	},
	{
		`package p; import "x";ℙvar x = 1`,
		"",
	},
	{
		`package p

		// comment

		import "x"
		import _ "x"
		import a "x"

		/* comment */

		import (
			"x" /* comment */
			_ "x"
			a "x" // comment
			` + quote + `x` + quote + `
			_ /*comment*/ ` + quote + `x` + quote + `
			a ` + quote + `x` + quote + `
		)
		import (
		)
		import ()
		import()import()import()
		import();import();import()

		ℙvar x = 1
		`,
		"",
	},
}

var readCommentsTests = []readTest{
	{
		`ℙpackage p`,
		"",
	},
	{
		`ℙpackage p; import "x"`,
		"",
	},
	{
		`ℙpackage p; import . "x"`,
		"",
	},
	{
		`// foo

		/* bar */

		/* quux */ // baz

		/*/ zot */

		// asdf
		ℙHello, world`,
		"",
	},
}

func testRead(t *testing.T, tests []readTest, read func(io.Reader) ([]byte, error)) {
	for i, tt := range tests {
		var in, testOut string
		j := strings.Index(tt.in, "ℙ")
		if j < 0 {
			in = tt.in
			testOut = tt.in
		} else {
			in = tt.in[:j] + tt.in[j+len("ℙ"):]
			testOut = tt.in[:j]
		}
		r := strings.NewReader(in)
		buf, err := read(r)
		if err != nil {
			if tt.err == "" {
				t.Errorf("#%d: err=%q, expected success (%q)", i, err, string(buf))
				continue
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("#%d: err=%q, expected %q", i, err, tt.err)
				continue
			}
			continue
		}
		if err == nil && tt.err != "" {
			t.Errorf("#%d: success, expected %q", i, tt.err)
			continue
		}

		out := string(buf)
		if out != testOut {
			t.Errorf("#%d: wrong output:\nhave %q\nwant %q\n", i, out, testOut)
		}
	}
}

func TestReadImports(t *testing.T) {
	testRead(t, readImportsTests, func(r io.Reader) ([]byte, error) { return readImports(r, true, nil) })
}

func TestReadComments(t *testing.T) {
	testRead(t, readCommentsTests, readComments)
}

var readFailuresTests = []readTest{
	{
		`package`,
		"syntax error",
	},
	{
		"package p\n\x00\nimport `math`\n",
		"unexpected NUL in input",
	},
	{
		`package p; import`,
		"syntax error",
	},
	{
		`package p; import "`,
		"syntax error",
	},
	{
		"package p; import ` \n\n",
		"syntax error",
	},
	{
		`package p; import "x`,
		"syntax error",
	},
	{
		`package p; import _`,
		"syntax error",
	},
	{
		`package p; import _ "`,
		"syntax error",
	},
	{
		`package p; import _ "x`,
		"syntax error",
	},
	{
		`package p; import .`,
		"syntax error",
	},
	{
		`package p; import . "`,
		"syntax error",
	},
	{
		`package p; import . "x`,
		"syntax error",
	},
	{
		`package p; import (`,
		"syntax error",
	},
	{
		`package p; import ("`,
		"syntax error",
	},
	{
		`package p; import ("x`,
		"syntax error",
	},
	{
		`package p; import ("x"`,
		"syntax error",
	},
}

func TestReadFailures(t *testing.T) {
	// Errors should be reported (true arg to readImports).
	testRead(t, readFailuresTests, func(r io.Reader) ([]byte, error) { return readImports(r, true, nil) })
}

func TestReadFailuresIgnored(t *testing.T) {
	// Syntax errors should not be reported (false arg to readImports).
	// Instead, entire file should be the output and no error.
	// Convert tests not to return syntax errors.
	tests := make([]readTest, len(readFailuresTests))
	copy(tests, readFailuresTests)
	for i := range tests {
		tt := &tests[i]
		if !strings.Contains(tt.err, "NUL") {
			tt.err = ""
		}
	}
	testRead(t, tests, func(r io.Reader) ([]byte, error) { return readImports(r, false, nil) })
}

var packageNameTests = []struct {
	src  string
	name string
	err  error
}{
	{
		src:  "package foo\n",
		name: "foo",
		err:  nil,
	},
	{
		src:  "// +build !windows\npackage foo\n",
		name: "foo",
		err:  nil,
	},
	{
		src:  "// +build !windows\npackagee extra_e\n",
		name: "",
		err:  errSyntax,
	},
	{
		src:  "/* foo */ // +build !windows\npackage foo\n",
		name: "foo",
		err:  nil,
	},
	{
		src: `// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

` + "// +build !go1.5" + `

// For all Go versions other than 1.5 use the Import and ImportDir functions
// declared in go/build.

package buildutil

import "go/build"
`,
		name: "buildutil",
		err:  nil,
	},
}

func TestReadPackageName(t *testing.T) {
	for i, x := range packageNameTests {
		name, err := readPackageName([]byte(x.src))
		if err != x.err {
			t.Errorf("%d error (%v): %v", i, x.err, err)
		}
		if err != nil {
			continue
		}
		if name != x.name {
			t.Errorf("%d name (%s): %s", i, x.name, name)
		}
	}
}

func BenchmarkReadPackageName_Short(b *testing.B) {
	src := []byte("package foo")
	for i := 0; i < b.N; i++ {
		readPackageName(src)
	}
}

func BenchmarkReadPackageName_Medium(b *testing.B) {
	src := []byte("// +build linux\npackage foo")
	for i := 0; i < b.N; i++ {
		readPackageName(src)
	}
}

const LongPackageHeader = `// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

` + "// +build !go1.5" + `

/*
  For all Go versions other than 1.5 use the Import and ImportDir functions
  declared in go/build.
*/

package buildutil

import "go/build"`

var LongPackageHeaderBytes = []byte(LongPackageHeader)

func BenchmarkReadPackageName_Long(b *testing.B) {
	for i := 0; i < b.N; i++ {
		readPackageName(LongPackageHeaderBytes)
	}
}

func BenchmarkReadImports_Long(b *testing.B) {
	r := bytes.NewReader(LongPackageHeaderBytes)
	for i := 0; i < b.N; i++ {
		readImports(r, true, nil)
		r.Seek(0, 0)
	}
}

func BenchmarkShortImport_Long(b *testing.B) {
	const filename = "go_darwin_amd64.go"
	rc := &nopReadCloser{s: LongPackageHeaderBytes}
	ctxt := build.Default
	ctxt.OpenFile = func(path string) (io.ReadCloser, error) {
		if path != filename {
			panic("invalid filename: " + path)
		}
		rc.Reset()
		return rc, nil
	}
	for i := 0; i < b.N; i++ {
		ShortImport(&ctxt, filename)
	}
}
