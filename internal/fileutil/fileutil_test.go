package fileutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStripBOM(t *testing.T) {
	plain := []byte(`{"a":1}`)
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, plain...)
	if got := StripBOM(withBOM); string(got) != string(plain) {
		t.Errorf("StripBOM = %q, want %q", got, plain)
	}
	if got := StripBOM(plain); string(got) != string(plain) {
		t.Errorf("StripBOM without BOM changed data: %q", got)
	}
}

func TestWriteJSONAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")

	if err := WriteJSONAtomic(path, map[string]int{"a": 1}, 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteJSONAtomic(path, map[string]int{"a": 2}, 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["a"] != 2 {
		t.Errorf("value = %d, want the overwritten 2", got["a"])
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file was left behind: %v", err)
	}
}
