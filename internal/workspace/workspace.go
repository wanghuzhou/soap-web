// Package workspace manages the set of project files ("workspaces") and which
// one is currently active.
//
// A workspace is nothing more than a projects.json-format file: a JSON array of
// model.Project. settings.json records the display names and the mapping to
// those files, plus the id of the active one, so the next start resumes where
// the user left off.
//
// File paths are an implementation detail: the API only exposes ids and names.
// That is why renaming a workspace is a metadata-only operation and never
// touches the filesystem.
//
// Concurrency: the registry is safe for concurrent use. A switch loads and
// validates the target file BEFORE writing settings.json, so a corrupt target
// leaves both the in-memory state and the settings file untouched.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"soapweb/internal/store"
)

const (
	settingsVersion = 1
	settingsFile    = "settings.json"
	defaultFileName = "projects.json"
	defaultName     = "默认"
	workspacesDir   = "workspaces"
	trashDir        = "trash"
	maxNameRunes    = 40
)

var (
	// ErrNotFound is returned when a workspace id does not exist.
	ErrNotFound = errors.New("未找到指定工作空间")
	// ErrLastWorkspace refuses deleting the only remaining workspace: the tool
	// always needs somewhere to write projects.
	ErrLastWorkspace = errors.New("至少需要保留一个工作空间")
	// ErrDuplicateName is returned for names that collide case-insensitively.
	ErrDuplicateName = errors.New("工作空间名称已存在")
	// ErrInvalidName wraps every name validation failure.
	ErrInvalidName = errors.New("工作空间名称无效")
)

// Info is the public view of a workspace. It deliberately carries no path:
// users switch by name, never by file.
type Info struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// entry is the on-disk form stored in settings.json.
type entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
}

// settings is the content of settings.json.
type settings struct {
	Version    int     `json:"version"`
	Current    string  `json:"current"`
	Workspaces []entry `json:"workspaces"`
}

// Registry owns settings.json and the currently active store.
type Registry struct {
	mu       sync.RWMutex
	dir      string
	path     string
	settings settings
	active   *store.Store
	activeID string
}

// Open loads (or initialises) the registry rooted at dir.
//
// legacyFile is the old -data projects.json path when the user still passes a
// file instead of a directory; it becomes the file of the initial "默认"
// workspace so existing data keeps working without a migration step.
//
// A missing data directory, a missing settings.json and an empty settings.json
// are all treated as a fresh install: the directory and a default configuration
// are created before returning, so the caller never has to special-case them.
func Open(dir, legacyFile string) (*Registry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析数据目录 %q 失败: %w", dir, err)
	}

	// Check and create the data directory explicitly: os.MkdirAll alone would
	// work, but the separate stat lets the log say whether this is a fresh
	// install and lets the error name the directory that could not be created.
	if info, statErr := os.Stat(abs); statErr == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("数据目录 %s 已被同名文件占用，请移走该文件或改用 -data 指定其他目录", abs)
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录 %s 失败（请改用 -data 指定一个可写目录）: %w", abs, err)
		}
		log.Printf("数据目录不存在，已创建：%s", abs)
	} else {
		return nil, fmt.Errorf("检查数据目录 %s 失败: %w", abs, statErr)
	}

	r := &Registry{dir: abs, path: filepath.Join(abs, settingsFile)}

	data, err := os.ReadFile(r.path)
	switch {
	case err == nil:
		data = stripBOM(data)
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			// An empty settings.json (a crashed write, or a file created by
			// hand) carries no configuration; treat it like a fresh install.
			log.Printf("%s 为空，按首次启动处理", r.path)
			if err := r.initialise(legacyFile); err != nil {
				return nil, err
			}
			break
		}
		if err := json.Unmarshal(trimmed, &r.settings); err != nil {
			return nil, fmt.Errorf("解析 %s 失败（请修复，或删除后重启以创建默认配置）: %w", r.path, err)
		}
		if err := r.validate(); err != nil {
			return nil, fmt.Errorf("%s 无效: %w", r.path, err)
		}
	case os.IsNotExist(err):
		if err := r.initialise(legacyFile); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("读取 %s 失败: %w", r.path, err)
	}

	if err := r.activateInitial(); err != nil {
		return nil, err
	}
	return r, nil
}

// stripBOM removes a UTF-8 byte-order mark. Windows editors (notably older
// Notepad) add one by default, and encoding/json rejects it as an invalid
// character at the start of the document.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

// DataDir returns the directory that holds settings.json and the workspace
// files. Used for startup logging.
func (r *Registry) DataDir() string { return r.dir }

// initialise creates the default configuration for a fresh data directory: an
// empty workspace file plus settings.json pointing at it. An existing legacy
// projects.json is adopted untouched.
func (r *Registry) initialise(legacyFile string) error {
	file := defaultFileName
	if legacyFile != "" {
		file = filepath.Base(legacyFile)
	}

	p, err := r.absPath(file)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("创建默认工作空间目录失败: %w", err)
		}
		if err := os.WriteFile(p, []byte("[]\n"), 0o644); err != nil {
			return fmt.Errorf("创建默认工作空间文件 %s 失败: %w", p, err)
		}
		log.Printf("工作空间文件不存在，已创建空文件：%s", p)
	} else if statErr != nil {
		return fmt.Errorf("检查工作空间文件 %s 失败: %w", p, statErr)
	}

	id := store.NewID()
	r.settings = settings{
		Version:    settingsVersion,
		Current:    id,
		Workspaces: []entry{{ID: id, Name: defaultName, File: file}},
	}
	if err := r.saveSettings(r.settings); err != nil {
		return fmt.Errorf("创建默认配置 %s 失败: %w", r.path, err)
	}
	log.Printf("未找到 %s，已创建默认配置（工作空间「%s」→ %s）", r.path, defaultName, file)
	return nil
}

// validate checks a settings file loaded from disk. A hand-edited file with a
// bad path or duplicate name is rejected loudly instead of being silently
// "repaired" into something the user did not ask for.
func (r *Registry) validate() error {
	if r.settings.Version == 0 {
		r.settings.Version = settingsVersion
	}
	if len(r.settings.Workspaces) == 0 {
		return errors.New("没有任何工作空间")
	}
	ids := make(map[string]bool, len(r.settings.Workspaces))
	names := make(map[string]string, len(r.settings.Workspaces))
	files := make(map[string]string, len(r.settings.Workspaces))
	for i := range r.settings.Workspaces {
		w := &r.settings.Workspaces[i]
		if w.ID == "" {
			return fmt.Errorf("第 %d 项缺少 id", i+1)
		}
		if ids[w.ID] {
			return fmt.Errorf("工作空间 id %q 重复", w.ID)
		}
		ids[w.ID] = true
		if _, err := normalizeName(w.Name); err != nil {
			return fmt.Errorf("工作空间 %q: %w", w.ID, err)
		}
		key := strings.ToLower(w.Name)
		if other, ok := names[key]; ok {
			return fmt.Errorf("工作空间名称 %q 重复（id %s 与 %s）", w.Name, other, w.ID)
		}
		names[key] = w.ID
		if _, err := r.absPath(w.File); err != nil {
			return fmt.Errorf("工作空间 %q 的文件路径无效: %w", w.Name, err)
		}
		// Two entries pointing at one file would make delete/rename move data
		// out from under the other entry. Reject it instead of guessing.
		fileKey := strings.ToLower(filepath.Clean(w.File))
		if other, ok := files[fileKey]; ok {
			return fmt.Errorf("工作空间 %q 与 %q 指向同一个文件", other, w.ID)
		}
		files[fileKey] = w.ID
	}
	return nil
}

// activateInitial opens the current workspace, falling back to the first one
// that loads when the configured current file is corrupt. Falling back (rather
// than refusing to start) keeps the other workspaces reachable so the user can
// delete or repair the broken one from the UI.
func (r *Registry) activateInitial() error {
	wanted := r.settings.Current
	wantedName := wanted
	order := make([]int, 0, len(r.settings.Workspaces))
	current := r.indexOfLocked(wanted)
	if current >= 0 {
		wantedName = r.settings.Workspaces[current].Name
		order = append(order, current)
	}
	for i := range r.settings.Workspaces {
		if i != current {
			order = append(order, i)
		}
	}

	var firstErr error
	for _, i := range order {
		w := r.settings.Workspaces[i]
		p, err := r.absPath(w.File)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("工作空间「%s」路径无效：%v", w.Name, err)
			continue
		}
		st, err := store.New(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("工作空间「%s」加载失败：%v", w.Name, err)
			continue
		}
		if r.settings.Current != w.ID {
			next := r.settings
			next.Current = w.ID
			if err := r.saveSettings(next); err != nil {
				log.Printf("保存当前工作空间失败：%v", err)
			}
			r.settings = next
			log.Printf("工作空间「%s」不可用，已切换到「%s」", wantedName, w.Name)
		}
		r.active = st
		r.activeID = w.ID
		return nil
	}
	if firstErr == nil {
		firstErr = errors.New("没有可用的工作空间文件")
	}
	return fmt.Errorf("没有可用的工作空间: %w", firstErr)
}

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

// Create adds a workspace and immediately makes it active, matching the
// expected "新建并进入" behaviour.
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

	id := store.NewID()
	rel := filepath.Join(workspacesDir, id+".json")
	p, err := r.absPath(rel)
	if err != nil {
		return Info{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Info{}, err
	}
	// Write a real (empty) file immediately: the mapping in settings.json then
	// never points at a file that does not exist.
	if err := os.WriteFile(p, []byte("[]\n"), 0o644); err != nil {
		return Info{}, err
	}
	// Load before persisting the mapping, mirroring Switch: a settings entry
	// must never point at a file that failed to load.
	st, err := store.New(p)
	if err != nil {
		_ = os.Remove(p)
		return Info{}, err
	}

	next := r.settings
	next.Workspaces = make([]entry, 0, len(r.settings.Workspaces)+1)
	next.Workspaces = append(next.Workspaces, r.settings.Workspaces...)
	next.Workspaces = append(next.Workspaces, entry{ID: id, Name: name, File: rel})
	next.Current = id
	if err := r.saveSettings(next); err != nil {
		_ = os.Remove(p)
		return Info{}, err
	}
	r.settings = next
	r.active = st
	r.activeID = id
	log.Printf("已创建工作空间：%s", name)
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

	next := r.settings
	next.Workspaces = make([]entry, len(r.settings.Workspaces))
	copy(next.Workspaces, r.settings.Workspaces)
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

	next := r.settings
	next.Workspaces = make([]entry, 0, len(r.settings.Workspaces)-1)
	next.Workspaces = append(next.Workspaces, r.settings.Workspaces[:i]...)
	next.Workspaces = append(next.Workspaces, r.settings.Workspaces[i+1:]...)

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

// loadFallbackLocked opens the first workspace in list that loads.
func (r *Registry) loadFallbackLocked(list []entry) (string, *store.Store, error) {
	var firstErr error
	for _, w := range list {
		p, err := r.absPath(w.File)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		st, err := store.New(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return w.ID, st, nil
	}
	if firstErr == nil {
		firstErr = errors.New("没有可用的工作空间")
	}
	return "", nil, firstErr
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

// saveSettings writes settings.json atomically (temp file + rename), so a crash
// mid-write cannot leave a truncated mapping behind.
func (r *Registry) saveSettings(s settings) error {
	if s.Version == 0 {
		s.Version = settingsVersion
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// absPath resolves a workspace's stored relative file to an absolute path and
// rejects anything that escapes the data directory. settings.json is user
// editable, so this is the boundary that keeps a typo (or a malicious edit)
// from reading/writing arbitrary files.
func (r *Registry) absPath(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("文件路径为空")
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("必须是数据目录内的相对路径")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("不能指向数据目录之外")
	}
	abs := filepath.Join(r.dir, clean)
	inside, err := filepath.Rel(r.dir, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", errors.New("不能指向数据目录之外")
	}
	return abs, nil
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

// normalizeName trims and validates a user-supplied workspace name.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w：名称不能为空", ErrInvalidName)
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", fmt.Errorf("%w：名称不能超过 %d 个字符", ErrInvalidName, maxNameRunes)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w：名称包含控制字符", ErrInvalidName)
		}
	}
	return name, nil
}
