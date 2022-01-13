//go:build go1.18
// +build go1.18

package gocode

import "strings"

func ReplaceInterfaceWithAny(s string) string {
	return strings.ReplaceAll(s, "interface{}", "any")
}
