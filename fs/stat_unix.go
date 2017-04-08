// +build darwin linux

package fs

import (
	"os"
	"syscall"
)

func Stat(name string) (os.FileInfo, error) {
	var sys syscall.Stat_t
	err := syscall.Stat(name, &sys)
	if err != nil {
		return nil, &os.PathError{"stat", name, err}
	}
	var f fileStat
	fillFileStatFromSys(&f, &sys, name)
	return &f, nil
}

func Lstat(name string) (os.FileInfo, error) {
	var sys syscall.Stat_t
	err := syscall.Lstat(name, &sys)
	if err != nil {
		return nil, &os.PathError{"lstat", name, err}
	}
	var f fileStat
	fillFileStatFromSys(&f, &sys, name)
	return &f, nil
}

// basename removes trailing slashes and the leading directory name from path name
func basename(name string) string {
	i := len(name) - 1
	// Remove trailing slashes
	for ; i > 0 && name[i] == '/'; i-- {
		name = name[:i]
	}
	// Remove leading directory name
	for i--; i >= 0; i-- {
		if name[i] == '/' {
			name = name[i+1:]
			break
		}
	}

	return name
}
