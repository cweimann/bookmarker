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
	mux.HandleFunc("/api/listing", s.handleListing)
	mux.HandleFunc("/api/folders", s.handleFolders)
	mux.HandleFunc("/api/folders/rename", s.handleFolderRename)
	mux.HandleFunc("/api/folders/delete", s.handleFolderDelete)
	mux.HandleFunc("/api/categories", s.handleCategories)
	mux.HandleFunc("/api/categories/move", s.handleCategoryMove)
	mux.HandleFunc("/api/categories/rename", s.handleCategoryRename)
	mux.HandleFunc("/api/bookmarks", s.handleBookmarks)
	mux.HandleFunc("/api/bookmarks/create", s.handleBookmarkCreate)
	mux.HandleFunc("/api/bookmarks/update", s.handleBookmarkUpdate)
	mux.HandleFunc("/api/bookmarks/delete", s.handleDelete)
	mux.HandleFunc("/api/move", s.handleMove)
	mux.HandleFunc("/api/config", s.handleConfig)
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

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"maxColumns": store.MaxColumns,
	})
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

	b, err := s.Store.AddToRootInbox(title, url, source)
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
<p>Added to root Inbox. <a href="/">Open Bookmarker</a></p>
</body></html>`,
		html.EscapeString(b.Title),
		html.EscapeString(b.URL),
		html.EscapeString(b.URL),
	)
}

func (s *Server) handleListing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	listing, err := s.Store.List(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, listing)
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ParentPath string `json:"parentPath"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	f, err := s.Store.CreateFolder(body.ParentPath, body.Title)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, f)
}

func (s *Server) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	f, err := s.Store.RenameFolder(body.Path, body.Title)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, f)
}

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteFolder(body.Path); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	cat, err := s.Store.CreateCategory(body.Path, body.Title)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, cat)
}

func (s *Server) handleCategoryMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Slug     string `json:"slug"`
		FromPath string `json:"fromPath"`
		ToPath   string `json:"toPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.Store.MoveCategory(body.Slug, body.FromPath, body.ToPath); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleCategoryRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path  string `json:"path"`
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	cat, err := s.Store.RenameCategory(body.Path, body.Slug, body.Title)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, cat)
}

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := s.Store.ListBookmarks(r.URL.Query().Get("path"), r.URL.Query().Get("category"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []store.Bookmark{}
	}
	writeJSON(w, list)
}

func (s *Server) handleBookmarkCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path     string `json:"path"`
		Category string `json:"category"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Source   string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if body.Source == "" {
		body.Source = "manual"
	}
	b, err := s.Store.AddBookmark(body.Path, body.Category, body.Title, body.URL, body.Source)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, b)
}

func (s *Server) handleBookmarkUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID       string `json:"id"`
		Path     string `json:"path"`
		Category string `json:"category"`
		Title    string `json:"title"`
		URL      string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	b, err := s.Store.UpdateBookmark(body.Path, body.Category, body.ID, body.Title, body.URL)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, b)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID           string `json:"id"`
		FromPath     string `json:"fromPath"`
		FromCategory string `json:"fromCategory"`
		ToPath       string `json:"toPath"`
		ToCategory   string `json:"toCategory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.Store.MoveBookmark(body.ID, body.FromPath, body.FromCategory, body.ToPath, body.ToCategory); err != nil {
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
		Path     string `json:"path"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteBookmark(body.ID, body.Path, body.Category); err != nil {
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
