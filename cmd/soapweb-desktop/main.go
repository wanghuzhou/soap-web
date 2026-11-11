// Command soapweb-desktop runs soap-web inside a native window (Wails +
// WebView2) while reusing the exact HTTP API and embedded frontend from
// cmd/soapweb. No TCP port is opened: Wails' AssetServer forwards requests to
// the same http.ServeMux the browser entry point serves.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"soapweb/internal/api"
	"soapweb/internal/applog"
	"soapweb/internal/appsetup"
	"soapweb/web"
)

func main() {
	dataPath := flag.String("data", "", "数据目录（默认：可执行文件旁 data）")
	flag.Parse()

	dir, legacy := appsetup.ResolveDataDir(*dataPath)
	if path, err := applog.Init(appsetup.LogDir(dir)); err != nil {
		log.Printf("日志文件不可用，仅输出到控制台：%v", err)
	} else {
		log.Printf("日志文件：%s", path)
	}
	defer applog.Close()

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

	// AssetServer forwards every non-GET request, and GETs it cannot serve from
	// Assets, to this handler. The existing REST API and embedded frontend run
	// unchanged, and no localhost listener is needed.
	err = wails.Run(&options.App{
		Title:     "soap-web",
		Width:     1360,
		Height:    860,
		MinWidth:  960,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Handler: mux,
		},
		Windows: &windows.Options{
			// Keep WebView2's profile inside the app data directory instead of
			// %APPDATA%\<exe name>; a stale profile there makes controller
			// creation fail with 8000ffff.
			WebviewUserDataPath:  filepath.Join(dir, "webview2"),
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})
	if err != nil {
		log.Printf("soap-web 桌面版退出：%v", err)
		os.Exit(1)
	}
}
