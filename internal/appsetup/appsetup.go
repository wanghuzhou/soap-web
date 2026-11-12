// Package appsetup resolves the data directory and opens the workspace registry.
// Both entry points (browser and desktop) share these rules so they always read
// and write the same files.
package appsetup

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"soapweb/internal/applog"
	"soapweb/internal/workspace"
)

// LogDir returns the log directory that sits next to the data directory.
func LogDir(dataDir string) string {
	return filepath.Join(filepath.Dir(dataDir), applog.DirName)
}

// ResolveDataDir turns -data into a data directory plus an optional legacy
// projects.json. A .json path means "use its directory, adopt this file"; a
// directory (or no flag) uses the settings.json layout.
func ResolveDataDir(raw string) (dir, legacy string) {
	if raw == "" {
		base := "."
		if exe, err := os.Executable(); err == nil {
			base = filepath.Dir(exe)
		}
		// macOS 的桌面版跑在 .app 包里，可执行文件位于 Contents/MacOS，而整个
		// bundle 是被代码签名封印的资源。往里写 data/ 会让签名失效（codesign
		// --verify 报 "a sealed resource is missing or invalid"），签名/公证
		// 过的包在别人机器上会被 Gatekeeper 拒绝。所以默认改写到用户数据目录。
		if insideMacAppBundle(base) {
			if dir, err := UserDataDir(); err == nil {
				return dir, ""
			}
		}
		return filepath.Join(base, "data"), ""
	}
	if info, err := os.Stat(raw); err == nil {
		if info.IsDir() {
			return raw, ""
		}
		return filepath.Dir(raw), filepath.Base(raw)
	}
	if strings.EqualFold(filepath.Base(raw), "settings.json") {
		return filepath.Dir(raw), ""
	}
	if strings.EqualFold(filepath.Ext(raw), ".json") {
		return filepath.Dir(raw), filepath.Base(raw)
	}
	return raw, ""
}

// OpenRegistry opens the data directory, falling back to a per-user location
// when the default is not writable. Only permission errors trigger the
// fallback; an explicit -data path never does.
func OpenRegistry(dir, legacy string, allowFallback bool) (*workspace.Registry, error) {
	reg, err := workspace.Open(dir, legacy)
	if err == nil {
		return reg, nil
	}
	if !allowFallback || !errors.Is(err, os.ErrPermission) {
		return nil, err
	}

	fallback, ferr := UserDataDir()
	if ferr != nil {
		return nil, err
	}
	if SamePath(fallback, dir) {
		return nil, err
	}

	log.Printf("默认数据目录不可写：%s", dir)
	log.Printf("改用用户数据目录：%s（可用 -data 指定其他目录）", fallback)
	reg, ferr = workspace.Open(fallback, "")
	if ferr != nil {
		return nil, fmt.Errorf("默认数据目录不可写（%v），用户数据目录 %s 也不可用: %w", err, fallback, ferr)
	}
	return reg, nil
}

// UserDataDir returns a per-user writable location for settings and workspaces.
func UserDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "soap-web", "data"), nil
		}
	}
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "soap-web", "data"), nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".soap-web", "data"), nil
	}
	return "", errors.New("无法确定用户数据目录")
}

// insideMacAppBundle reports whether dir is the Contents/MacOS directory of a
// macOS .app bundle, where writing files would break the code signature.
func insideMacAppBundle(dir string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return strings.Contains(filepath.ToSlash(dir), ".app/Contents/MacOS")
}

// SamePath reports whether two paths point at the same directory, tolerating
// Windows' case-insensitive filesystem.
func SamePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	aa = filepath.Clean(aa)
	bb = filepath.Clean(bb)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}
