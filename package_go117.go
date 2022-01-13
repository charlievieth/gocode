//go:build !go1.18
// +build !go1.18

package gocode

import "go/ast"

var predeclared = []ast.Expr{
	// basic types
	ast.NewIdent("bool"),
	ast.NewIdent("int"),
	ast.NewIdent("int8"),
	ast.NewIdent("int16"),
	ast.NewIdent("int32"),
	ast.NewIdent("int64"),
	ast.NewIdent("uint"),
	ast.NewIdent("uint8"),
	ast.NewIdent("uint16"),
	ast.NewIdent("uint32"),
	ast.NewIdent("uint64"),
	ast.NewIdent("uintptr"),
	ast.NewIdent("float32"),
	ast.NewIdent("float64"),
	ast.NewIdent("complex64"),
	ast.NewIdent("complex128"),
	ast.NewIdent("string"),

	// basic type aliases
	ast.NewIdent("byte"),
	ast.NewIdent("rune"),

	// error
	ast.NewIdent("error"),

	// TODO(nsf): don't think those are used in just package type info,
	// maybe for consts, but we are not interested in that
	// untyped types
	ast.NewIdent("&untypedBool&"),    // TODO: types.Typ[types.UntypedBool],
	ast.NewIdent("&untypedInt&"),     // TODO: types.Typ[types.UntypedInt],
	ast.NewIdent("&untypedRune&"),    // TODO: types.Typ[types.UntypedRune],
	ast.NewIdent("&untypedFloat&"),   // TODO: types.Typ[types.UntypedFloat],
	ast.NewIdent("&untypedComplex&"), // TODO: types.Typ[types.UntypedComplex],
	ast.NewIdent("&untypedString&"),  // TODO: types.Typ[types.UntypedString],
	ast.NewIdent("&untypedNil&"),     // TODO: types.Typ[types.UntypedNil],

	// package unsafe
	&ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent("Pointer")},

	// invalid type
	ast.NewIdent(">_<"), // TODO: types.Typ[types.Invalid], // only appears in packages with errors

	// used internally by gc; never used by this package or in .a files
	ast.NewIdent("any"),
}
