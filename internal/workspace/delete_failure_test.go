package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"soapweb/internal/model"
)

// TestFailedDeleteNeverOrphansTheWorkspace is the regression test for the
// silent-data-loss path in Delete.
//
// The old order was: rename the file into data/trash, then write settings.json,
// and on a failed settings write attempt a rollback whose error was discarded
// (`_ = os.Rename(...)`). Two outcomes were possible, both bad:
//
//   - the rollback failed (the destination is held open — routine on Windows),
//     leaving the file in trash while settings.json and r.active still pointed at
//     it. Every later save recreated the file at its old path, because
//     WriteAtomic uses os.WriteFile, and reported "已保存" — for a workspace that
//     vanished on the next restart;
//   - the rollback worked, and the only sign of trouble was a bare
//     "rename ... Access is denied" that never mentioned the workspace.
//
// The fixed order copies the file aside for the backup (leaving the original in
// place), then writes settings.json, and only then removes the original. A
// failure therefore leaves both the registry and the file system untouched.
func TestFailedDeleteNeverOrphansTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	second, err := reg.Create("要被删除的")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	beforeCurrent := reg.CurrentInfo()
	beforeCount := len(reg.List())

	// Break the settings write. A non-empty directory in place of settings.json
	// makes the atomic rename fail (verified: "Access is denied" on Windows).
	settingsPath := filepath.Join(dir, settingsFile)
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("remove settings: %v", err)
	}
	if err := os.Mkdir(settingsPath, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsPath, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed settings dir: %v", err)
	}

	err = reg.Delete(second.ID)
	if err == nil {
		t.Fatal("Delete reported success although settings.json could not be written")
	}

	// 1. The workspace file must still be at its original path: it is what the
	// surviving registry entry refers to.
	wsFile := filepath.Join(dir, workspacesDir, second.ID+".json")
	if _, statErr := os.Stat(wsFile); statErr != nil {
		t.Errorf("workspace file was moved despite a failed delete: %v", statErr)
	}

	// 2. A failed delete must not leave a backup behind: a stray trash entry
	// for a workspace that still exists is exactly the orphan this guards.
	if trashHasWorkspaceFile(t, dir, second.ID) {
		t.Errorf("failed delete left a backup in trash for a workspace that still exists")
	}

	// 3. Registry state must be untouched: same current, same count.
	if got := reg.CurrentInfo(); got.ID != beforeCurrent.ID {
		t.Errorf("current = %q, want %q (unchanged)", got.ID, beforeCurrent.ID)
	}
	if got := len(reg.List()); got != beforeCount {
		t.Errorf("workspace count = %d, want %d (unchanged)", got, beforeCount)
	}

	// 4. The error must name the workspace so the user knows what failed.
	if !strings.Contains(err.Error(), "要被删除的") && !strings.Contains(err.Error(), second.ID) {
		t.Errorf("error does not identify the workspace: %v", err)
	}

	// 5. The active store must still be the one that survived, and writes to it
	// must land in a workspace settings.json still knows about.
	st, info := reg.Active()
	if info.ID != beforeCurrent.ID {
		t.Fatalf("active = %q, want %q", info.ID, beforeCurrent.ID)
	}
	if _, err := st.AddProject(model.Project{Name: "仍然可以保存"}); err != nil {
		t.Fatalf("saving after a failed delete: %v", err)
	}
}

// TestDeleteMovesFileOnlyAfterSettingsRecordsIt checks the successful path keeps
// its contract: the backup lands in trash once, the original is gone, and
// settings no longer lists the workspace.
func TestDeleteMovesFileOnlyAfterSettingsRecordsIt(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	second, err := reg.Create("要删除的")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := reg.Delete(second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	wsFile := filepath.Join(dir, workspacesDir, second.ID+".json")
	if _, err := os.Stat(wsFile); !os.IsNotExist(err) {
		t.Errorf("workspace file should be gone from its original path, err = %v", err)
	}
	trashEntries, err := os.ReadDir(filepath.Join(dir, trashDir))
	if err != nil || len(trashEntries) != 1 {
		t.Fatalf("trash entries = %d, err = %v", len(trashEntries), err)
	}
	for _, w := range reg.List() {
		if w.ID == second.ID {
			t.Errorf("deleted workspace is still listed: %+v", w)
		}
	}
}

// TestDeleteRefusesWhenBackupCannotBeWritten keeps the soft-delete promise: if
// the backup cannot be created, the workspace must not be deleted at all, so a
// click can never destroy data that has no recoverable copy.
func TestDeleteRefusesWhenBackupCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	second, err := reg.Create("要删除的")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	beforeCount := len(reg.List())

	// A regular file where data/trash should be makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(dir, trashDir), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("block trash dir: %v", err)
	}

	err = reg.Delete(second.ID)
	if err == nil {
		t.Fatal("Delete succeeded although its backup could not be written")
	}
	if !strings.Contains(err.Error(), "备份") {
		t.Errorf("error should mention the failed backup: %v", err)
	}

	// Nothing may have changed: the workspace and its file are both intact.
	wsFile := filepath.Join(dir, workspacesDir, second.ID+".json")
	if _, statErr := os.Stat(wsFile); statErr != nil {
		t.Errorf("workspace file was removed although the backup failed: %v", statErr)
	}
	if got := len(reg.List()); got != beforeCount {
		t.Errorf("workspace count = %d, want %d (unchanged)", got, beforeCount)
	}
	listed := false
	for _, w := range reg.List() {
		if w.ID == second.ID {
			listed = true
		}
	}
	if !listed {
		t.Error("workspace disappeared even though the backup failed")
	}
}

// trashHasWorkspaceFile reports whether any file in data/trash belongs to id.
func trashHasWorkspaceFile(t *testing.T, dir, id string) bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, trashDir))
	if err != nil {
		return false // unreadable, or not a directory: nothing was moved into it
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id+"-") || e.Name() == id+".json" {
			return true
		}
	}
	return false
}
