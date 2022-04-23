//go:build !go1.18
// +build !go1.18

package gocode

func ReplaceInterfaceWithAny(s string) string {
	return s
}
