// Copyright 2012 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildutil

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

type importReader struct {
	b    *bufio.Reader
	buf  []byte
	peek byte
	err  error
	eof  bool
	nerr int
}

func isIdent(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '_' || c >= 0x80
}

var (
	errSyntax = errors.New("syntax error")
	errNUL    = errors.New("unexpected NUL in input")
)

// syntaxError records a syntax error, but only if an I/O error has not already been recorded.
func (r *importReader) syntaxError() {
	if r.err == nil {
		r.err = errSyntax
	}
}

// readByte reads the next byte from the input, saves it in buf, and returns it.
// If an error occurs, readByte records the error in r.err and returns 0.
func (r *importReader) readByte() byte {
	c, err := r.b.ReadByte()
	if err == nil {
		r.buf = append(r.buf, c)
		if c == 0 {
			err = errNUL
		}
	}
	if err != nil {
		if err == io.EOF {
			r.eof = true
		} else if r.err == nil {
			r.err = err
		}
		c = 0
	}
	return c
}

// peekByte returns the next byte from the input reader but does not advance beyond it.
// If skipSpace is set, peekByte skips leading spaces and comments.
func (r *importReader) peekByte(skipSpace bool) byte {
	if r.err != nil {
		if r.nerr++; r.nerr > 10000 {
			panic("go/build: import reader looping")
		}
		return 0
	}

	// Use r.peek as first input byte.
	// Don't just return r.peek here: it might have been left by peekByte(false)
	// and this might be peekByte(true).
	c := r.peek
	if c == 0 {
		c = r.readByte()
	}
	for r.err == nil && !r.eof {
		if skipSpace {
			// For the purposes of this reader, semicolons are never necessary to
			// understand the input and are treated as spaces.
			switch c {
			case ' ', '\f', '\t', '\r', '\n', ';':
				c = r.readByte()
				continue

			case '/':
				c = r.readByte()
				if c == '/' {
					for c != '\n' && r.err == nil && !r.eof {
						c = r.readByte()
					}
				} else if c == '*' {
					var c1 byte
					for (c != '*' || c1 != '/') && r.err == nil {
						if r.eof {
							r.syntaxError()
						}
						c, c1 = c1, r.readByte()
					}
				} else {
					r.syntaxError()
				}
				c = r.readByte()
				continue
			}
		}
		break
	}
	r.peek = c
	return r.peek
}

// nextByte is like peekByte but advances beyond the returned byte.
func (r *importReader) nextByte(skipSpace bool) byte {
	c := r.peekByte(skipSpace)
	r.peek = 0
	return c
}

// readKeyword reads the given keyword from the input.
// If the keyword is not present, readKeyword records a syntax error.
func (r *importReader) readKeyword(kw string) {
	r.peekByte(true)
	for i := 0; i < len(kw); i++ {
		if r.nextByte(false) != kw[i] {
			r.syntaxError()
			return
		}
	}
	if isIdent(r.peekByte(false)) {
		r.syntaxError()
	}
}

// readIdent reads an identifier from the input.
// If an identifier is not present, readIdent records a syntax error.
func (r *importReader) readIdent() {
	c := r.peekByte(true)
	if !isIdent(c) {
		r.syntaxError()
		return
	}
	for isIdent(r.peekByte(false)) {
		r.peek = 0
	}
}

// readString reads a quoted string literal from the input.
// If an identifier is not present, readString records a syntax error.
func (r *importReader) readString(save *[]string) {
	switch r.nextByte(true) {
	case '`':
		start := len(r.buf) - 1
		for r.err == nil {
			if r.nextByte(false) == '`' {
				if save != nil {
					*save = append(*save, string(r.buf[start:]))
				}
				break
			}
			if r.eof {
				r.syntaxError()
			}
		}
	case '"':
		start := len(r.buf) - 1
		for r.err == nil {
			c := r.nextByte(false)
			if c == '"' {
				if save != nil {
					*save = append(*save, string(r.buf[start:]))
				}
				break
			}
			if r.eof || c == '\n' {
				r.syntaxError()
			}
			if c == '\\' {
				r.nextByte(false)
			}
		}
	default:
		r.syntaxError()
	}
}

// readImport reads an import clause - optional identifier followed by quoted string -
// from the input.
func (r *importReader) readImport(imports *[]string) {
	c := r.peekByte(true)
	if c == '.' {
		r.peek = 0
	} else if isIdent(c) {
		r.readIdent()
	}
	r.readString(imports)
}

// readComments is like ioutil.ReadAll, except that it only reads the leading
// block of comments in the file.
func readComments(f io.Reader) ([]byte, error) {
	r := &importReader{b: bufio.NewReader(f)}
	r.peekByte(true)
	if r.err == nil && !r.eof {
		// Didn't reach EOF, so must have found a non-space byte. Remove it.
		r.buf = r.buf[:len(r.buf)-1]
	}
	return r.buf, r.err
}

// readImports is like ioutil.ReadAll, except that it expects a Go file as input
// and stops reading the input once the imports have completed.
func readImports(f io.Reader, reportSyntaxError bool, imports *[]string) ([]byte, error) {
	r := &importReader{b: bufio.NewReader(f)}

	r.readKeyword("package")
	r.readIdent()
	for r.peekByte(true) == 'i' {
		r.readKeyword("import")
		if r.peekByte(true) == '(' {
			r.nextByte(false)
			for r.peekByte(true) != ')' && r.err == nil {
				r.readImport(imports)
			}
			r.nextByte(false)
		} else {
			r.readImport(imports)
		}
	}

	// If we stopped successfully before EOF, we read a byte that told us we were done.
	// Return all but that last byte, which would cause a syntax error if we let it through.
	if r.err == nil && !r.eof {
		return r.buf[:len(r.buf)-1], nil
	}

	// If we stopped for a syntax error, consume the whole file so that
	// we are sure we don't change the errors that go/parser returns.
	if r.err == errSyntax && !reportSyntaxError {
		r.err = nil
		for r.err == nil && !r.eof {
			r.readByte()
		}
	}

	return r.buf, r.err
}

// readImportsFast is like readImports, except that it stops reading after the
// package clause.
func readImportsFast(f io.Reader) ([]byte, error) {
	r := importReader{b: bufio.NewReader(f)}

	r.readKeyword("package")
	r.readIdent()
	r.readByte()

	// If we stopped successfully before EOF, we read a byte that told us we were done.
	// Return all but that last byte, which would cause a syntax error if we let it through.
	if r.err == nil && !r.eof {
		return r.buf[:len(r.buf)-1], nil
	}
	return r.buf, r.err
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

var packageBytes = []byte("package")

func readPackageName(b []byte) (string, error) {
	const minLen = len("package _\n")

	// trim left whitespace
	for len(b) > 0 && isSpace(b[0]) {
		b = b[1:]
	}

Loop:
	for len(b) >= minLen {
		c := b[0]
		switch c {
		case ' ', '\t', '\n', '\r', '\f', ';':
			b = b[1:]
		case '/':
			b = b[1:]
			c = b[0]
			switch c {
			case '/':
				n := bytes.IndexByte(b, '\n')
				if n == -1 || n == len(b)-1 {
					return "", errSyntax
				}
				b = b[n+1:]
			case '*':
				n := bytes.Index(b, []byte("*/"))
				if n == -1 || n == len(b)-2 {
					return "", errSyntax
				}
				b = b[n+2:]
			default:
				return "", errSyntax
			}
		default:
			break Loop
		}
	}

	if len(b) >= minLen && bytes.HasPrefix(b, packageBytes) {
		b = b[len("package"):]
		if !isSpace(b[0]) {
			return "", errSyntax
		}
		for len(b) > 0 && isSpace(b[0]) {
			b = b[1:]
		}
		i := 0
		for ; i < len(b) && isIdent(b[i]); i++ {
		}
		if i == 0 || i == len(b) {
			return "", errSyntax
		}
		return string(b[:i]), nil
	}

	return "", errSyntax
}
