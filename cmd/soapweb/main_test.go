package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveDataDir locks the -data compatibility rules: the flag used to be a
// projects.json path, and existing shortcuts still pass one.
func TestResolveDataDir(t *testing.T) {
	dir := t.TempDir()

	if gotDir, gotLegacy := resolveDataDir(dir); gotDir != dir || gotLegacy != "" {
		t.Errorf("existing dir = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, dir)
	}

	file := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(file, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if gotDir, gotLegacy := resolveDataDir(file); gotDir != dir || gotLegacy != "projects.json" {
		t.Errorf("existing file = (%q, %q), want (%q, projects.json)", gotDir, gotLegacy, dir)
	}

	missing := filepath.Join(dir, "data", "projects.json")
	if gotDir, gotLegacy := resolveDataDir(missing); gotDir != filepath.Join(dir, "data") || gotLegacy != "projects.json" {
		t.Errorf("missing file = (%q, %q)", gotDir, gotLegacy)
	}

	settings := filepath.Join(dir, "data", "settings.json")
	if gotDir, gotLegacy := resolveDataDir(settings); gotDir != filepath.Join(dir, "data") || gotLegacy != "" {
		t.Errorf("settings path = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, filepath.Join(dir, "data"))
	}

	plain := filepath.Join(dir, "newdir")
	if gotDir, gotLegacy := resolveDataDir(plain); gotDir != plain || gotLegacy != "" {
		t.Errorf("missing dir = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, plain)
	}

	// No flag: a data dir next to the executable.
	gotDir, gotLegacy := resolveDataDir("")
	if !strings.HasSuffix(gotDir, "data") || gotLegacy != "" {
		t.Errorf("default = (%q, %q)", gotDir, gotLegacy)
	}
}
