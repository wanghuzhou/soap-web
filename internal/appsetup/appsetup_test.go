package appsetup

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

	if gotDir, gotLegacy := ResolveDataDir(dir); gotDir != dir || gotLegacy != "" {
		t.Errorf("existing dir = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, dir)
	}

	file := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(file, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if gotDir, gotLegacy := ResolveDataDir(file); gotDir != dir || gotLegacy != "projects.json" {
		t.Errorf("existing file = (%q, %q), want (%q, projects.json)", gotDir, gotLegacy, dir)
	}

	missing := filepath.Join(dir, "data", "projects.json")
	if gotDir, gotLegacy := ResolveDataDir(missing); gotDir != filepath.Join(dir, "data") || gotLegacy != "projects.json" {
		t.Errorf("missing file = (%q, %q)", gotDir, gotLegacy)
	}

	settings := filepath.Join(dir, "data", "settings.json")
	if gotDir, gotLegacy := ResolveDataDir(settings); gotDir != filepath.Join(dir, "data") || gotLegacy != "" {
		t.Errorf("settings path = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, filepath.Join(dir, "data"))
	}

	plain := filepath.Join(dir, "newdir")
	if gotDir, gotLegacy := ResolveDataDir(plain); gotDir != plain || gotLegacy != "" {
		t.Errorf("missing dir = (%q, %q), want (%q, \"\")", gotDir, gotLegacy, plain)
	}

	// No flag: a data dir next to the executable.
	gotDir, gotLegacy := ResolveDataDir("")
	if !strings.HasSuffix(gotDir, "data") || gotLegacy != "" {
		t.Errorf("default = (%q, %q)", gotDir, gotLegacy)
	}
}

// TestLogDir keeps the log directory a sibling of the data directory.
func TestLogDir(t *testing.T) {
	base := t.TempDir()
	if got, want := LogDir(filepath.Join(base, "data")), filepath.Join(base, "logs"); got != want {
		t.Errorf("LogDir = %q, want %q", got, want)
	}
}

// TestUserDataDir checks the fallback location is a per-user soap-web path.
func TestUserDataDir(t *testing.T) {
	dir, err := UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir: %v", err)
	}
	slash := filepath.ToSlash(dir)
	if !strings.HasSuffix(slash, "soap-web/data") {
		t.Errorf("UserDataDir = %q, want a .../soap-web/data path", dir)
	}
}

// TestSamePath covers the comparison used to avoid "falling back" to the same
// directory (and to handle Windows case-insensitive paths).
func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	if !SamePath(dir, dir) {
		t.Errorf("SamePath(%q, %q) = false", dir, dir)
	}
	if !SamePath(dir, filepath.Join(dir, ".")) {
		t.Errorf("SamePath should ignore a trailing dot component")
	}
	if SamePath(dir, filepath.Join(dir, "sub")) {
		t.Errorf("SamePath should distinguish different directories")
	}
}

// TestOpenRegistryKeepsCorruptSettingsError: the fallback is only for permission
// problems. A corrupt settings.json must keep failing loudly instead of
// silently starting on an empty data set.
func TestOpenRegistryKeepsCorruptSettingsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRegistry(dir, "", true); err == nil {
		t.Fatal("OpenRegistry should return the parse error")
	}
}
