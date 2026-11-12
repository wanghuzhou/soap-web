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
	for i := range projects {
		if err := projects[i].Proxy.Validate(); err != nil {
			return Info{}, fmt.Errorf("%w：工程「%s」的代理配置无效: %v", ErrInvalidProject, projects[i].Name, err)
		}
		if err := model.ValidateProjectResponses(projects[i]); err != nil {
			return Info{}, fmt.Errorf("%w：工程「%s」: %v", ErrInvalidProject, projects[i].Name, err)
		}
		store.EnsureIDs(&projects[i])
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

// Delete removes a workspace and moves its file to data/trash so an accidental
// click never destroys projects outright.
//
// Deleting the active workspace switches to the first other workspace that
// loads; the last remaining workspace cannot be deleted.
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
	trashPath, moved, err := r.moveToTrash(p)
	if err != nil {
		return fmt.Errorf("移入回收站失败: %w", err)
	}
	if err := r.saveSettings(next); err != nil {
		if moved {
			_ = os.Rename(trashPath, p)
		}
		return err
	}

	r.settings = next
	if nextStore != nil {
		r.active = nextStore
		r.activeID = nextID
	}
	log.Printf("已删除工作空间：%s", w.Name)
	return nil
}

// moveToTrash renames a workspace file into data/trash. It reports whether a
// file was actually moved, so the caller can roll back on a failed settings
// write.
func (r *Registry) moveToTrash(p string) (string, bool, error) {
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	dir := filepath.Join(r.dir, trashDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	stamp := time.Now().Format("20060102-150405")
	target := filepath.Join(dir, base+"-"+stamp+".json")
	for n := 2; ; n++ {
		_, err := os.Stat(target)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", false, err
		}
		target = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", base, stamp, n))
	}
	if err := os.Rename(p, target); err != nil {
		return "", false, err
	}
	return target, true, nil
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
