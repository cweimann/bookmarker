package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cweimann/bookmarker/internal/server"
	"github.com/cweimann/bookmarker/internal/store"
)

//go:embed web/*
var webFS embed.FS

func main() {
	addr := flag.String("addr", envOr("BOOKMARKER_ADDR", "127.0.0.1:8787"), "listen address (host:port)")
	dataDir := flag.String("data", envOr("BOOKMARKER_DATA", "data"), "directory for Markdown bookmark files")
	flag.Parse()

	absData, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.New(absData)
	if err != nil {
		log.Fatal(err)
	}

	static, source, err := loadStatic()
	if err != nil {
		log.Fatal(err)
	}

	srv := &server.Server{Store: st, Static: static}
	log.Printf("bookmarker listening on http://%s", *addr)
	log.Printf("data directory: %s", absData)
	log.Printf("UI assets: %s", source)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

// Prefer ./web on disk when present so UI edits show without a rebuild.
// Fall back to the embedded copy for a lone bookmarker.exe.
func loadStatic() (fs.FS, string, error) {
	if info, err := os.Stat("web"); err == nil && info.IsDir() {
		return os.DirFS("web"), "web/ (disk)", nil
	}
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, "", err
	}
	return sub, "embedded", nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
