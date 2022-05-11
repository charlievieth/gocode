//go:build go1.17
// +build go1.17

package suggest

var builtinTypes = map[string]string{
	// Universe.
	"append":  "func(slice []Type, elems ..Type) []Type",
	"cap":     "func(v Type) int",
	"close":   "func(c chan<- Type)",
	"complex": "func(real FloatType, imag FloatType) ComplexType",
	"copy":    "func(dst []Type, src []Type) int",
	"delete":  "func(m map[Key]Type, key Key)",
	"imag":    "func(c ComplexType) FloatType",
	"len":     "func(v Type) int",
	"make":    "func(Type, size IntegerType) Type",
	"new":     "func(Type) *Type",
	"panic":   "func(v interface{})",
	"print":   "func(args ...Type)",
	"println": "func(args ...Type)",
	"real":    "func(c ComplexType) FloatType",
	"recover": "func() interface{}",

	// Package unsafe.
	"Alignof":  "func(x Type) uintptr",
	"Sizeof":   "func(x Type) uintptr",
	"Offsetof": "func(x Type) uintptr",

	// Package unsafe go1.17
	"Add":   "func(ptr unsafe.Pointer, len IntegerType) unsafe.Pointer",
	"Slice": "func(ptr *Type, len IntegerType) []Type",
}
