// +build darwin linux

package fs

import (
	"os"
	"syscall"
	"time"
)

// A fileStat is the implementation of FileInfo returned by Stat and Lstat,
// that implements the GobEncode, GobDecode, MarshalJSON and UnmarshalJSON.
// Sys() always returns nil.
type fileStat struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name, base name of the file
func (fs *fileStat) Name() string { return fs.name }

// Size, length in bytes for regular files; system-dependent for others
func (fs *fileStat) Size() int64 { return fs.size }

// Mode, file mode bits
func (fs *fileStat) Mode() os.FileMode { return fs.mode }

// ModTime, modification time
func (fs *fileStat) ModTime() time.Time { return fs.modTime }

// IsDir, abbreviation for Mode().IsDir()
func (fs *fileStat) IsDir() bool { return fs.mode.IsDir() }

// Sys, underlying data source, unlike os.FileInfo nil is always returned.
func (fs *fileStat) Sys() interface{} { return nil }

// Stat returns a FileInfo describing the named file.
// If there is an error, it will be of type *PathError.
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

// Lstat returns a FileInfo describing the named file.
// If the file is a symbolic link, the returned FileInfo
// describes the symbolic link. Lstat makes no attempt to follow the link.
// If there is an error, it will be of type *PathError.
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
