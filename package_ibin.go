package gocode

/* TODO: update this for go1.18

Commits:
  * https://github.com/golang/go/commit/fd2f4b58b34effdbdacba41e0c36fa701c6dfa27
  * https://github.com/golang/tools/commit/52e95274200fabcac2b99138b37c22fec3ae0460
  * https://github.com/golang/go/issues/47654

git diff --no-index \
	/Users/cvieth/go/src/github.com/charlievieth/gocode/package_ibin.go \
	/Users/cvieth/Projects/go-pkg-src/dev/go/src/go/internal/gcimporter/iimport.go

TODO:
	* Add tests from golang.org/x/tools/go/internal/gcimporter/iexport_test.go
*/

//-------------------------------------------------------------------------
// gc_ibin_parser
//
// The following part of the code may contain portions of the code from the Go
// standard library, which tells me to retain their copyright notice:
//
// Copyright (c) 2012 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//-------------------------------------------------------------------------

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"io"
	"sort"
	"strings"
)

type intReader struct {
	*bytes.Reader
}

func (r *intReader) int64() int64 {
	i, err := binary.ReadVarint(r.Reader)
	if err != nil {
		panic(fmt.Sprintf("read varint error: %v", err))
	}
	return i
}

func (r *intReader) uint64() uint64 {
	i, err := binary.ReadUvarint(r.Reader)
	if err != nil {
		panic(fmt.Sprintf("read varint error: %v", err))
	}
	return i
}

// Keep this in sync with constants in iexport.go.
const (
	iexportVersionGo1_11   = 0
	iexportVersionPosCol   = 1
	iexportVersionGenerics = 2
	iexportVersionGo1_18   = 2

	iexportVersionCurrent = 2
)

type gc_ibin_parser struct {
	data          []byte
	exportVersion int64
	version       int
	callback      func(pkg string, decl ast.Decl)
	pfc           *package_file_cache

	stringData  []byte
	stringCache map[uint64]string
	declData    []byte
	typCache    map[uint64]*ibinType
	pkgCache    map[uint64]ibinPackage

	// TOOD: see if we need this field
	tparamIndex map[ident]*ibinType
}

type ibinPackage struct {
	fullName string
	index    map[string]uint64
	declTyp  map[string]*ibinType
}

func (p *gc_ibin_parser) typAt(off uint64) *ibinType {
	if t, ok := p.typCache[off]; ok {
		return t
	}

	if off < predeclReserved {
		panic(fmt.Sprintf("predeclared type missing from cache: %v", off))
	}

	r := &bimportReader{p: p, version: p.version}
	r.declReader.Reset(p.declData[off-predeclReserved:])
	t := r.doType()
	p.typCache[off] = t
	return t
}

func (p *gc_ibin_parser) stringAt(off uint64) string {
	if s, ok := p.stringCache[off]; ok {
		return s
	}

	slen, n := binary.Uvarint(p.stringData[off:])
	if n <= 0 {
		panic(fmt.Sprintf("varint failed"))
	}
	spos := off + uint64(n)
	s := string(p.stringData[spos : spos+slen])
	p.stringCache[off] = s
	return s
}

func (p *gc_ibin_parser) pkgAt(off uint64) ibinPackage {
	if pkg, ok := p.pkgCache[off]; ok {
		return pkg
	}
	path := p.stringAt(off)
	panic(fmt.Sprintf("missing package %q", path))
}

func (p *gc_ibin_parser) init(data []byte, pfc *package_file_cache) {
	p.data = data
	p.version = -1
	p.pfc = pfc
	p.stringCache = make(map[uint64]string)
	p.pkgCache = make(map[uint64]ibinPackage)
}

// TODO: error or report version skew ???
func (p *gc_ibin_parser) parse_export(callback func(string, ast.Decl)) {
	p.callback = callback

	r := &intReader{bytes.NewReader(p.data)}
	version := int64(r.uint64())
	p.exportVersion = version
	p.version = int(version)
	switch version {
	case iexportVersionGo1_18, iexportVersionPosCol, iexportVersionGo1_11:
		// ok
	default:
		panic(fmt.Errorf("unknown export format version %d", version))
	}

	sLen := int64(r.uint64())
	dLen := int64(r.uint64())
	whence, _ := r.Seek(0, io.SeekCurrent)
	p.stringData = p.data[whence : whence+sLen]

	p.declData = p.data[whence+sLen : whence+sLen+dLen]
	r.Seek(sLen+dLen, io.SeekCurrent)

	// built-in types
	p.typCache = make(map[uint64]*ibinType, len(predeclaredIBinTypes))
	for i := range predeclaredIBinTypes {
		p.typCache[uint64(i)] = &predeclaredIBinTypes[i]
	}
	// TOOD: see if we need this field
	p.tparamIndex = make(map[ident]*ibinType)

	pkgs := make([]ibinPackage, r.uint64())
	for i := range pkgs {
		pkgPathOff := r.uint64()
		pkgPath := p.stringAt(pkgPathOff)
		pkgName := p.stringAt(r.uint64())
		_ = r.uint64() // package height; unused here

		var fullName string
		if pkgPath == "" {
			// imported package
			fullName = "!" + p.pfc.name + "!" + pkgName
			p.pfc.defalias = fullName[strings.LastIndex(fullName, "!")+1:]
		} else {
			// third party import
			fullName = "!" + pkgPath + "!" + pkgName
			p.pfc.add_package_to_scope(fullName, pkgPath)
		}

		// list of package entities pointing at decl data by name
		nSyms := int(r.uint64())
		index := make(map[string]uint64, nSyms)
		for ; nSyms > 0; nSyms-- {
			name := p.stringAt(r.uint64())
			index[name] = r.uint64()
		}

		pkg := ibinPackage{fullName, index, make(map[string]*ibinType)}
		p.pkgCache[pkgPathOff] = pkg
		pkgs[i] = pkg
	}

	n := 0
	for _, pkg := range pkgs {
		if len(pkg.index) > n {
			n = len(pkg.index)
		}
	}
	names := make([]string, n)
	for _, pkg := range pkgs {
		names = names[:0]
		for name := range pkg.index {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			p.doDecl(pkg, name)
		}
	}
}

func (p *gc_ibin_parser) doDecl(pkg ibinPackage, name string) *ibinType {
	if t, ok := pkg.declTyp[name]; ok { // already processed
		return t
	}

	off, ok := pkg.index[name]
	if !ok {
		panic(fmt.Sprintf("%q not in %q", name, pkg.fullName))
	}

	r := &bimportReader{p: p, currPkg: pkg, version: p.version}
	r.declReader.Reset(p.declData[off:])

	t := r.obj(name)
	pkg.declTyp[name] = t
	return t
}

type ibinType struct {
	typ ast.Expr
	und *ibinType
}

func (t *ibinType) underlying() ast.Expr {
	for t.und != nil {
		t = t.und
	}
	return t.typ
}

type bimportReader struct {
	p          *gc_ibin_parser
	declReader bytes.Reader
	currPkg    ibinPackage
	version    int
}

func (r *bimportReader) obj(name string) *ibinType {
	tag := r.byte()
	r.pos()

	switch tag {
	case 'A':
		typ := r.typ()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok:   token.TYPE,
			Specs: []ast.Spec{typeAliasSpec(name, typ.typ)},
		})
		return typ
	case 'C':
		typ := r.value()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.CONST,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent(name)},
					Type:   typ.typ,
					Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}},
				},
			},
		})
		return typ
	case 'F', 'G':
		var tparams []*ibinType
		if tag == 'G' {
			tparams = r.tparamList()
		}
		sig := r.signature(nil, tparams)
		// TODO: do we actually need tparams ???
		r.p.callback(r.currPkg.fullName, &ast.FuncDecl{
			Name: ast.NewIdent(name),
			Type: sig,
		})
		return &ibinType{typ: sig}
	case 'T', 'U':
		// Types can be recursive. We need to setup a stub
		// declaration before recursing.
		t := &ibinType{typ: &ast.SelectorExpr{
			X:   ast.NewIdent(r.currPkg.fullName),
			Sel: ast.NewIdent(name),
		}}
		// TODO: figure out how to store/represent union types
		//
		// From: go/internal/gcimporter/iimport.go
		//
		// Declare obj before calling r.tparamList, so the new type name is recognized
		// if used in the constraint of one of its own typeparams (see #48280).
		r.currPkg.declTyp[name] = t
		if tag == 'U' {
			_ = r.tparamList()
		}

		t.und = r.p.typAt(r.uint64()) // underlying

		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{
				&ast.TypeSpec{
					Name: ast.NewIdent(name),
					Type: t.und.typ,
				},
			},
		})

		if !isInterface(t.und.typ) {
			// read associated methods
			for n := r.uint64(); n > 0; n-- {
				r.pos() // mpos
				mname := r.ident()
				recv := &ast.FieldList{List: []*ast.Field{r.param()}}

				// TODO: handle targs

				msig := r.signature(nil, nil)
				strip_method_receiver(recv)
				r.p.callback(r.currPkg.fullName, &ast.FuncDecl{
					Recv: recv,
					Name: ast.NewIdent(mname),
					Type: msig,
				})
			}
		}
		return t

	case 'P':
		// We need to "declare" a typeparam in order to have a name that
		// can be referenced recursively (if needed) in the type param's
		// bound.
		if r.p.exportVersion < iexportVersionGenerics {
			panic("unexpected type param type")
		}
		// Remove the "path" from the type param name that makes it unique
		ix := strings.LastIndex(name, ".")
		if ix < 0 {
			panic("missing path for type param")
		}
		// TODO: determine the correct ast type
		tn := &ibinType{typ: &ast.SelectorExpr{
			X:   ast.NewIdent(r.currPkg.fullName),
			Sel: ast.NewIdent(name[ix+1:]),
		}}
		t := tn // WARN
		// To handle recursive references to the typeparam within its
		// bound, save the partial type in tparamIndex before reading the bounds.
		id := ident{r.currPkg.fullName, name}
		r.p.tparamIndex[id] = t

		var implicit bool
		if r.p.exportVersion >= iexportVersionGo1_18 {
			implicit = r.bool()
		}
		constraint := r.typ()
		_ = constraint // WARN
		if implicit {
			// iface, _ := constraint.(*types.Interface)
			// if iface == nil {
			// 	errorf("non-interface constraint marked implicit")
			// }
			// iface.MarkImplicit()
		}
		// t.SetConstraint(constraint)

		// WARN: callback !!!
		return t

	case 'V':
		typ := r.typ()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names: []*ast.Ident{ast.NewIdent(name)},
					Type:  typ.typ,
				},
			},
		})

		return typ
	default:
		panic(fmt.Sprintf("unexpected tag: %v name: %q", tag, name))
	}
}

type ident struct {
	pkg  string
	name string
}

const predeclReserved = 32

type itag uint64

const (
	// Types
	definedType itag = iota
	pointerType
	sliceType
	arrayType
	chanType
	mapType
	signatureType
	structType
	interfaceType
	typeParamType
	instanceType
	unionType
)

// we don't care about that, let's just skip it
func (r *bimportReader) pos() {
	if r.version >= 1 {
		r.posv1()
	} else {
		r.posv0()
	}
}

func (r *bimportReader) posv0() {
	if r.int64() != deltaNewFile {
		// pass
	} else if l := r.int64(); l == -1 {
		// pass
	} else {
		r.string()
	}
}

func (r *bimportReader) posv1() {
	delta := r.int64()
	if delta&1 != 0 {
		delta = r.int64()
		if delta&1 != 0 {
			r.string()
		}
	}
}

func (r *bimportReader) value() *ibinType {
	t := r.typ()
	if r.p.exportVersion >= iexportVersionGo1_18 {
		// TODO: add support for using the kind
		_ = constant.Kind(r.int64())
	}

	typ := t.underlying()
	ident, ok := typ.(*ast.Ident)
	if !ok {
		panic(fmt.Sprintf("unexpected type: %v", typ))
	}

	switch ident.Name {
	case "bool", "&untypedBool&":
		r.bool()
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16",
		"uint32", "uint64", "uintptr", "byte", "rune", "&untypedInt&", "&untypedRune&":
		r.mpint(ident)
	case "float32", "float64", "&untypedFloat&":
		r.mpfloat(ident)
	case "complex64", "complex128", "&untypedComplex&":
		r.mpfloat(ident)
		r.mpfloat(ident)
	case "string", "&untypedString&":
		r.string()
	default:
		panic(fmt.Sprintf("unexpected type: %v", typ))
	}
	return t
}

func intSize(typ *ast.Ident) (signed bool, maxBytes uint) {
	if typ.Name[0] == '&' { // untyped
		return true, 64
	}

	switch typ.Name {
	case "float32", "complex64":
		return true, 3
	case "float64", "complex128":
		return true, 7
	case "int8":
		return true, 1
	case "int16":
		return true, 2
	case "int32", "rune":
		return true, 4
	case "int64", "int":
		return true, 8
	case "uint8", "byte":
		return false, 1
	case "uint16":
		return false, 2
	case "uint32":
		return false, 4
	case "uint64", "uint", "uintptr":
		return false, 8
	}
	panic(fmt.Sprintf("unexpected type: %v", typ))
}

func (r *bimportReader) mpint(typ *ast.Ident) constant.Value {
	signed, maxBytes := intSize(typ)

	maxSmall := 256 - maxBytes
	if signed {
		maxSmall = 256 - 2*maxBytes
	}
	if maxBytes == 1 {
		maxSmall = 256
	}

	n, _ := r.declReader.ReadByte()
	if uint(n) < maxSmall {
		v := int64(n)
		if signed {
			v >>= 1
			if n&1 != 0 {
				v = ^v
			}
		}
		return constant.MakeInt64(v)
	}

	v := -n
	if signed {
		v = -(n &^ 1) >> 1
	}
	if v < 1 || uint(v) > maxBytes {
		panic(fmt.Sprintf("weird decoding: %v, %v => %v", n, signed, v))
	}

	buf := make([]byte, v)
	io.ReadFull(&r.declReader, buf)

	// convert to little endian
	// TODO(gri) go/constant should have a more direct conversion function
	//           (e.g., once it supports a big.Float based implementation)
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	x := constant.MakeFromBytes(buf)
	if signed && n&1 != 0 {
		x = constant.UnaryOp(token.SUB, x, 0)
	}
	return x
}

func (r *bimportReader) mpfloat(typ *ast.Ident) {
	x := r.mpint(typ)
	if constant.Sign(x) == 0 {
		return
	}
	r.int64()
}

func (r *bimportReader) doType() *ibinType {
	k := r.kind()
	switch k {
	default:
		panic(fmt.Sprintf("unexpected kind tag: %v", k))
	case definedType:
		pkg, name := r.qualifiedIdent()
		r.p.doDecl(pkg, name)
		return pkg.declTyp[name]
	case pointerType:
		elt := r.typ()
		return &ibinType{typ: &ast.StarExpr{X: elt.typ}}
	case sliceType:
		elt := r.typ()
		return &ibinType{typ: &ast.ArrayType{Len: nil, Elt: elt.typ}}
	case arrayType:
		n := r.uint64()
		elt := r.typ()
		return &ibinType{typ: &ast.ArrayType{
			Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(n)},
			Elt: elt.typ,
		}}
	case chanType:
		dir := ast.SEND | ast.RECV
		switch d := r.uint64(); d {
		case 1:
			dir = ast.RECV
		case 2:
			dir = ast.SEND
		case 3:
			// already set
		default:
			panic(fmt.Sprintf("unexpected channel dir %d", d))
		}
		elt := r.typ()
		return &ibinType{typ: &ast.ChanType{Dir: dir, Value: elt.typ}}
	case mapType:
		key := r.typ()
		val := r.typ()
		return &ibinType{typ: &ast.MapType{Key: key.typ, Value: val.typ}}
	case signatureType:
		r.currPkg = r.pkg()
		return &ibinType{typ: r.signature(nil, nil)}

	case structType:
		r.currPkg = r.pkg()

		fields := make([]*ast.Field, r.uint64())
		for i := range fields {
			r.pos()
			fname := r.ident()
			ftyp := r.typ()
			emb := r.bool()
			r.string() // tag
			var names []*ast.Ident
			if fname != "" && !emb {
				names = []*ast.Ident{ast.NewIdent(fname)}
			}

			// TODO: add tag?
			fields[i] = &ast.Field{Names: names, Type: ftyp.typ}
		}

		return &ibinType{typ: &ast.StructType{Fields: &ast.FieldList{List: fields}}}

	// TODO: this probably needs to be updated for go1.18
	case interfaceType:
		r.currPkg = r.pkg()

		numEmbeds := int(r.uint64())
		embeddeds := make([]*ast.SelectorExpr, 0, numEmbeds)
		for i := 0; i < numEmbeds; i++ {
			r.pos()
			t := r.typ()
			// TODO: this differs
			if named, ok := t.typ.(*ast.SelectorExpr); ok {
				embeddeds = append(embeddeds, named)
			}
		}

		methods := make([]*ast.Field, r.uint64())
		for i := range methods {
			r.pos()
			mname := r.ident()
			msig := r.signature(nil, nil)
			methods[i] = &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(mname)},
				Type:  msig,
			}
		}
		for _, field := range embeddeds {
			methods = append(methods, &ast.Field{Type: field})
		}

		return &ibinType{typ: &ast.InterfaceType{Methods: &ast.FieldList{List: methods}}}

	case typeParamType:
		if r.p.exportVersion < iexportVersionGenerics {
			panic("unexpected type param type")
		}
		pkg, name := r.qualifiedIdent()
		id := ident{pkg.fullName, name}
		if t, ok := r.p.tparamIndex[id]; ok {
			// We're already in the process of importing this typeparam.
			return t
		}
		// Otherwise, import the definition of the typeparam now.
		r.p.doDecl(pkg, name)
		return r.p.tparamIndex[id]

	// WARN: handle
	case instanceType:
		if r.p.exportVersion < iexportVersionGenerics {
			panic("unexpected type param type")
		}
		// pos does not matter for instances: they are positioned on the original
		// type.
		r.pos()
		len := r.uint64()
		targs := make([]*ibinType, len)
		for i := range targs {
			targs[i] = r.typ()
		}
		baseType := r.typ()
		_ = baseType

		// The imported instantiated type doesn't include any methods, so
		// we must always use the methods of the base (orig) type.
		// TODO provide a non-nil *Context
		// t, _ := types.Instantiate(nil, baseType, targs, false)
		// return t

		// WARN: figure out how to implement this
		// Maybe: ast.TypeSpec

		// WARN WARN WARN
		// This is wrong
		return &ibinType{typ: &ast.FuncType{}}

	// WARN: handle
	case unionType:
		if r.p.exportVersion < iexportVersionGenerics {
			panic("unexpected type param type")
		}

		// terms := make([]*types.Term, r.uint64())
		// for i := range terms {
		// 	terms[i] = types.NewTerm(r.bool(), r.typ())
		// }
		n := r.uint64()
		for ; n > 0; n-- {
			_ = r.bool()
			_ = r.typ()
		}
		_ = ast.UnaryExpr{}

		// WARN WARN WARN
		// This is wrong
		return &ibinType{typ: &ast.FuncType{}}
	}
}

// TODO: ast.FuncType has a new field: TypeParams and we likely need to add
// the rparams, tparams []*types.TypeParam to this method.
func (r *bimportReader) signature(rparams, tparams []*ibinType) *ast.FuncType {
	params := r.paramList()
	results := r.paramList()
	if params != nil && len(params.List) > 0 {
		if r.bool() { // variadic flag
			last := params.List[len(params.List)-1]
			last.Type = &ast.Ellipsis{Elt: last.Type.(*ast.ArrayType).Elt}
		}
	}
	// TODO: use rparams and tparams
	return &ast.FuncType{Params: params, Results: results}
}

func (r *bimportReader) tparamList() []*ibinType {
	n := r.uint64()
	if n == 0 {
		return nil
	}
	xs := make([]*ibinType, n)
	for i := range xs {
		xs[i] = r.typ()
	}
	return xs
}

func (r *bimportReader) paramList() *ast.FieldList {
	xs := make([]*ast.Field, r.uint64())
	for i := range xs {
		xs[i] = r.param()
	}
	return &ast.FieldList{List: xs}
}

func (r *bimportReader) param() *ast.Field {
	r.pos()
	name := r.ident()
	if name == "" { // gocode specific hack for unnamed parameters
		name = "?"
	}
	t := r.typ()
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  t.typ,
	}
}

func (r *bimportReader) typ() *ibinType {
	return r.p.typAt(r.uint64())
}

func isInterface(t ast.Expr) bool {
	_, ok := t.(*ast.InterfaceType)
	return ok
}

func (r *bimportReader) kind() itag       { return itag(r.uint64()) }
func (r *bimportReader) pkg() ibinPackage { return r.p.pkgAt(r.uint64()) }
func (r *bimportReader) string() string   { return r.p.stringAt(r.uint64()) }
func (r *bimportReader) bool() bool       { return r.uint64() != 0 }
func (r *bimportReader) ident() string    { return r.string() }

func (r *bimportReader) qualifiedIdent() (ibinPackage, string) {
	name := r.string()
	pkg := r.pkg()
	return pkg, name
}

func (r *bimportReader) int64() int64 {
	n, err := binary.ReadVarint(&r.declReader)
	if err != nil {
		panic(fmt.Sprintf("readVarint: %v", err))
	}
	return n
}

func (r *bimportReader) uint64() uint64 {
	n, err := binary.ReadUvarint(&r.declReader)
	if err != nil {
		panic(fmt.Sprintf("readUvarint: %v", err))
	}
	return n
}

func (r *bimportReader) byte() byte {
	x, err := r.declReader.ReadByte()
	if err != nil {
		panic(fmt.Sprintf("declReader.ReadByte: %v", err))
	}
	return x
}

var predeclaredIBinTypes []ibinType

func init() {
	predeclaredIBinTypes = make([]ibinType, len(predeclared))
	for i, pt := range predeclared {
		predeclaredIBinTypes[i] = ibinType{typ: pt}
	}
}
