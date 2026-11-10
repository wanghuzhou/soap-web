// Package workspace manages the project files ("workspaces") and which one is
// active. A workspace is a projects.json-format file; settings.json maps
// display names to files and records the active id.
//
// Paths never leave this package: the API exposes ids and names only. A switch
// loads the target before writing settings.json, so a corrupt file leaves the
// current state untouched.
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

	"soapweb/internal/fileutil"
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
	// ErrInvalidProject wraps validation failures in an imported workspace.
	ErrInvalidProject = errors.New("导入的工作空间数据无效")
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

// clone returns a settings value whose Workspaces slice can be mutated without
// affecting the registry's copy.
func (s settings) clone() settings {
	out := s
	out.Workspaces = append([]entry(nil), s.Workspaces...)
	return out
}

// Open loads the registry at dir, creating the directory, settings.json and a
// default workspace when they are missing or settings.json is empty.
//
// legacyFile is the old -data projects.json path; it becomes the initial
// "默认" workspace so existing data keeps working without a migration.
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

	// A directory that exists but is not writable would let the app start and
	// then fail on the first save. Probe it now so main can fall back or report
	// a precise error before serving anything.
	probe, probeErr := os.CreateTemp(abs, ".soapweb-write-*")
	if probeErr != nil {
		return nil, fmt.Errorf("数据目录 %s 不可写（请改用 -data 指定一个可写目录）: %w", abs, probeErr)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	r := &Registry{dir: abs, path: filepath.Join(abs, settingsFile)}

	data, err := os.ReadFile(r.path)
	switch {
	case err == nil:
		data = fileutil.StripBOM(data)
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
	order := make([]entry, 0, len(r.settings.Workspaces))
	if i := r.indexOfLocked(wanted); i >= 0 {
		wantedName = r.settings.Workspaces[i].Name
		order = append(order, r.settings.Workspaces[i])
	}
	for _, w := range r.settings.Workspaces {
		if w.ID != wanted {
			order = append(order, w)
		}
	}

	w, st, err := r.openFirstStore(order, func(w entry, err error) {
		log.Printf("工作空间「%s」加载失败：%v", w.Name, err)
	})
	if err != nil {
		return fmt.Errorf("没有可用的工作空间: %w", err)
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

// loadFallbackLocked opens the first workspace in list that loads.
func (r *Registry) loadFallbackLocked(list []entry) (string, *store.Store, error) {
	w, st, err := r.openFirstStore(list, nil)
	if err != nil {
		return "", nil, err
	}
	return w.ID, st, nil
}

// openFirstStore tries each entry in order and returns the first workspace that
// loads. Skipped entries are reported through onSkip (may be nil); when none
// load, the first failure is returned.
func (r *Registry) openFirstStore(entries []entry, onSkip func(entry, error)) (entry, *store.Store, error) {
	var firstErr error
	for _, w := range entries {
		p, err := r.absPath(w.File)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if onSkip != nil {
				onSkip(w, err)
			}
			continue
		}
		st, err := store.New(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if onSkip != nil {
				onSkip(w, err)
			}
			continue
		}
		return w, st, nil
	}
	if firstErr == nil {
		firstErr = errors.New("没有可用的工作空间")
	}
	return entry{}, nil, firstErr
}

// saveSettings writes settings.json atomically.
func (r *Registry) saveSettings(s settings) error {
	if s.Version == 0 {
		s.Version = settingsVersion
	}
	return fileutil.WriteJSONAtomic(r.path, s, 0o644)
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
