// +build !darwin,!linux

package fs

import (
	"os"
)

func Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

func Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
