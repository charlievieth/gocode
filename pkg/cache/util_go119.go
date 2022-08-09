//go:build go1.19

package cache

import "hash/maphash"

func hashData(b []byte) uint64 {
	return maphash.Bytes(hashSeed, b)
}
