package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/netip"
	"strings"

	"github.com/cweimann/bookmarker/internal/store"
)

// Server is the Bookmarker HTTP API + UI.
type Server struct {
	Store  *store.Store
	Static fs.FS
	// Allow lists client networks; empty means loopback only.
	Allow []netip.Prefix
	// Hosts are Host-header names accepted besides IP literals and localhost.
	Hosts []string
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
		files := http.StripPrefix("/static/", http.FileServer(http.FS(s.Static)))
		mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/") {
				http.NotFound(w, r)
				return
			}
			files.ServeHTTP(w, r)
		}))
	}
	return s.guard(mux)
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
	if !requireMethod(w, r, http.MethodGet) {
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
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	// Send the tab back to the page it saved. Referer is useless here: browsers
	// trim it to the origin (or drop it) on cross-site navigations.
	lower := strings.ToLower(b.URL)
	if r.URL.Query().Get("noredirect") == "" &&
		(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		http.Redirect(w, r, b.URL, http.StatusFound)
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
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	listing, err := s.Store.List(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, listing)
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPath string `json:"parentPath"`
		Title      string `json:"title"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respond(w)(s.Store.CreateFolder(body.ParentPath, body.Title))
}

func (s *Server) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respond(w)(s.Store.RenameFolder(body.Path, body.Title))
}

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respondOK(w, s.Store.DeleteFolder(body.Path))
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respond(w)(s.Store.CreateCategory(body.Path, body.Title))
}

func (s *Server) handleCategoryMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug     string `json:"slug"`
		FromPath string `json:"fromPath"`
		ToPath   string `json:"toPath"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respondOK(w, s.Store.MoveCategory(body.Slug, body.FromPath, body.ToPath))
}

func (s *Server) handleCategoryRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path  string `json:"path"`
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respond(w)(s.Store.RenameCategory(body.Path, body.Slug, body.Title))
}

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	list, err := s.Store.ListBookmarks(r.URL.Query().Get("path"), r.URL.Query().Get("category"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []store.Bookmark{}
	}
	writeJSON(w, list)
}

func (s *Server) handleBookmarkCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path     string `json:"path"`
		Category string `json:"category"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Source   string `json:"source"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	if body.Source == "" {
		body.Source = "manual"
	}
	respond(w)(s.Store.AddBookmark(body.Path, body.Category, body.Title, body.URL, body.Source))
}

func (s *Server) handleBookmarkUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Path     string `json:"path"`
		Category string `json:"category"`
		Title    string `json:"title"`
		URL      string `json:"url"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respond(w)(s.Store.UpdateBookmark(body.Path, body.Category, body.ID, body.Title, body.URL))
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID           string `json:"id"`
		FromPath     string `json:"fromPath"`
		FromCategory string `json:"fromCategory"`
		ToPath       string `json:"toPath"`
		ToCategory   string `json:"toCategory"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respondOK(w, s.Store.MoveBookmark(body.ID, body.FromPath, body.FromCategory, body.ToPath, body.ToCategory))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Path     string `json:"path"`
		Category string `json:"category"`
	}
	if !decodePost(w, r, &body) {
		return
	}
	respondOK(w, s.Store.DeleteBookmark(body.ID, body.Path, body.Category))
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func decodePost(w http.ResponseWriter, r *http.Request, v any) bool {
	if !requireMethod(w, r, http.MethodPost) {
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONError(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// respond(w)(store.Op(...)) writes the result as JSON or maps the error.
func respond(w http.ResponseWriter) func(any, error) {
	return func(v any, err error) {
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, v)
	}
}

func respondOK(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, err)
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

func statusFor(err error) int {
	switch {
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps store errors to status codes. Unexpected errors are logged
// and reported generically, since OS errors carry absolute paths.
func writeErr(w http.ResponseWriter, err error) {
	code := statusFor(err)
	msg := err.Error()
	if code == http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
		msg = "internal error (see server log)"
	}
	writeJSONError(w, msg, code)
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
