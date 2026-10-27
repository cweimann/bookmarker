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

// Folder groups categories (and a per-folder inbox).
type Folder struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Category sits inside a folder. Slug "inbox" is the folder inbox.
type Category struct {
	Folder string `json:"folder"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Inbox  bool   `json:"inbox"`
}

// Store reads and writes Markdown bookmark files under DataDir.
//
//	data/folders/<folder>/_folder.md   # folder title
//	data/folders/<folder>/inbox.md
//	data/folders/<folder>/<category>.md
type Store struct {
	DataDir string
	mu      sync.Mutex
}

// DefaultFolder is where /add (bookmarklet) lands.
const DefaultFolder = "unfiled"

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// New creates a Store, migrates legacy layout if needed, ensures defaults.
func New(dataDir string) (*Store, error) {
	s := &Store{DataDir: dataDir}
	if err := os.MkdirAll(s.foldersRoot(), 0o755); err != nil {
		return nil, err
	}
	if err := s.migrateLegacyIfNeeded(); err != nil {
		return nil, err
	}
	if err := s.ensureFolder(DefaultFolder, "Unfiled"); err != nil {
		return nil, err
	}
	return s, nil
}

// Slugify turns a title into a filesystem-safe slug.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "category"
	}
	return s
}

func (s *Store) foldersRoot() string {
	return filepath.Join(s.DataDir, "folders")
}

func (s *Store) folderDir(folder string) string {
	return filepath.Join(s.foldersRoot(), folder)
}

func (s *Store) folderMetaPath(folder string) string {
	return filepath.Join(s.folderDir(folder), "_folder.md")
}

func (s *Store) listPath(folder, category string) string {
	if category == "" || category == "inbox" {
		return filepath.Join(s.folderDir(folder), "inbox.md")
	}
	return filepath.Join(s.folderDir(folder), category+".md")
}

func (s *Store) ensureFolder(slug, title string) error {
	dir := s.folderDir(slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	meta := s.folderMetaPath(slug)
	if _, err := os.Stat(meta); os.IsNotExist(err) {
		if err := atomicWrite(meta, "# "+title+"\n"); err != nil {
			return err
		}
	}
	inbox := s.listPath(slug, "inbox")
	if _, err := os.Stat(inbox); os.IsNotExist(err) {
		if err := atomicWrite(inbox, "# Inbox\n\n"); err != nil {
			return err
		}
	}
	return nil
}

// ListFolders returns all folders sorted by title.
func (s *Store) ListFolders() ([]Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listFoldersUnlocked()
}

func (s *Store) listFoldersUnlocked() ([]Folder, error) {
	entries, err := os.ReadDir(s.foldersRoot())
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		title := s.folderTitleUnlocked(slug)
		out = append(out, Folder{Slug: slug, Title: title})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}

func (s *Store) folderTitleUnlocked(slug string) string {
	data, err := os.ReadFile(s.folderMetaPath(slug))
	if err != nil {
		return humanize(slug)
	}
	return parseTitle(string(data), humanize(slug))
}

// CreateFolder creates data/folders/<slug>/ with inbox.
func (s *Store) CreateFolder(title string) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	if title == "" {
		return Folder{}, fmt.Errorf("title required")
	}
	slug := Slugify(title)
	if _, err := os.Stat(s.folderDir(slug)); err == nil {
		return Folder{}, fmt.Errorf("folder %q already exists", slug)
	}
	if err := s.ensureFolder(slug, title); err != nil {
		return Folder{}, err
	}
	return Folder{Slug: slug, Title: title}, nil
}

// ListCategories returns inbox + categories for a folder.
func (s *Store) ListCategories(folder string) ([]Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil, fmt.Errorf("folder required")
	}
	if _, err := os.Stat(s.folderDir(folder)); err != nil {
		return nil, fmt.Errorf("folder not found: %s", folder)
	}

	out := []Category{{
		Folder: folder,
		Slug:   "inbox",
		Title:  "Inbox",
		Inbox:  true,
	}}

	entries, err := os.ReadDir(s.folderDir(folder))
	if err != nil {
		return nil, err
	}
	var cats []Category
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "inbox.md" || name == "_folder.md" {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		data, err := os.ReadFile(filepath.Join(s.folderDir(folder), name))
		if err != nil {
			return nil, err
		}
		cats = append(cats, Category{
			Folder: folder,
			Slug:   slug,
			Title:  parseTitle(string(data), humanize(slug)),
			Inbox:  false,
		})
	}
	sort.Slice(cats, func(i, j int) bool {
		return strings.ToLower(cats[i].Title) < strings.ToLower(cats[j].Title)
	})
	return append(out, cats...), nil
}

// CreateCategory creates a category markdown file in a folder.
func (s *Store) CreateCategory(folder, title string) (Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder = strings.TrimSpace(folder)
	title = strings.TrimSpace(title)
	if folder == "" {
		return Category{}, fmt.Errorf("folder required")
	}
	if title == "" {
		return Category{}, fmt.Errorf("title required")
	}
	if _, err := os.Stat(s.folderDir(folder)); err != nil {
		return Category{}, fmt.Errorf("folder not found: %s", folder)
	}
	slug := Slugify(title)
	if slug == "inbox" || slug == "_folder" {
		return Category{}, fmt.Errorf("reserved name")
	}
	path := s.listPath(folder, slug)
	if _, err := os.Stat(path); err == nil {
		return Category{}, fmt.Errorf("category %q already exists", slug)
	}
	if err := atomicWrite(path, fmt.Sprintf("# %s\n\n", title)); err != nil {
		return Category{}, err
	}
	return Category{Folder: folder, Slug: slug, Title: title, Inbox: false}, nil
}

// MoveCategory relocates a category file between folders (not inbox).
func (s *Store) MoveCategory(slug, fromFolder, toFolder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug = strings.TrimSpace(slug)
	fromFolder = strings.TrimSpace(fromFolder)
	toFolder = strings.TrimSpace(toFolder)
	if slug == "" || slug == "inbox" {
		return fmt.Errorf("cannot move inbox")
	}
	if fromFolder == "" || toFolder == "" {
		return fmt.Errorf("from and to folder required")
	}
	if fromFolder == toFolder {
		return nil
	}
	if _, err := os.Stat(s.folderDir(toFolder)); err != nil {
		return fmt.Errorf("folder not found: %s", toFolder)
	}
	src := s.listPath(fromFolder, slug)
	dst := s.listPath(toFolder, slug)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("category not found: %s/%s", fromFolder, slug)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("category %q already exists in %s", slug, toFolder)
	}
	return os.Rename(src, dst)
}

// ListBookmarks returns bookmarks for folder + category (category "inbox" or "").
func (s *Store) ListBookmarks(folder, category string) ([]Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, _, err := s.resolve(folder, category)
	if err != nil {
		return nil, err
	}
	return parseBookmarksFile(path)
}

// AddToInbox appends to the default folder inbox (bookmarklet target).
func (s *Store) AddToInbox(title, url, source string) (Bookmark, error) {
	return s.AddToFolderInbox(DefaultFolder, title, url, source)
}

// AddToFolderInbox appends a bookmark to a folder's inbox.
func (s *Store) AddToFolderInbox(folder, title, url, source string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = DefaultFolder
	}
	if err := s.ensureFolder(folder, humanize(folder)); err != nil {
		return Bookmark{}, err
	}

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
	path := s.listPath(folder, "inbox")
	existing, err := parseBookmarksFile(path)
	if err != nil {
		return Bookmark{}, err
	}
	existing = append(existing, b)
	if err := writeBookmarksFile(path, "Inbox", existing); err != nil {
		return Bookmark{}, err
	}
	return b, nil
}

// MoveBookmark moves a bookmark between folder/category locations.
func (s *Store) MoveBookmark(id, fromFolder, fromCat, toFolder, toCat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return fmt.Errorf("id required")
	}
	fromFolder = strings.TrimSpace(fromFolder)
	toFolder = strings.TrimSpace(toFolder)
	fromCat = normalizeCat(fromCat)
	toCat = normalizeCat(toCat)
	if fromFolder == toFolder && fromCat == toCat {
		return nil
	}

	fromPath, fromTitle, err := s.resolve(fromFolder, fromCat)
	if err != nil {
		return err
	}
	toPath, toTitle, err := s.resolve(toFolder, toCat)
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

// DeleteBookmark removes a bookmark by ID.
func (s *Store) DeleteBookmark(id, folder, category string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, title, err := s.resolve(folder, category)
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

func normalizeCat(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return "inbox"
	}
	return c
}

func (s *Store) resolve(folder, category string) (path, title string, err error) {
	folder = strings.TrimSpace(folder)
	category = normalizeCat(category)
	if folder == "" {
		return "", "", fmt.Errorf("folder required")
	}
	if _, err := os.Stat(s.folderDir(folder)); err != nil {
		return "", "", fmt.Errorf("folder not found: %s", folder)
	}
	path = s.listPath(folder, category)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("category not found: %s/%s", folder, category)
	}
	fallback := "Inbox"
	if category != "inbox" {
		fallback = humanize(category)
	}
	return path, parseTitle(string(data), fallback), nil
}

func humanize(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
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
		switch {
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "url:"):
			cur.URL = strings.TrimSpace(strings.TrimPrefix(line, "url:"))
		case strings.HasPrefix(line, "source:"):
			cur.Source = strings.TrimSpace(strings.TrimPrefix(line, "source:"))
		case strings.HasPrefix(line, "saved:"):
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
