package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"soapweb/internal/api"
	"soapweb/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8420", "listen address")
	flag.Parse()

	srv := &api.Server{StaticFS: web.FS()}
	mux := http.NewServeMux()
	srv.Register(mux)

	log.Printf("soap-web 启动：http://%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Printf("server error: %v", err)
		os.Exit(1)
	}
}
