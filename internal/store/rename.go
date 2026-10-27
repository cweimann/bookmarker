package store

import (
	"fmt"
	"os"
	"strings"
)

// RenameFolder updates the folder title and renames the directory when the slug changes.
func (s *Store) RenameFolder(slug, newTitle string) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug = strings.TrimSpace(slug)
	newTitle = strings.TrimSpace(newTitle)
	if slug == "" {
		return Folder{}, fmt.Errorf("slug required")
	}
	if newTitle == "" {
		return Folder{}, fmt.Errorf("title required")
	}
	if _, err := os.Stat(s.folderDir(slug)); err != nil {
		return Folder{}, fmt.Errorf("folder not found: %s", slug)
	}

	newSlug := Slugify(newTitle)
	if newSlug != slug {
		if _, err := os.Stat(s.folderDir(newSlug)); err == nil {
			return Folder{}, fmt.Errorf("folder %q already exists", newSlug)
		}
		if err := os.Rename(s.folderDir(slug), s.folderDir(newSlug)); err != nil {
			return Folder{}, err
		}
		slug = newSlug
	}

	if err := atomicWrite(s.folderMetaPath(slug), "# "+newTitle+"\n"); err != nil {
		return Folder{}, err
	}
	return Folder{Slug: slug, Title: newTitle}, nil
}

// RenameCategory updates a category title and renames its .md file when the slug changes.
// Inbox cannot be renamed.
func (s *Store) RenameCategory(folder, slug, newTitle string) (Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder = strings.TrimSpace(folder)
	slug = strings.TrimSpace(slug)
	newTitle = strings.TrimSpace(newTitle)
	if folder == "" || slug == "" {
		return Category{}, fmt.Errorf("folder and slug required")
	}
	if slug == "inbox" {
		return Category{}, fmt.Errorf("cannot rename inbox")
	}
	if newTitle == "" {
		return Category{}, fmt.Errorf("title required")
	}
	if _, err := os.Stat(s.folderDir(folder)); err != nil {
		return Category{}, fmt.Errorf("folder not found: %s", folder)
	}

	src := s.listPath(folder, slug)
	list, err := parseBookmarksFile(src)
	if err != nil {
		return Category{}, fmt.Errorf("category not found: %s/%s", folder, slug)
	}

	newSlug := Slugify(newTitle)
	if newSlug == "inbox" || newSlug == "_folder" {
		return Category{}, fmt.Errorf("reserved name")
	}

	dstSlug := slug
	if newSlug != slug {
		dst := s.listPath(folder, newSlug)
		if _, err := os.Stat(dst); err == nil {
			return Category{}, fmt.Errorf("category %q already exists", newSlug)
		}
		if err := writeBookmarksFile(dst, newTitle, list); err != nil {
			return Category{}, err
		}
		_ = os.Remove(src)
		dstSlug = newSlug
	} else {
		if err := writeBookmarksFile(src, newTitle, list); err != nil {
			return Category{}, err
		}
	}

	return Category{Folder: folder, Slug: dstSlug, Title: newTitle, Inbox: false}, nil
}

// UpdateBookmark changes title and/or URL for a bookmark by ID.
func (s *Store) UpdateBookmark(folder, category, id, title, url string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
	if id == "" {
		return Bookmark{}, fmt.Errorf("id required")
	}
	if title == "" && url == "" {
		return Bookmark{}, fmt.Errorf("title or url required")
	}

	path, fileTitle, err := s.resolve(folder, category)
	if err != nil {
		return Bookmark{}, err
	}
	list, err := parseBookmarksFile(path)
	if err != nil {
		return Bookmark{}, err
	}

	var updated *Bookmark
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if title != "" {
			list[i].Title = title
		}
		if url != "" {
			list[i].URL = url
		}
		cp := list[i]
		updated = &cp
		break
	}
	if updated == nil {
		return Bookmark{}, fmt.Errorf("bookmark not found: %s", id)
	}
	if err := writeBookmarksFile(path, fileTitle, list); err != nil {
		return Bookmark{}, err
	}
	return *updated, nil
}
