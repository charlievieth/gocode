// +build go1.5

package gocode

import "github.com/charlievieth/gocode/fs"

func (m *package_file_cache) update_cache() {
	if m.mtime == -1 {
		return
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
		m.process_package_data(data)
	}
}
