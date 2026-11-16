package workspace

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"soapweb/internal/fileutil"
	"soapweb/internal/model"
	"soapweb/internal/store"
)

// Registry owns settings.json and the currently active store.
type Registry struct {
	mu       sync.RWMutex
	dir      string
	path     string
	settings settings
	active   *store.Store
	activeID string
	// lastBackup is the trash file created by the most recent backupLocked, so a
	// failed settings write can remove it again. Guarded by mu.
	lastBackup string
}

// DataDir returns the directory that holds settings.json and the workspace
// files. Used for startup logging.
func (r *Registry) DataDir() string { return r.dir }

// Active returns the current store and its public info.
func (r *Registry) Active() (*store.Store, Info) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active, r.infoAtLocked(r.activeID)
}

// CurrentInfo returns the public info of the active workspace.
func (r *Registry) CurrentInfo() Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.infoAtLocked(r.activeID)
}

// List returns every workspace in settings order.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.settings.Workspaces))
	for _, w := range r.settings.Workspaces {
		out = append(out, Info{ID: w.ID, Name: w.Name})
	}
	return out
}

// Switch makes id the active workspace.
//
// The target file is loaded first: a corrupt or unreadable file fails the
// switch without changing settings.json or the in-memory active workspace.
func (r *Registry) Switch(id string) (Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	i := r.indexOfLocked(id)
	if i < 0 {
		return Info{}, ErrNotFound
	}
	w := r.settings.Workspaces[i]
	if r.activeID == id && r.active != nil {
		return Info{ID: w.ID, Name: w.Name}, nil
	}

	p, err := r.absPath(w.File)
	if err != nil {
		return Info{}, err
	}
	st, err := store.New(p)
	if err != nil {
		return Info{}, fmt.Errorf("加载工作空间「%s」失败: %w", w.Name, err)
	}

	next := r.settings
	next.Current = id
	if err := r.saveSettings(next); err != nil {
		return Info{}, fmt.Errorf("保存工作空间设置失败: %w", err)
	}
	r.settings = next
	r.active = st
	r.activeID = id
	log.Printf("已切换工作空间：%s", w.Name)
	return Info{ID: w.ID, Name: w.Name}, nil
}

// Create adds an empty workspace and immediately makes it active.
func (r *Registry) Create(rawName string) (Info, error) {
	name, err := normalizeName(rawName)
	if err != nil {
		return Info{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasNameLocked(name, "") {
		return Info{}, ErrDuplicateName
	}
	return r.createLocked(name, nil)
}

// Import creates a workspace from a projects.json-format array and makes it
// active. Missing ids are filled in; proxy settings are validated before any
// file is written.
func (r *Registry) Import(rawName string, projects []model.Project) (Info, error) {
	name, err := normalizeName(rawName)
	if err != nil {
		return Info{}, err
	}
	// One claimer for the whole array, not one per project: a workspace is a
	// project array and the store addresses projects by id alone, so two
	// projects sharing an id behave like one — Get returns the first and
	// deleting it makes the second reappear. Per-project uniqueness would leave
	// exactly that case open.
	claim := store.NewIDClaimer()
	for i := range projects {
		if err := projects[i].Proxy.Validate(); err != nil {
			return Info{}, fmt.Errorf("%w：工程「%s」的代理配置无效: %v", ErrInvalidProject, projects[i].Name, err)
		}
		if err := model.ValidateProjectResponses(projects[i]); err != nil {
			return Info{}, fmt.Errorf("%w：工程「%s」: %v", ErrInvalidProject, projects[i].Name, err)
		}
		store.EnsureIDsIn(&projects[i], claim)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasNameLocked(name, "") {
		return Info{}, ErrDuplicateName
	}
	return r.createLocked(name, projects)
}

// createLocked writes a new workspace file, registers it in settings.json and
// activates it. Caller must hold the write lock and have checked the name.
func (r *Registry) createLocked(name string, projects []model.Project) (Info, error) {
	if projects == nil {
		projects = []model.Project{}
	}

	id := store.NewID()
	rel := filepath.Join(workspacesDir, id+".json")
	p, err := r.absPath(rel)
	if err != nil {
		return Info{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Info{}, err
	}
	if err := fileutil.WriteJSONAtomic(p, projects, 0o644); err != nil {
		return Info{}, err
	}
	// Load before persisting the mapping: a settings entry must never point at
	// a file that failed to load.
	st, err := store.New(p)
	if err != nil {
		_ = os.Remove(p)
		return Info{}, err
	}

	next := r.settings.clone()
	next.Workspaces = append(next.Workspaces, entry{ID: id, Name: name, File: rel})
	next.Current = id
	if err := r.saveSettings(next); err != nil {
		_ = os.Remove(p)
		return Info{}, err
	}
	r.settings = next
	r.active = st
	r.activeID = id
	log.Printf("已创建工作空间：%s（%d 个工程）", name, len(projects))
	return Info{ID: id, Name: name}, nil
}

// Rename changes a workspace's display name. The file is untouched.
func (r *Registry) Rename(id, rawName string) (Info, error) {
	name, err := normalizeName(rawName)
	if err != nil {
		return Info{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexOfLocked(id)
	if i < 0 {
		return Info{}, ErrNotFound
	}
	if r.hasNameLocked(name, id) {
		return Info{}, ErrDuplicateName
	}

	next := r.settings.clone()
	next.Workspaces[i].Name = name
	if err := r.saveSettings(next); err != nil {
		return Info{}, err
	}
	r.settings = next
	log.Printf("工作空间已重命名：%s", name)
	return Info{ID: id, Name: name}, nil
}

// Delete removes a workspace and archives its file into data/trash so an
// accidental click never destroys projects outright.
//
// Deleting the active workspace switches to the first other workspace that
// loads; the last remaining workspace cannot be deleted.
//
// The steps are ordered so that a failure leaves everything as it was:
//
//	1. copy the file into trash (the original stays put, so the registry keeps a
//	   workspace whose file still exists);
//	2. write settings.json;
//	3. move the active pointer, now that the removal is on disk;
//	4. delete the original.
//
// Doing it the other way round — move the file aside, then write settings — is
// how a workspace could be lost silently: when the settings write failed, the
// rollback's error was discarded, so the file stayed in trash while the in-memory
// store still pointed at it. Every later save then recreated the file at the old
// path (WriteAtomic uses os.WriteFile) and reported success for a workspace that
// no longer existed in settings.json and vanished on the next restart.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	i := r.indexOfLocked(id)
	if i < 0 {
		return ErrNotFound
	}
	if len(r.settings.Workspaces) == 1 {
		return ErrLastWorkspace
	}
	w := r.settings.Workspaces[i]

	next := r.settings.clone()
	next.Workspaces = append(next.Workspaces[:i], next.Workspaces[i+1:]...)

	var (
		nextStore *store.Store
		nextID    string
	)
	if r.activeID == id {
		fallbackID, st, err := r.loadFallbackLocked(next.Workspaces)
		if err != nil {
			return fmt.Errorf("无法切换到其他工作空间: %w", err)
		}
		nextID = fallbackID
		nextStore = st
		next.Current = fallbackID
	}

	p, err := r.absPath(w.File)
	if err != nil {
		return err
	}

	// 1. Take the backup first, but by copying rather than moving. A copy leaves
	// the workspace file in place, so if the settings write below fails nothing
	// has moved and the caller can simply retry.
	if err := r.backupLocked(p, w.ID); err != nil {
		return fmt.Errorf("删除工作空间「%s」失败：无法创建备份，工作空间未删除: %w", w.Name, err)
	}

	// 2. Persist the removal. On failure, remove the stale backup so a retry
	// does not accumulate copies of a workspace that still exists. The message
	// names the workspace: the underlying error is a bare rename failure that
	// says nothing about what the user was doing.
	if err := r.saveSettings(next); err != nil {
		r.discardBackupLocked()
		return fmt.Errorf("删除工作空间「%s」失败：保存工作空间设置失败，工作空间未删除: %w", w.Name, err)
	}
	r.settings = next

	// 3. From here the workspace is logically gone, so the active pointer must
	// follow before anything else can observe the old one.
	if nextStore != nil {
		r.active = nextStore
		r.activeID = nextID
	}

	// 4. Drop the original. The backup is already on disk, so this failing only
	// leaves a stray file behind — the workspace is gone from settings.json
	// either way and can never be written again by accident.
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		log.Printf("工作空间「%s」已删除并备份，但原文件 %s 移除失败（可手动删除）：%v", w.Name, p, err)
	}
	log.Printf("已删除工作空间：%s", w.Name)
	return nil
}

// backupLocked copies the workspace file into data/trash so an accidental delete
// stays recoverable. It copies instead of renaming so the original keeps serving
// the registry until settings.json has committed the removal.
func (r *Registry) backupLocked(p, id string) error {
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to back up (file already gone). The entry can still be
			// removed; this is the "settings points at a missing file" repair.
			return nil
		}
		return err
	}
	dir := filepath.Join(r.dir, trashDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	stamp := time.Now().Format("20060102-150405")
	target := filepath.Join(dir, base+"-"+stamp+".json")
	for n := 2; ; n++ {
		if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
			break
		} else if statErr != nil {
			return statErr
		}
		target = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", base, stamp, n))
	}
	if err := fileutil.WriteAtomic(target, data, info.Mode().Perm()); err != nil {
		return err
	}
	r.lastBackup = target
	return nil
}

// discardBackupLocked removes the backup created by backupLocked, used when the
// settings write failed and the delete did not happen after all.
func (r *Registry) discardBackupLocked() {
	if r.lastBackup == "" {
		return
	}
	_ = os.Remove(r.lastBackup)
	r.lastBackup = ""
}

func (r *Registry) indexOfLocked(id string) int {
	for i := range r.settings.Workspaces {
		if r.settings.Workspaces[i].ID == id {
			return i
		}
	}
	return -1
}

func (r *Registry) infoAtLocked(id string) Info {
	if i := r.indexOfLocked(id); i >= 0 {
		return Info{ID: r.settings.Workspaces[i].ID, Name: r.settings.Workspaces[i].Name}
	}
	return Info{ID: id}
}

// hasNameLocked reports whether another workspace already uses name. Names are
// compared case-insensitively so "Test" and "test" cannot both exist and
// confuse the switcher.
func (r *Registry) hasNameLocked(name, exceptID string) bool {
	for _, w := range r.settings.Workspaces {
		if w.ID == exceptID {
			continue
		}
		if strings.EqualFold(w.Name, name) {
			return true
		}
	}
	return false
}
