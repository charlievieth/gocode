// Search for objects with go1.4 and prior extensions

// +build !go1.5

package gocode

import "github.com/charlievieth/gocode/fs"

func (m *package_file_cache) find_file() string {
	if file_exists(m.name) {
		return m.name
	}

	n := len(m.name)
	filename := m.name[:n-1] + "6"
	if file_exists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "8"
	if file_exists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "5"
	if file_exists(filename) {
		return filename
	}
	return m.name
}

func (m *package_file_cache) update_cache() {
	if m.mtime == -1 {
		return
	}
	fname := m.find_file()
	stat, err := fs.Stat(fname)
	if err != nil {
		return
	}

	statmtime := stat.ModTime().UnixNano()
	if m.mtime != statmtime {
		m.mtime = statmtime

		data, err := file_reader.read_file(fname)
		if err != nil {
			return
		}
		m.process_package_data(data)
	}
}
