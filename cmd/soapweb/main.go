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
	"soapweb/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8420", "listen address")
	open := flag.Bool("open", true, "启动后自动打开浏览器")
	flag.Parse()

	srv := &api.Server{StaticFS: web.FS()}
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
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux / *nix
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
