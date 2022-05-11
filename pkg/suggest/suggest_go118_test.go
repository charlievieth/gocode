//go:build go1.18
// +build go1.18

package suggest_test

import "bytes"

func ReplaceInterfaceWithAny(in []byte) []byte {
	return bytes.ReplaceAll(in, []byte("interface{}"), []byte("any"))
}
