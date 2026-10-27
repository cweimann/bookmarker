package store

import (
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
//
// Folder and category names on disk are slugs (see validSlug). Reads never
// create files; only explicit create/write operations do.
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

// abs maps an already-validated folder path to its directory.
func (s *Store) abs(rel string) string {
	if rel == "" {
		return s.DataDir
	}
	return filepath.Join(s.DataDir, filepath.FromSlash(rel))
}

// folder validates a client folder path and requires the directory to exist.
func (s *Store) folder(p string) (clean, dir string, err error) {
	clean, err = parseFolderPath(p)
	if err != nil {
		return "", "", err
	}
	dir = s.abs(clean)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", "", errf(ErrNotFound, "folder not found: %s", displayPath(clean))
	}
	return clean, dir, nil
}

func pathDepth(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

func joinPath(parent, slug string) string {
	if parent == "" {
		return slug
	}
	return parent + "/" + slug
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func (s *Store) metaPath(folderPath string) string {
	return filepath.Join(s.abs(folderPath), "_folder.md")
}

func (s *Store) listPath(folderPath, category string) string {
	if category == "" {
		category = "inbox"
	}
	return filepath.Join(s.abs(folderPath), category+".md")
}

func (s *Store) ensureFolderPath(folderPath string) error {
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
	fallback := "Root"
	if folderPath != "" {
		fallback = humanize(filepath.Base(filepath.FromSlash(folderPath)))
	}
	data, err := os.ReadFile(s.metaPath(folderPath))
	if err != nil {
		return fallback
	}
	return parseTitle(string(data), fallback)
}

// List returns folders + categories at path (Inbox first among categories).
func (s *Store) List(folderPath string) (Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath, dir, err := s.folder(folderPath)
	if err != nil {
		return Listing{}, err
	}
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

	parentPath, _, err := s.folder(parentPath)
	if err != nil {
		return FolderRef{}, err
	}
	title = cleanLine(title)
	if title == "" {
		return FolderRef{}, errf(ErrInvalid, "title required")
	}
	if pathDepth(parentPath) >= MaxColumns-1 {
		return FolderRef{}, errf(ErrInvalid, "maximum folder depth is %d", MaxColumns-1)
	}
	slug := Slugify(title)
	if slug == "inbox" {
		return FolderRef{}, errf(ErrInvalid, "reserved name")
	}
	child := joinPath(parentPath, slug)
	if _, err := os.Stat(s.abs(child)); err == nil {
		return FolderRef{}, errf(ErrExists, "folder %q already exists", slug)
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

	folderPath, _, err := s.folder(folderPath)
	if err != nil {
		return CategoryRef{}, err
	}
	title = cleanLine(title)
	if title == "" {
		return CategoryRef{}, errf(ErrInvalid, "title required")
	}
	slug := Slugify(title)
	if slug == "inbox" {
		return CategoryRef{}, errf(ErrInvalid, "reserved name")
	}
	path := s.listPath(folderPath, slug)
	if _, err := os.Stat(path); err == nil {
		return CategoryRef{}, errf(ErrExists, "category %q already exists", slug)
	}
	if err := atomicWrite(path, "# "+title+"\n\n"); err != nil {
		return CategoryRef{}, err
	}
	return CategoryRef{Slug: slug, Title: title, Inbox: false}, nil
}

// MoveCategory moves a category file between existing folders.
func (s *Store) MoveCategory(slug, fromPath, toPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug, err := parseCategory(slug)
	if err != nil {
		return err
	}
	if slug == "inbox" {
		return errf(ErrInvalid, "cannot move inbox")
	}
	fromPath, _, err = s.folder(fromPath)
	if err != nil {
		return err
	}
	toPath, _, err = s.folder(toPath)
	if err != nil {
		return err
	}
	if fromPath == toPath {
		return nil
	}
	src := s.listPath(fromPath, slug)
	dst := s.listPath(toPath, slug)
	_, srcErr := os.Stat(src)
	_, dstErr := os.Stat(dst)
	if srcErr != nil && dstErr == nil {
		return nil // already moved (browser DnD can fire twice)
	}
	if srcErr != nil {
		return errf(ErrNotFound, "category not found: %s/%s", displayPath(fromPath), slug)
	}
	if dstErr == nil {
		return errf(ErrExists, "category %q already exists in %s", slug, displayPath(toPath))
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
	return s.AddBookmark("", "inbox", title, url, source)
}

// AddToFolderInbox appends a bookmark to a folder's inbox.
func (s *Store) AddToFolderInbox(folderPath, title, url, source string) (Bookmark, error) {
	return s.AddBookmark(folderPath, "inbox", title, url, source)
}

// AddBookmark appends a bookmark to path + category (defaults to root inbox).
func (s *Store) AddBookmark(folderPath, category, title, url, source string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	url, err := cleanURL(url)
	if err != nil {
		return Bookmark{}, err
	}
	title = cleanLine(title)
	source = cleanLine(source)
	if title == "" {
		title = url
	}
	if source == "" {
		source = "manual"
	}
	path, fileTitle, err := s.resolve(folderPath, category)
	if err != nil {
		return Bookmark{}, err
	}
	b := Bookmark{
		ID:     uuid.NewString(),
		Title:  title,
		URL:    url,
		Source: source,
		Saved:  time.Now().UTC().Format(time.RFC3339),
	}
	existing, err := parseBookmarksFile(path)
	if err != nil {
		return Bookmark{}, err
	}
	existing = append(existing, b)
	if err := writeBookmarksFile(path, fileTitle, existing); err != nil {
		return Bookmark{}, err
	}
	return b, nil
}

// MoveBookmark moves a bookmark between path/category locations.
// The destination is written first, so a failure leaves a duplicate rather
// than losing the bookmark; a retry then just removes it from the source.
func (s *Store) MoveBookmark(id, fromPath, fromCat, toPath, toCat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return errf(ErrInvalid, "id required")
	}
	fromFile, fromTitle, err := s.resolve(fromPath, fromCat)
	if err != nil {
		return err
	}
	toFile, toTitle, err := s.resolve(toPath, toCat)
	if err != nil {
		return err
	}
	if fromFile == toFile {
		return nil
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
		return errf(ErrNotFound, "bookmark not found: %s", id)
	}
	if indexOf(toList, id) < 0 {
		toList = append(toList, *moved)
		if err := writeBookmarksFile(toFile, toTitle, toList); err != nil {
			return err
		}
	}
	return writeBookmarksFile(fromFile, fromTitle, remaining)
}

// DeleteBookmark removes a bookmark by ID.
func (s *Store) DeleteBookmark(id, folderPath, category string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	path, title, err := s.resolve(folderPath, category)
	if err != nil {
		return err
	}
	list, err := parseBookmarksFile(path)
	if err != nil {
		return err
	}
	i := indexOf(list, id)
	if id == "" || i < 0 {
		return errf(ErrNotFound, "bookmark not found: %s", id)
	}
	list = append(list[:i], list[i+1:]...)
	return writeBookmarksFile(path, title, list)
}

func indexOf(list []Bookmark, id string) int {
	for i, b := range list {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// resolve validates a client folder + category and returns the list file and
// its title. A missing inbox.md resolves (it is created on first write); any
// other missing category is ErrNotFound.
func (s *Store) resolve(folderPath, category string) (path, title string, err error) {
	folderPath, _, err = s.folder(folderPath)
	if err != nil {
		return "", "", err
	}
	category, err = parseCategory(category)
	if err != nil {
		return "", "", err
	}
	path = s.listPath(folderPath, category)
	fallback := "Inbox"
	if category != "inbox" {
		fallback = humanize(category)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && category == "inbox" {
			return path, fallback, nil
		}
		if os.IsNotExist(err) {
			return "", "", errf(ErrNotFound, "category not found: %s", joinPath(folderPath, category))
		}
		return "", "", err
	}
	return path, parseTitle(string(data), fallback), nil
}

func humanize(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		parts[i] = strings.ToUpper(string(r[:1])) + string(r[1:])
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
	b.WriteString(cleanLine(title))
	b.WriteString("\n\n")
	for _, bm := range bookmarks {
		b.WriteString("## ")
		b.WriteString(cleanLine(bm.Title))
		b.WriteString("\n")
		b.WriteString("id: ")
		b.WriteString(cleanLine(bm.ID))
		b.WriteString("\n")
		b.WriteString("url: ")
		b.WriteString(cleanLine(bm.URL))
		b.WriteString("\n")
		b.WriteString("source: ")
		b.WriteString(cleanLine(bm.Source))
		b.WriteString("\n")
		b.WriteString("saved: ")
		b.WriteString(cleanLine(bm.Saved))
		b.WriteString("\n\n")
	}
	return atomicWrite(path, b.String())
}

// atomicWrite replaces path via a synced temp file and a single rename.
// os.Rename replaces an existing file on Windows too, so the old file stays
// intact until the new one is in place.
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
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
