// +build go1.5

package gocode

import (
	"hash/crc32"

	"github.com/charlievieth/gocode/fs"
)

func (m *package_file_cache) update_cache() {
	if m.mtime == -1 {
		return // cached forever
	}
	stat, err := fs.Stat(m.name)
	if err != nil {
		return
	}

	statmtime := stat.ModTime().UnixNano()
	if m.mtime != statmtime {
		m.mtime = statmtime
		data, err := file_reader.read_file(m.name)
		if err != nil {
			return
		}
		sum := crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
		if m.checksum != sum || m.size != stat.Size() {
			m.checksum = sum
			m.size = stat.Size()
			m.process_package_data(data)
		}
	}
}
