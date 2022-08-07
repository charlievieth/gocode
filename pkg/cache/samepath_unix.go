//go:build !windows
// +build !windows

package cache

// TODO: remove - only used by GetGbProjectPaths
//
// SamePath checks two file paths for their equality based on the current filesystem
func SamePath(a, b string) bool {
	return a == b
}
