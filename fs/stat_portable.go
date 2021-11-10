//go:build !darwin && !linux
// +build !darwin,!linux

package fs

import (
	"os"
)

// Lstat returns a FileInfo describing the named file.
// If the file is a symbolic link, the returned FileInfo
// describes the symbolic link. Lstat makes no attempt to follow the link.
// If there is an error, it will be of type *PathError.
func Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Stat returns a FileInfo describing the named file.
// If there is an error, it will be of type *PathError.
func Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func FileExists(name string) bool {
	_, err := Stat(name)
	return err == nil
}

func IsDir(name string) bool {
	fi, err := Stat(name)
	return err == nil && fi.IsDir()
}
