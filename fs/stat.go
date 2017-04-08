package fs

import (
	"os"
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
