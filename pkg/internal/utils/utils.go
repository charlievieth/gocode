package utils

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mdempsky/gocode/pkg/cache/lru"
)

type dirnamesEntry struct {
	names   []string
	modTime time.Time
}

var dirnamesCache = lru.Cache{
	MaxEntries: 128,
}

func Readdirnames(dir string) ([]string, error) {
	dir = filepath.Clean(dir)
	// fast path for cached entrie
	if v, ok := dirnamesCache.Get(dir); ok && v != nil {
		if fi, err := os.Stat(dir); err == nil {
			ent := v.(*dirnamesEntry)
			if fi.ModTime().Equal(ent.modTime) {
				return ent.names, nil
			}
		}
	}

	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	if err == nil {
		if fi, err := f.Stat(); err == nil {
			dirnamesCache.Add(dir, &dirnamesEntry{
				names:   names,
				modTime: fi.ModTime(),
			})
		}
	}
	f.Close()
	return names, err
}
