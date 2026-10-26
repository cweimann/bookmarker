package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Bookmark is one saved link.
type Bookmark struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source"`
	Saved  string `json:"saved"` // RFC3339
}

// Category is a named group of bookmarks (slug → file).
type Category struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Store reads and writes Markdown bookmark files under DataDir.
type Store struct {
	DataDir string
	mu      sync.Mutex
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// New creates a Store and ensures inbox + categories dirs exist.
func New(dataDir string) (*Store, error) {
	s := &Store{DataDir: dataDir}
	if err := os.MkdirAll(filepath.Join(dataDir, "categories"), 0o755); err != nil {
		return nil, err
	}
	inbox := filepath.Join(dataDir, "inbox.md")
	if _, err := os.Stat(inbox); os.IsNotExist(err) {
		if err := atomicWrite(inbox, "# Inbox\n\n"); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Slugify turns a title into a filesystem-safe category slug.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "category"
	}
	return s
}

func (s *Store) inboxPath() string {
	return filepath.Join(s.DataDir, "inbox.md")
}

func (s *Store) categoryPath(slug string) string {
	return filepath.Join(s.DataDir, "categories", slug+".md")
}

// ListCategories returns all categories (sorted by title).
func (s *Store) ListCategories() ([]Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.DataDir, "categories")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var cats []Category
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		title := parseTitle(string(data), slug)
		cats = append(cats, Category{Slug: slug, Title: title})
	}
	sort.Slice(cats, func(i, j int) bool {
		return strings.ToLower(cats[i].Title) < strings.ToLower(cats[j].Title)
	})
	return cats, nil
}

// CreateCategory writes data/categories/<slug>.md. Returns the new category.
func (s *Store) CreateCategory(title string) (Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	if title == "" {
		return Category{}, fmt.Errorf("title required")
	}
	slug := Slugify(title)
	path := s.categoryPath(slug)
	if _, err := os.Stat(path); err == nil {
		return Category{}, fmt.Errorf("category %q already exists", slug)
	}
	body := fmt.Sprintf("# %s\n\n", title)
	if err := atomicWrite(path, body); err != nil {
		return Category{}, err
	}
	return Category{Slug: slug, Title: title}, nil
}

// ListInbox returns bookmarks in inbox.md.
func (s *Store) ListInbox() ([]Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return parseBookmarksFile(s.inboxPath())
}

// ListCategory returns bookmarks in a category file.
func (s *Store) ListCategory(slug string) ([]Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.categoryPath(slug)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("category not found: %s", slug)
	}
	return parseBookmarksFile(path)
}

// AddToInbox appends a bookmark to inbox.md.
func (s *Store) AddToInbox(title, url, source string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
	source = strings.TrimSpace(source)
	if url == "" {
		return Bookmark{}, fmt.Errorf("url required")
	}
	if title == "" {
		title = url
	}
	if source == "" {
		source = "manual"
	}
	b := Bookmark{
		ID:     uuid.NewString(),
		Title:  title,
		URL:    url,
		Source: source,
		Saved:  time.Now().UTC().Format(time.RFC3339),
	}
	existing, err := parseBookmarksFile(s.inboxPath())
	if err != nil {
		return Bookmark{}, err
	}
	existing = append(existing, b)
	if err := writeBookmarksFile(s.inboxPath(), "Inbox", existing); err != nil {
		return Bookmark{}, err
	}
	return b, nil
}

// MoveBookmark moves a bookmark by ID from one place to another.
// fromSlug empty means inbox; toSlug empty means inbox.
func (s *Store) MoveBookmark(id, fromSlug, toSlug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return fmt.Errorf("id required")
	}
	if fromSlug == toSlug {
		return nil
	}

	fromPath, fromTitle, err := s.resolve(fromSlug)
	if err != nil {
		return err
	}
	toPath, toTitle, err := s.resolve(toSlug)
	if err != nil {
		return err
	}

	fromList, err := parseBookmarksFile(fromPath)
	if err != nil {
		return err
	}
	toList, err := parseBookmarksFile(toPath)
	if err != nil {
		return err
	}

	var moved *Bookmark
	var remaining []Bookmark
	for _, b := range fromList {
		if b.ID == id {
			cp := b
			moved = &cp
			continue
		}
		remaining = append(remaining, b)
	}
	if moved == nil {
		return fmt.Errorf("bookmark not found: %s", id)
	}
	toList = append(toList, *moved)

	if err := writeBookmarksFile(fromPath, fromTitle, remaining); err != nil {
		return err
	}
	return writeBookmarksFile(toPath, toTitle, toList)
}

// DeleteBookmark removes a bookmark by ID from inbox or a category.
func (s *Store) DeleteBookmark(id, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, title, err := s.resolve(slug)
	if err != nil {
		return err
	}
	list, err := parseBookmarksFile(path)
	if err != nil {
		return err
	}
	var remaining []Bookmark
	found := false
	for _, b := range list {
		if b.ID == id {
			found = true
			continue
		}
		remaining = append(remaining, b)
	}
	if !found {
		return fmt.Errorf("bookmark not found: %s", id)
	}
	return writeBookmarksFile(path, title, remaining)
}

func (s *Store) resolve(slug string) (path, title string, err error) {
	if slug == "" || slug == "inbox" {
		data, err := os.ReadFile(s.inboxPath())
		if err != nil {
			return "", "", err
		}
		return s.inboxPath(), parseTitle(string(data), "Inbox"), nil
	}
	path = s.categoryPath(slug)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("category not found: %s", slug)
	}
	return path, parseTitle(string(data), slug), nil
}

func parseTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			t := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if t != "" {
				return t
			}
		}
	}
	return fallback
}

// File format (documented in README):
//
//	# Title
//
//	## Bookmark Title
//	id: <uuid>
//	url: https://example.com
//	source: bookmarklet
//	saved: 2026-10-26T19:35:00Z
func parseBookmarksFile(path string) ([]Bookmark, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseBookmarks(string(data)), nil
}

func parseBookmarks(content string) []Bookmark {
	lines := strings.Split(content, "\n")
	var out []Bookmark
	var cur *Bookmark

	flush := func() {
		if cur != nil && cur.URL != "" {
			if cur.ID == "" {
				cur.ID = uuid.NewString()
			}
			if cur.Title == "" {
				cur.Title = cur.URL
			}
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			flush()
			cur = &Bookmark{Title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "id:") {
			cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "url:") {
			cur.URL = strings.TrimSpace(strings.TrimPrefix(line, "url:"))
		} else if strings.HasPrefix(line, "source:") {
			cur.Source = strings.TrimSpace(strings.TrimPrefix(line, "source:"))
		} else if strings.HasPrefix(line, "saved:") {
			cur.Saved = strings.TrimSpace(strings.TrimPrefix(line, "saved:"))
		}
	}
	flush()
	return out
}

func writeBookmarksFile(path, title string, bookmarks []Bookmark) error {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, bm := range bookmarks {
		b.WriteString("## ")
		b.WriteString(bm.Title)
		b.WriteString("\n")
		b.WriteString("id: ")
		b.WriteString(bm.ID)
		b.WriteString("\n")
		b.WriteString("url: ")
		b.WriteString(bm.URL)
		b.WriteString("\n")
		b.WriteString("source: ")
		b.WriteString(bm.Source)
		b.WriteString("\n")
		b.WriteString("saved: ")
		b.WriteString(bm.Saved)
		b.WriteString("\n\n")
	}
	return atomicWrite(path, b.String())
}

func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows cannot rename over an existing file.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
