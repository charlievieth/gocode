//go:build !go1.17
// +build !go1.17

package gocode

const g_builtin_unsafe_package = `
import
$$
package unsafe
	type @"".Pointer uintptr
	func @"".Offsetof (? any) uintptr
	func @"".Sizeof (? any) uintptr
	func @"".Alignof (? any) uintptr

$$
`
