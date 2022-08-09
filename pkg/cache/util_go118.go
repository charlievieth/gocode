//go:build !go1.19

package cache

import "hash/maphash"

func hashData(b []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	_, _ = h.Write(b)
	return h.Sum64()
}
