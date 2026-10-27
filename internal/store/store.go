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

// MaxColumns is the UI column cap (root + nested). Depth limit = MaxColumns-1.
// Change this single value to allow deeper trees later.
const MaxColumns = 3

// Bookmark is one saved link.
type Bookmark struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source"`
	Saved  string `json:"saved"`
}

// FolderRef is a child folder under some path.
type FolderRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Path  string `json:"path"` // full path from root, e.g. "work/dev"
}

// CategoryRef is a category (or inbox) in a folder.
type CategoryRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Inbox bool   `json:"inbox"`
}

// Listing is the contents of one folder path.
type Listing struct {
	Path       string        `json:"path"`
	Title      string        `json:"title"`
	Depth      int           `json:"depth"`
	Folders    []FolderRef   `json:"folders"`
	Categories []CategoryRef `json:"categories"`
}

// Store reads/writes a tree of Markdown under DataDir.
//
//	data/
//	  _folder.md          # root title
//	  inbox.md            # root inbox (/add target)
//	  some-category.md
//	  work/
//	    _folder.md
//	    inbox.md
//	    webapp-dev.md
//	    nested/
//	      ...
type Store struct {
	DataDir string
	mu      sync.Mutex
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// New creates a Store, migrates legacy layouts, ensures root inbox.
func New(dataDir string) (*Store, error) {
	s := &Store{DataDir: dataDir}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := s.migrateLegacyIfNeeded(); err != nil {
		return nil, err
	}
	if err := s.ensureFolderPath(""); err != nil {
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
		s = "item"
	}
	return s
}

func (s *Store) abs(rel string) string {
	rel = cleanPath(rel)
	if rel == "" {
		return s.DataDir
	}
	return filepath.Join(s.DataDir, filepath.FromSlash(rel))
}

func cleanPath(p string) string {
	p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	if p == "." || p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		if part == "_folder" || part == "inbox" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

func pathDepth(p string) int {
	p = cleanPath(p)
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

func joinPath(parent, slug string) string {
	parent = cleanPath(parent)
	slug = strings.Trim(slug, "/")
	if parent == "" {
		return slug
	}
	return parent + "/" + slug
}

func (s *Store) metaPath(folderPath string) string {
	return filepath.Join(s.abs(folderPath), "_folder.md")
}

func (s *Store) listPath(folderPath, category string) string {
	category = normalizeCat(category)
	if category == "inbox" {
		return filepath.Join(s.abs(folderPath), "inbox.md")
	}
	return filepath.Join(s.abs(folderPath), category+".md")
}

func normalizeCat(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return "inbox"
	}
	return c
}

func (s *Store) ensureFolderPath(folderPath string) error {
	folderPath = cleanPath(folderPath)
	dir := s.abs(folderPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	meta := s.metaPath(folderPath)
	if _, err := os.Stat(meta); os.IsNotExist(err) {
		title := "Root"
		if folderPath != "" {
			title = humanize(filepath.Base(filepath.FromSlash(folderPath)))
		}
		if err := atomicWrite(meta, "# "+title+"\n"); err != nil {
			return err
		}
	}
	inbox := s.listPath(folderPath, "inbox")
	if _, err := os.Stat(inbox); os.IsNotExist(err) {
		if err := atomicWrite(inbox, "# Inbox\n\n"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) folderTitle(folderPath string) string {
	data, err := os.ReadFile(s.metaPath(folderPath))
	if err != nil {
		if folderPath == "" {
			return "Root"
		}
		return humanize(filepath.Base(filepath.FromSlash(folderPath)))
	}
	fallback := "Root"
	if folderPath != "" {
		fallback = humanize(filepath.Base(filepath.FromSlash(folderPath)))
	}
	return parseTitle(string(data), fallback)
}

// List returns folders + categories at path (Inbox first among categories).
func (s *Store) List(folderPath string) (Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listUnlocked(folderPath)
}

func (s *Store) listUnlocked(folderPath string) (Listing, error) {
	folderPath = cleanPath(folderPath)
	if err := s.ensureFolderPath(folderPath); err != nil {
		return Listing{}, err
	}
	dir := s.abs(folderPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, err
	}

	out := Listing{
		Path:  folderPath,
		Title: s.folderTitle(folderPath),
		Depth: pathDepth(folderPath),
		Categories: []CategoryRef{{
			Slug:  "inbox",
			Title: "Inbox",
			Inbox: true,
		}},
	}

	var folders []FolderRef
	var cats []CategoryRef
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name == "" || strings.HasPrefix(name, ".") {
				continue
			}
			childPath := joinPath(folderPath, name)
			folders = append(folders, FolderRef{
				Slug:  name,
				Title: s.folderTitle(childPath),
				Path:  childPath,
			})
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "inbox.md" || name == "_folder.md" {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return Listing{}, err
		}
		cats = append(cats, CategoryRef{
			Slug:  slug,
			Title: parseTitle(string(data), humanize(slug)),
			Inbox: false,
		})
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Title) < strings.ToLower(folders[j].Title)
	})
	sort.Slice(cats, func(i, j int) bool {
		return strings.ToLower(cats[i].Title) < strings.ToLower(cats[j].Title)
	})
	out.Folders = folders
	out.Categories = append(out.Categories, cats...)
	return out, nil
}

// CreateFolder creates a child folder under parentPath.
func (s *Store) CreateFolder(parentPath, title string) (FolderRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parentPath = cleanPath(parentPath)
	title = strings.TrimSpace(title)
	if title == "" {
		return FolderRef{}, fmt.Errorf("title required")
	}
	if pathDepth(parentPath) >= MaxColumns-1 {
		return FolderRef{}, fmt.Errorf("maximum folder depth is %d", MaxColumns-1)
	}
	if err := s.ensureFolderPath(parentPath); err != nil {
		return FolderRef{}, err
	}
	slug := Slugify(title)
	child := joinPath(parentPath, slug)
	if _, err := os.Stat(s.abs(child)); err == nil {
		return FolderRef{}, fmt.Errorf("folder %q already exists", slug)
	}
	if err := s.ensureFolderPath(child); err != nil {
		return FolderRef{}, err
	}
	if err := atomicWrite(s.metaPath(child), "# "+title+"\n"); err != nil {
		return FolderRef{}, err
	}
	return FolderRef{Slug: slug, Title: title, Path: child}, nil
}

// CreateCategory creates a category .md in folderPath.
func (s *Store) CreateCategory(folderPath, title string) (CategoryRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath = cleanPath(folderPath)
	title = strings.TrimSpace(title)
	if title == "" {
		return CategoryRef{}, fmt.Errorf("title required")
	}
	if err := s.ensureFolderPath(folderPath); err != nil {
		return CategoryRef{}, err
	}
	slug := Slugify(title)
	if slug == "inbox" || slug == "_folder" {
		return CategoryRef{}, fmt.Errorf("reserved name")
	}
	path := s.listPath(folderPath, slug)
	if _, err := os.Stat(path); err == nil {
		return CategoryRef{}, fmt.Errorf("category %q already exists", slug)
	}
	if err := atomicWrite(path, fmt.Sprintf("# %s\n\n", title)); err != nil {
		return CategoryRef{}, err
	}
	return CategoryRef{Slug: slug, Title: title, Inbox: false}, nil
}

// MoveCategory moves a category file between folder paths.
func (s *Store) MoveCategory(slug, fromPath, toPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug = strings.TrimSpace(slug)
	fromPath = cleanPath(fromPath)
	toPath = cleanPath(toPath)
	if slug == "" || slug == "inbox" {
		return fmt.Errorf("cannot move inbox")
	}
	if fromPath == toPath {
		return nil
	}
	if err := s.ensureFolderPath(toPath); err != nil {
		return err
	}
	src := s.listPath(fromPath, slug)
	dst := s.listPath(toPath, slug)
	_, srcErr := os.Stat(src)
	_, dstErr := os.Stat(dst)
	if srcErr != nil && dstErr == nil {
		return nil // already moved
	}
	if srcErr != nil {
		return fmt.Errorf("category not found: %s/%s", fromPath, slug)
	}
	if dstErr == nil {
		return fmt.Errorf("category %q already exists in %s", slug, toPath)
	}
	return os.Rename(src, dst)
}

// ListBookmarks returns bookmarks for path + category.
func (s *Store) ListBookmarks(folderPath, category string) ([]Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, err := s.resolve(folderPath, category)
	if err != nil {
		return nil, err
	}
	return parseBookmarksFile(path)
}

// AddToRootInbox is the bookmarklet target.
func (s *Store) AddToRootInbox(title, url, source string) (Bookmark, error) {
	return s.AddToFolderInbox("", title, url, source)
}

// AddToFolderInbox appends a bookmark to a folder's inbox.
func (s *Store) AddToFolderInbox(folderPath, title, url, source string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath = cleanPath(folderPath)
	if err := s.ensureFolderPath(folderPath); err != nil {
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
	path := s.listPath(folderPath, "inbox")
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

// MoveBookmark moves a bookmark between path/category locations.
func (s *Store) MoveBookmark(id, fromPath, fromCat, toPath, toCat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return fmt.Errorf("id required")
	}
	fromPath = cleanPath(fromPath)
	toPath = cleanPath(toPath)
	fromCat = normalizeCat(fromCat)
	toCat = normalizeCat(toCat)
	if fromPath == toPath && fromCat == toCat {
		return nil
	}
	fromFile, fromTitle, err := s.resolve(fromPath, fromCat)
	if err != nil {
		return err
	}
	toFile, toTitle, err := s.resolve(toPath, toCat)
	if err != nil {
		return err
	}
	fromList, err := parseBookmarksFile(fromFile)
	if err != nil {
		return err
	}
	toList, err := parseBookmarksFile(toFile)
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
	if err := writeBookmarksFile(fromFile, fromTitle, remaining); err != nil {
		return err
	}
	return writeBookmarksFile(toFile, toTitle, toList)
}

// DeleteBookmark removes a bookmark by ID.
func (s *Store) DeleteBookmark(id, folderPath, category string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, title, err := s.resolve(folderPath, category)
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

func (s *Store) resolve(folderPath, category string) (path, title string, err error) {
	folderPath = cleanPath(folderPath)
	category = normalizeCat(category)
	if err := s.ensureFolderPath(folderPath); err != nil {
		return "", "", err
	}
	path = s.listPath(folderPath, category)
	data, err := os.ReadFile(path)
	if err != nil {
		loc := category
		if folderPath != "" {
			loc = folderPath + "/" + category
		}
		return "", "", fmt.Errorf("category not found: %s", loc)
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
