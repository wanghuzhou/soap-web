package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"soapweb/internal/model"
	"soapweb/internal/store"
)

// writeLegacyProjects creates a projects.json in dir holding one project and
// returns the project's id.
func writeLegacyProjects(t *testing.T, dir string) string {
	t.Helper()
	st, err := store.New(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	p, err := st.AddProject(model.Project{Name: "legacy"})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	return p.ID
}

// readSettings loads settings.json from disk for assertions.
func readSettings(t *testing.T, dir string) settings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var s settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return s
}

// TestOpenMigratesLegacyProjects covers the upgrade path: an existing
// data/projects.json becomes the "默认" workspace without moving or renaming
// the file.
func TestOpenMigratesLegacyProjects(t *testing.T) {
	dir := t.TempDir()
	projectID := writeLegacyProjects(t, dir)

	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	info := reg.CurrentInfo()
	if info.Name != defaultName {
		t.Errorf("current name = %q, want %q", info.Name, defaultName)
	}
	st, _ := reg.Active()
	list := st.List()
	if len(list) != 1 || list[0].ID != projectID {
		t.Fatalf("projects = %+v, want the migrated legacy project", list)
	}
	s := readSettings(t, dir)
	if s.Current != info.ID || len(s.Workspaces) != 1 {
		t.Fatalf("settings = %+v", s)
	}
	if s.Workspaces[0].File != "projects.json" {
		t.Errorf("file = %q, want projects.json", s.Workspaces[0].File)
	}
}

// TestOpenLegacyFilePath keeps the old "-data data/projects.json" invocation
// working: the file is adopted as the default workspace.
func TestOpenLegacyFilePath(t *testing.T) {
	dir := t.TempDir()
	writeLegacyProjects(t, dir)

	reg, err := Open(dir, "projects.json")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := readSettings(t, dir)
	if s.Workspaces[0].File != "projects.json" {
		t.Errorf("file = %q", s.Workspaces[0].File)
	}
	st, _ := reg.Active()
	if st == nil {
		t.Fatal("Active returned a nil store")
	}
}

// TestSwitchPersistsAcrossReopen verifies that switching changes the visible
// projects and that settings.json remembers the choice for the next start.
func TestSwitchPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defaultID := reg.CurrentInfo().ID

	other, err := reg.Create("测试环境")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if reg.CurrentInfo().ID != other.ID {
		t.Fatal("Create should activate the new workspace")
	}
	otherStore, _ := reg.Active()
	if _, err := otherStore.AddProject(model.Project{Name: "only-in-other"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	if _, err := reg.Switch(defaultID); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	mainStore, _ := reg.Active()
	if got := mainStore.List(); len(got) != 0 {
		t.Fatalf("default workspace should be empty, got %+v", got)
	}
	if _, err := mainStore.AddProject(model.Project{Name: "only-in-default"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	// Reopen: current must be the default workspace, and both files keep their
	// own projects.
	reopened, err := Open(dir, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.CurrentInfo().ID != defaultID {
		t.Errorf("current = %q, want %q", reopened.CurrentInfo().ID, defaultID)
	}
	st, _ := reopened.Active()
	if got := st.List(); len(got) != 1 || got[0].Name != "only-in-default" {
		t.Errorf("default projects = %+v", got)
	}

	if _, err := reopened.Switch(other.ID); err != nil {
		t.Fatalf("Switch other: %v", err)
	}
	st, _ = reopened.Active()
	if got := st.List(); len(got) != 1 || got[0].Name != "only-in-other" {
		t.Errorf("other projects = %+v", got)
	}
}

// TestSwitchToCorruptFileKeepsState is the safety property that matters most:
// a broken target must not change the active workspace nor settings.json.
func TestSwitchToCorruptFileKeepsState(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	original := reg.CurrentInfo()
	other, err := reg.Create("坏文件")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := reg.Switch(original.ID); err != nil {
		t.Fatalf("Switch back: %v", err)
	}

	bad := filepath.Join(dir, workspacesDir, other.ID+".json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := reg.Switch(other.ID); err == nil {
		t.Fatal("Switch to a corrupt workspace should fail")
	}
	if got := reg.CurrentInfo().ID; got != original.ID {
		t.Errorf("current = %q, want %q", got, original.ID)
	}
	if s := readSettings(t, dir); s.Current != original.ID {
		t.Errorf("settings current = %q, want %q", s.Current, original.ID)
	}
}

// TestCreateRenameDelete covers the lifecycle and the soft-delete behaviour.
func TestCreateRenameDelete(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	created, err := reg.Create("  临时空间  ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "临时空间" {
		t.Errorf("name = %q, want trimmed", created.Name)
	}

	if _, err := reg.Create("临时空间"); err == nil {
		t.Error("duplicate name should be rejected")
	}
	if _, err := reg.Create("TEMP"); err != nil {
		t.Fatalf("Create TEMP: %v", err)
	}
	if _, err := reg.Create("temp"); err == nil {
		t.Error("case-insensitive duplicate should be rejected")
	}

	renamed, err := reg.Rename(created.ID, "改名后")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "改名后" {
		t.Errorf("renamed = %+v", renamed)
	}
	if s := readSettings(t, dir); s.Workspaces[1].Name != "改名后" || s.Workspaces[1].File != filepath.Join(workspacesDir, created.ID+".json") {
		t.Errorf("rename touched the file mapping: %+v", s.Workspaces[1])
	}

	// Deleting the active workspace must switch to another one.
	if err := reg.Delete(created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if reg.CurrentInfo().ID == created.ID {
		t.Error("deleted workspace is still current")
	}
	if _, err := os.Stat(filepath.Join(dir, workspacesDir, created.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("workspace file should be gone, stat err = %v", err)
	}
	trash, err := os.ReadDir(filepath.Join(dir, trashDir))
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash entries = %d, err = %v", len(trash), err)
	}

	// The last workspace cannot be deleted.
	for len(reg.List()) > 1 {
		if err := reg.Delete(reg.List()[0].ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	if err := reg.Delete(reg.List()[0].ID); err != ErrLastWorkspace {
		t.Errorf("deleting the last workspace = %v, want ErrLastWorkspace", err)
	}
}

// TestInvalidSettingsRejected covers hand-edited settings: paths that escape
// the data directory, duplicate names and empty registries.
func TestInvalidSettingsRejected(t *testing.T) {
	cases := map[string]string{
		"traversal":      `{"version":1,"current":"a","workspaces":[{"id":"a","name":"x","file":"../evil.json"}]}`,
		"absolute":       `{"version":1,"current":"a","workspaces":[{"id":"a","name":"x","file":"` + filepath.ToSlash(filepath.Join(t.TempDir(), "evil.json")) + `"}]}`,
		"duplicate":      `{"version":1,"current":"a","workspaces":[{"id":"a","name":"x","file":"a.json"},{"id":"b","name":"X","file":"b.json"}]}`,
		"duplicate-file": `{"version":1,"current":"a","workspaces":[{"id":"a","name":"x","file":"same.json"},{"id":"b","name":"y","file":"same.json"}]}`,
		"empty":          `{"version":1,"current":"a","workspaces":[]}`,
		"no-id":          `{"version":1,"current":"a","workspaces":[{"name":"x","file":"a.json"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir, ""); err == nil {
				t.Fatalf("Open should reject %s", name)
			}
		})
	}
}

// TestCurrentMissingFallsBack self-heals a settings file whose current id no
// longer exists.
func TestCurrentMissingFallsBack(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":1,"current":"gone","workspaces":[{"id":"a","name":"A","file":"a.json"},{"id":"b","name":"B","file":"b.json"}]}`
	if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := reg.CurrentInfo().ID; got != "a" {
		t.Errorf("current = %q, want a", got)
	}
	if s := readSettings(t, dir); s.Current != "a" {
		t.Errorf("settings current = %q, want a", s.Current)
	}
}

// TestCorruptCurrentFallsBackToHealthyWorkspace keeps the UI usable when the
// current file was damaged by hand.
func TestCorruptCurrentFallsBackToHealthyWorkspace(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":1,"current":"a","workspaces":[{"id":"a","name":"A","file":"a.json"},{"id":"b","name":"B","file":"b.json"}]}`
	if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := reg.CurrentInfo().ID; got != "b" {
		t.Errorf("current = %q, want the healthy fallback b", got)
	}
}

// TestConcurrentSwitchAndRead exercises the registry lock. Run with -race.
func TestConcurrentSwitchAndRead(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	a := reg.CurrentInfo().ID
	b, err := reg.Create("B")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				target := a
				if (i+j)%2 == 0 {
					target = b.ID
				}
				if _, err := reg.Switch(target); err != nil {
					t.Errorf("Switch: %v", err)
					return
				}
				reg.List()
				reg.CurrentInfo()
				st, _ := reg.Active()
				st.List()
			}
		}(i)
	}
	wg.Wait()

	if len(reg.List()) != 2 {
		t.Errorf("workspaces = %+v", reg.List())
	}
	current := readSettings(t, dir).Current
	if current != a && current != b.ID {
		t.Errorf("settings current = %q, want one of the two workspaces", current)
	}
}
