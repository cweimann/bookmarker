package server

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"

	"github.com/cweimann/bookmarker/internal/store"
)

// Server is the Bookmarker HTTP API + UI.
type Server struct {
	Store  *store.Store
	Static fs.FS
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/add", s.handleAdd)
	mux.HandleFunc("/api/folders", s.handleFolders)
	mux.HandleFunc("/api/categories", s.handleCategories)
	mux.HandleFunc("/api/categories/move", s.handleCategoryMove)
	mux.HandleFunc("/api/bookmarks", s.handleBookmarks)
	mux.HandleFunc("/api/move", s.handleMove)
	mux.HandleFunc("/api/bookmarks/delete", s.handleDelete)
	if s.Static != nil {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.Static))))
	}
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.Static == nil {
		http.Error(w, "UI not embedded", http.StatusInternalServerError)
		return
	}
	data, err := fs.ReadFile(s.Static, "index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	url := first(r.Form.Get("url"), r.URL.Query().Get("url"))
	title := first(r.Form.Get("title"), r.URL.Query().Get("title"))
	source := first(r.Form.Get("source"), r.URL.Query().Get("source"), "bookmarklet")
	folder := first(r.Form.Get("folder"), r.URL.Query().Get("folder"), store.DefaultFolder)

	b, err := s.Store.AddToFolderInbox(folder, title, url, source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if ref := r.Header.Get("Referer"); ref != "" && r.URL.Query().Get("noredirect") == "" {
		http.Redirect(w, r, ref, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Saved</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
body{font-family:system-ui,sans-serif;max-width:28rem;margin:3rem auto;padding:0 1rem;color:#1a1a1a}
a{color:#0b57d0}
</style></head><body>
<h1>Saved</h1>
<p><strong>%s</strong></p>
<p><a href="%s">%s</a></p>
<p>Added to %s inbox. <a href="/">Open Bookmarker</a></p>
</body></html>`,
		html.EscapeString(b.Title),
		html.EscapeString(b.URL),
		html.EscapeString(b.URL),
		html.EscapeString(folder),
	)
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folders, err := s.Store.ListFolders()
		if err != nil {
			writeErr(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, folders)
	case http.MethodPost:
		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, err, http.StatusBadRequest)
			return
		}
		f, err := s.Store.CreateFolder(body.Title)
		if err != nil {
			writeErr(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, f)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folder := r.URL.Query().Get("folder")
		cats, err := s.Store.ListCategories(folder)
		if err != nil {
			writeErr(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, cats)
	case http.MethodPost:
		var body struct {
			Folder string `json:"folder"`
			Title  string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, err, http.StatusBadRequest)
			return
		}
		cat, err := s.Store.CreateCategory(body.Folder, body.Title)
		if err != nil {
			writeErr(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, cat)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCategoryMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Slug       string `json:"slug"`
		FromFolder string `json:"fromFolder"`
		ToFolder   string `json:"toFolder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.Store.MoveCategory(body.Slug, body.FromFolder, body.ToFolder); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	folder := r.URL.Query().Get("folder")
	category := r.URL.Query().Get("category")
	list, err := s.Store.ListBookmarks(folder, category)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []store.Bookmark{}
	}
	writeJSON(w, list)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID           string `json:"id"`
		FromFolder   string `json:"fromFolder"`
		FromCategory string `json:"fromCategory"`
		ToFolder     string `json:"toFolder"`
		ToCategory   string `json:"toCategory"`
		// Back-compat aliases
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	fromCat := first(body.FromCategory, body.From)
	toCat := first(body.ToCategory, body.To)
	fromFolder := body.FromFolder
	toFolder := body.ToFolder
	if fromFolder == "" {
		fromFolder = store.DefaultFolder
	}
	if toFolder == "" {
		toFolder = fromFolder
	}
	if err := s.Store.MoveBookmark(body.ID, fromFolder, fromCat, toFolder, toCat); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID       string `json:"id"`
		Folder   string `json:"folder"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	folder := body.Folder
	if folder == "" {
		folder = store.DefaultFolder
	}
	if err := s.Store.DeleteBookmark(body.ID, folder, body.Category); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
