package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"soapweb/internal/api"
	"soapweb/internal/applog"
	"soapweb/internal/appsetup"
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
	dir, legacy := appsetup.ResolveDataDir(*dataPath)
	if path, err := applog.Init(appsetup.LogDir(dir)); err != nil {
		log.Printf("日志文件不可用，仅输出到控制台：%v", err)
	} else {
		log.Printf("日志文件：%s", path)
	}
	reg, err := appsetup.OpenRegistry(dir, legacy, *dataPath == "")
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
