// Package fileutil holds the filesystem helpers shared by the JSON stores:
// BOM-tolerant reads and crash-safe atomic writes.
package fileutil

import (
	"bytes"
	"encoding/json"
	"os"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// StripBOM removes a UTF-8 byte-order mark. Windows editors (notably older
// Notepad) add one by default, and encoding/json rejects it.
func StripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

// WriteJSONAtomic marshals v as indented JSON and writes it atomically.
func WriteJSONAtomic(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteAtomic(path, data, perm)
}

// WriteAtomic writes data through path+".tmp" and renames it over path, so a
// crash mid-write cannot truncate the real file.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
