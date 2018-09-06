// Copyright 2012 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildutil

import (
	"go/build"
	"io"
	"strings"
	"testing"
)

const quote = "`"

type readFastTest struct {
	// Test input contains ℙ where readImports should stop.
	in  string
	out string
	err string
}

var readFastTests = []readFastTest{
	{
		"package p",
		"package p",
		"",
	},
	{
		"package p;",
		"package p;",
		"",
	},
	{
		"// +build linux\npackage p;",
		"// +build linux\npackage p;",
		"",
	},
	{
		"// foo /\npackage p;",
		"// foo /\npackage p;",
		"",
	},
	{
		"/* foo */\npackage p;",
		"/* foo */\npackage p;",
		"",
	},
	{
		"/* foo */package p",
		"/* foo */package p",
		"",
	},
	{
		`/*
		  * a
		  * long
		  * comment
		 */
		 // foo
	     package p
		`,
		`/*
		  * a
		  * long
		  * comment
		 */
		 // foo
	     ` + "package p\n",
		"",
	},
	{
		"// Copyright 2012 The Go Authors.  All rights reserved.\n" +
			"//+build linux\n" +
			"package p\n" +
			"import .\n",
		"// Copyright 2012 The Go Authors.  All rights reserved.\n" +
			"//+build linux\n" +
			"package p\n",
		"",
	},
	{
		`package p; import "x"`,
		"package p;",
		"",
	},
	{
		`package p; import . "x"`,
		"package p;",
		"",
	},
	// syntax error in import clause, but not package
	{
		"package p; import",
		"package p;",
		"",
	},
	{
		"packge misspelled",
		"",
		"syntax error",
	},
	{
		"",
		"",
		"syntax error",
	},
	{
		"package",
		"",
		"syntax error",
	},
	{
		"/ bad comment\npackage p;",
		"",
		"syntax error",
	},
	{
		"package p\n\x00\nimport `math`\n",
		"p",
		"unexpected NUL in input",
	},
}

func TestReadImportsFast(t *testing.T) {
	for i, tt := range readFastTests {
		buf, err := readImportsFast(strings.NewReader(tt.in))
		if err != nil {
			if tt.err == "" {
				t.Errorf("#%d (%+v): err=%q, expected success (%q)", i, tt, err, string(buf))
				continue
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("#%d (%+v): err=%q, expected %q", i, tt, err, tt.err)
				continue
			}
			continue
		}
		if err == nil && tt.err != "" {
			t.Errorf("#%d (%+v): success, expected %q", i, tt, tt.err)
			continue
		}
		out := string(buf)
		if out != tt.out {
			t.Errorf("#%d (%+v): wrong output:\nhave %q\nwant %q\n", i, tt, out, tt.out)
		}
	}
}

var packageNameTests = []struct {
	src  string
	name string
	err  error
}{
	{
		src:  "package foo\n",
		name: "foo",
	},
	{
		src:  "package foo;",
		name: "foo",
	},
	{
		src:  "// +build !windows\npackage foo\n",
		name: "foo",
	},
	{
		src:  "/* foo */ // +build !windows\npackage foo\n",
		name: "foo",
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
	},
	// errors
	{
		src:  "// +build !windows\npackagee extra_e\n",
		name: "",
		err:  errSyntax,
	},
	{
		src:  "package ;\n",
		name: "",
		err:  errSyntax,
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
		ShouldBuild(&ctxt, filename)
	}
}
