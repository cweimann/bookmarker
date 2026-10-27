package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cweimann/bookmarker/internal/config"
	"github.com/cweimann/bookmarker/internal/server"
	"github.com/cweimann/bookmarker/internal/store"
)

//go:embed web/*
var webFS embed.FS

func main() {
	configPath := flag.String("config", os.Getenv("BOOKMARKER_CONFIG"), "config file (default: ./"+config.FileName+", then next to the executable)")
	addr := flag.String("addr", os.Getenv("BOOKMARKER_ADDR"), "comma-separated listen addresses; overrides the config file's listen list")
	dataDir := flag.String("data", os.Getenv("BOOKMARKER_DATA"), "bookmark directory; overrides the config file's data")
	flag.Parse()

	path, err := config.Find(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	if *addr != "" {
		cfg.Listen = splitList(*addr)
	}
	if *dataDir != "" {
		if cfg.Data, err = filepath.Abs(*dataDir); err != nil {
			log.Fatal(err)
		}
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	editNets, err := cfg.EditNets()
	if err != nil {
		log.Fatal(err)
	}
	viewNets, err := cfg.ViewNets()
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.New(cfg.Data)
	if err != nil {
		log.Fatal(err)
	}
	static, source, err := loadStatic()
	if err != nil {
		log.Fatal(err)
	}
	handler := (&server.Server{
		Store:  st,
		Static: static,
		Edit:   editNets,
		View:   viewNets,
		Hosts:  cfg.Hosts,
	}).Handler()

	if path == "" {
		log.Printf("config: none found, using defaults (%s)", config.FileName)
	} else {
		log.Printf("config: %s", path)
	}
	log.Printf("data directory: %s", cfg.Data)
	log.Printf("UI assets: %s", source)
	log.Printf("edit clients: %s", joinOrNone(cfg.Edit))
	log.Printf("view clients: %s (plus edit)", joinOrNone(cfg.View))

	// An address that cannot be bound (say, the LAN interface is down) is
	// logged and skipped so loopback keeps working; no listeners is fatal.
	errs := make(chan error, len(cfg.Listen))
	bound := 0
	for _, a := range cfg.Listen {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			log.Printf("listen %s: %v (skipped)", a, err)
			continue
		}
		bound++
		log.Printf("bookmarker listening on http://%s", ln.Addr())
		srv := &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		go func() { errs <- srv.Serve(ln) }()
	}
	if bound == 0 {
		log.Fatal("no listen address could be bound")
	}
	if err := <-errs; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinOrNone(list []string) string {
	if len(list) == 0 {
		return "(none)"
	}
	return strings.Join(list, ", ")
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
