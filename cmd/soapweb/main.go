package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"soapweb/internal/api"
	"soapweb/internal/store"
	"soapweb/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8420", "listen address")
	open := flag.Bool("open", true, "启动后自动打开浏览器")
	dataPath := flag.String("data", "", "数据文件路径 (默认: 可执行文件旁 data/projects.json)")
	flag.Parse()

	// On Windows, switch the console to UTF-8 so Chinese log lines display
	// correctly regardless of the system default code page.
	setConsoleUTF8()

	// Resolve the data file. Default to a data/ dir next to the executable so
	// projects persist regardless of the working directory.
	dp := *dataPath
	if dp == "" {
		base := "."
		if exe, err := os.Executable(); err == nil {
			base = filepath.Dir(exe)
		}
		dp = filepath.Join(base, "data", "projects.json")
	}
	st, err := store.New(dp)
	if err != nil {
		log.Printf("加载数据文件失败 %s：%v", dp, err)
		os.Exit(1)
	}
	log.Printf("数据文件：%s", dp)

	srv := &api.Server{StaticFS: web.FS(), Store: st}
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
