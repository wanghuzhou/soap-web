package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"soapweb/internal/api"
	"soapweb/internal/workspace"
	"soapweb/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8420", "listen address")
	open := flag.Bool("open", true, "启动后自动打开浏览器")
	dataPath := flag.String("data", "", "数据目录（默认：可执行文件旁 data）；兼容旧用法：也可传 projects.json 路径")
	flag.Parse()

	// On Windows, switch the console to UTF-8 so Chinese log lines display
	// correctly regardless of the system default code page.
	setConsoleUTF8()

	// Resolve the data directory. Default to a data/ dir next to the executable
	// so settings and workspaces persist regardless of the working directory.
	// When that location is not writable (for example the exe sits in a
	// read-only folder), fall back to a per-user directory instead of refusing
	// to start. An explicit -data path never falls back: the user asked for it.
	dir, legacy := resolveDataDir(*dataPath)
	reg, err := openRegistry(dir, legacy, *dataPath == "")
	if err != nil {
		log.Printf("加载数据目录失败 %s：%v", dir, err)
		os.Exit(1)
	}
	log.Printf("数据目录：%s", reg.DataDir())
	log.Printf("当前工作空间：%s", reg.CurrentInfo().Name)

	srv := &api.Server{StaticFS: web.FS(), Workspaces: reg}
	mux := http.NewServeMux()
	srv.Register(mux)

	url := "http://" + *addr

	// Pre-check the port so a stale instance produces a clear message.
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Printf("无法监听 %s：%v（端口可能已被占用）", *addr, err)
		os.Exit(1)
	}

	log.Printf("soap-web 已启动：%s", url)
	if *open {
		go func() {
			// Give the server a moment, then open the browser.
			time.Sleep(400 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				log.Printf("自动打开浏览器失败（请手动访问 %s）：%v", url, err)
			}
		}()
	}

	if err := http.Serve(ln, mux); err != nil {
		log.Printf("server error: %v", err)
		os.Exit(1)
	}
}

// resolveDataDir turns the -data flag into a data directory plus an optional
// legacy projects.json file to adopt as the initial workspace.
//
// The old flag took a projects.json path, and existing shortcuts pass one. We
// keep that working: a .json path means "use its directory as the data root and
// this file as the default workspace". A directory (or no flag) means the new
// layout, with settings.json inside it.
func resolveDataDir(raw string) (dir, legacy string) {
	if raw == "" {
		base := "."
		if exe, err := os.Executable(); err == nil {
			base = filepath.Dir(exe)
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

// openRegistry opens the data directory, falling back to a per-user location
// when the default (next to the executable) is not writable.
//
// Only permission errors trigger the fallback: a corrupt settings.json must
// keep failing loudly instead of silently starting on a fresh, empty data set.
// An explicit -data path never falls back — the user named that exact directory.
func openRegistry(dir, legacy string, allowFallback bool) (*workspace.Registry, error) {
	reg, err := workspace.Open(dir, legacy)
	if err == nil {
		return reg, nil
	}
	if !allowFallback || !errors.Is(err, os.ErrPermission) {
		return nil, err
	}

	fallback, ferr := userDataDir()
	if ferr != nil {
		return nil, err
	}
	if samePath(fallback, dir) {
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

// userDataDir returns a per-user writable location for settings and workspaces.
func userDataDir() (string, error) {
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

// samePath reports whether two paths point at the same directory, tolerating
// Windows' case-insensitive filesystem.
func samePath(a, b string) bool {
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

// openBrowser opens the given URL in the platform's default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use the cmd built-in `start`, which follows the current default-browser
		// association. `rundll32 url.dll,FileProtocolHandler` silently fails to
		// open a browser on some Windows 10/11 configurations.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux / *nix
		cmd = exec.Command("xdg-open", url)
	}
	if runtime.GOOS == "windows" {
		// cmd exits immediately after handing the URL to the shell, but waiting
		// lets us report a genuinely missing browser association.
		return cmd.Run()
	}
	return cmd.Start()
}
