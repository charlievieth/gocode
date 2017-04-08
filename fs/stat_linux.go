package fs

import (
	"os"
	"syscall"
	"time"
)

func fillFileStatFromSys(fs *fileStat, sys *syscall.Stat_t, name string) {
	fs.name = basename(name)
	fs.size = sys.Size
	fs.modTime = timespecToTime(sys.Mtim)
	fs.mode = os.FileMode(sys.Mode & 0777)
	switch sys.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		fs.mode |= os.ModeDevice
	case syscall.S_IFCHR:
		fs.mode |= os.ModeDevice | os.ModeCharDevice
	case syscall.S_IFDIR:
		fs.mode |= os.ModeDir
	case syscall.S_IFIFO:
		fs.mode |= os.ModeNamedPipe
	case syscall.S_IFLNK:
		fs.mode |= os.ModeSymlink
	case syscall.S_IFREG:
		// nothing to do
	case syscall.S_IFSOCK:
		fs.mode |= os.ModeSocket
	}
	if sys.Mode&syscall.S_ISGID != 0 {
		fs.mode |= os.ModeSetgid
	}
	if sys.Mode&syscall.S_ISUID != 0 {
		fs.mode |= os.ModeSetuid
	}
	if sys.Mode&syscall.S_ISVTX != 0 {
		fs.mode |= os.ModeSticky
	}
}

func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}
