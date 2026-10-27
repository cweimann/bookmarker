package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RenameFolder updates title and optionally renames the directory (last path segment).
func (s *Store) RenameFolder(folderPath, newTitle string) (FolderRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath = cleanPath(folderPath)
	newTitle = strings.TrimSpace(newTitle)
	if folderPath == "" {
		// Root: title only, no directory rename.
		if newTitle == "" {
			return FolderRef{}, fmt.Errorf("title required")
		}
		if err := atomicWrite(s.metaPath(""), "# "+newTitle+"\n"); err != nil {
			return FolderRef{}, err
		}
		return FolderRef{Slug: "", Title: newTitle, Path: ""}, nil
	}
	if newTitle == "" {
		return FolderRef{}, fmt.Errorf("title required")
	}
	if _, err := os.Stat(s.abs(folderPath)); err != nil {
		return FolderRef{}, fmt.Errorf("folder not found: %s", folderPath)
	}

	newSlug := Slugify(newTitle)
	parent := ""
	if i := strings.LastIndex(folderPath, "/"); i >= 0 {
		parent = folderPath[:i]
	}
	oldSlug := folderPath
	if i := strings.LastIndex(folderPath, "/"); i >= 0 {
		oldSlug = folderPath[i+1:]
	}

	finalPath := folderPath
	if newSlug != oldSlug {
		dest := joinPath(parent, newSlug)
		if _, err := os.Stat(s.abs(dest)); err == nil {
			return FolderRef{}, fmt.Errorf("folder %q already exists", newSlug)
		}
		if err := os.Rename(s.abs(folderPath), s.abs(dest)); err != nil {
			return FolderRef{}, err
		}
		finalPath = dest
	}
	if err := atomicWrite(s.metaPath(finalPath), "# "+newTitle+"\n"); err != nil {
		return FolderRef{}, err
	}
	return FolderRef{
		Slug:  filepath.Base(filepath.FromSlash(finalPath)),
		Title: newTitle,
		Path:  finalPath,
	}, nil
}

// RenameCategory updates title and renames the .md when slug changes.
func (s *Store) RenameCategory(folderPath, slug, newTitle string) (CategoryRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath = cleanPath(folderPath)
	slug = strings.TrimSpace(slug)
	newTitle = strings.TrimSpace(newTitle)
	if slug == "" || slug == "inbox" {
		return CategoryRef{}, fmt.Errorf("cannot rename Inbox")
	}
	if newTitle == "" {
		return CategoryRef{}, fmt.Errorf("title required")
	}
	src := s.listPath(folderPath, slug)
	list, err := parseBookmarksFile(src)
	if err != nil {
		return CategoryRef{}, fmt.Errorf("category not found: %s/%s", folderPath, slug)
	}
	newSlug := Slugify(newTitle)
	if newSlug == "inbox" || newSlug == "_folder" {
		return CategoryRef{}, fmt.Errorf("reserved name")
	}
	if newSlug != slug {
		dst := s.listPath(folderPath, newSlug)
		if _, err := os.Stat(dst); err == nil {
			return CategoryRef{}, fmt.Errorf("category %q already exists", newSlug)
		}
		if err := writeBookmarksFile(dst, newTitle, list); err != nil {
			return CategoryRef{}, err
		}
		_ = os.Remove(src)
		slug = newSlug
	} else if err := writeBookmarksFile(src, newTitle, list); err != nil {
		return CategoryRef{}, err
	}
	return CategoryRef{Slug: slug, Title: newTitle, Inbox: false}, nil
}

// UpdateBookmark changes title and/or URL for a bookmark by ID.
func (s *Store) UpdateBookmark(folderPath, category, id, title, url string) (Bookmark, error) {
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
	path, fileTitle, err := s.resolve(folderPath, category)
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
