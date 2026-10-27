package store

import (
	"os"
	"strings"
)

// RenameFolder updates title and renames the directory when the slug changes.
func (s *Store) RenameFolder(folderPath, newTitle string) (FolderRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath, dir, err := s.folder(folderPath)
	if err != nil {
		return FolderRef{}, err
	}
	newTitle = cleanLine(newTitle)
	if newTitle == "" {
		return FolderRef{}, errf(ErrInvalid, "title required")
	}
	if folderPath == "" {
		// Root: title only, no directory rename.
		if err := atomicWrite(s.metaPath(""), "# "+newTitle+"\n"); err != nil {
			return FolderRef{}, err
		}
		return FolderRef{Slug: "", Title: newTitle, Path: ""}, nil
	}

	newSlug := Slugify(newTitle)
	if newSlug == "inbox" {
		return FolderRef{}, errf(ErrInvalid, "reserved name")
	}
	parent := parentOf(folderPath)
	oldSlug := strings.TrimPrefix(folderPath[len(parent):], "/")

	finalPath := folderPath
	if newSlug != oldSlug {
		dest := joinPath(parent, newSlug)
		if _, err := os.Stat(s.abs(dest)); err == nil {
			return FolderRef{}, errf(ErrExists, "folder %q already exists", newSlug)
		}
		if err := os.Rename(dir, s.abs(dest)); err != nil {
			return FolderRef{}, err
		}
		finalPath = dest
	}
	if err := atomicWrite(s.metaPath(finalPath), "# "+newTitle+"\n"); err != nil {
		return FolderRef{}, err
	}
	return FolderRef{Slug: newSlug, Title: newTitle, Path: finalPath}, nil
}

// RenameCategory updates title and renames the .md when the slug changes.
func (s *Store) RenameCategory(folderPath, slug, newTitle string) (CategoryRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath, _, err := s.folder(folderPath)
	if err != nil {
		return CategoryRef{}, err
	}
	slug, err = parseCategory(slug)
	if err != nil {
		return CategoryRef{}, err
	}
	if slug == "inbox" {
		return CategoryRef{}, errf(ErrInvalid, "cannot rename Inbox")
	}
	newTitle = cleanLine(newTitle)
	if newTitle == "" {
		return CategoryRef{}, errf(ErrInvalid, "title required")
	}
	src := s.listPath(folderPath, slug)
	if _, err := os.Stat(src); err != nil {
		return CategoryRef{}, errf(ErrNotFound, "category not found: %s", joinPath(folderPath, slug))
	}
	list, err := parseBookmarksFile(src)
	if err != nil {
		return CategoryRef{}, err
	}
	newSlug := Slugify(newTitle)
	if newSlug == "inbox" {
		return CategoryRef{}, errf(ErrInvalid, "reserved name")
	}
	dst := src
	if newSlug != slug {
		dst = s.listPath(folderPath, newSlug)
		if _, err := os.Stat(dst); err == nil {
			return CategoryRef{}, errf(ErrExists, "category %q already exists", newSlug)
		}
		if err := os.Rename(src, dst); err != nil {
			return CategoryRef{}, err
		}
	}
	if err := writeBookmarksFile(dst, newTitle, list); err != nil {
		return CategoryRef{}, err
	}
	return CategoryRef{Slug: newSlug, Title: newTitle, Inbox: false}, nil
}

// UpdateBookmark changes title and/or URL for a bookmark by ID.
func (s *Store) UpdateBookmark(folderPath, category, id, title, url string) (Bookmark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	title = cleanLine(title)
	if id == "" {
		return Bookmark{}, errf(ErrInvalid, "id required")
	}
	if strings.TrimSpace(url) != "" {
		u, err := cleanURL(url)
		if err != nil {
			return Bookmark{}, err
		}
		url = u
	} else {
		url = ""
	}
	if title == "" && url == "" {
		return Bookmark{}, errf(ErrInvalid, "title or url required")
	}
	path, fileTitle, err := s.resolve(folderPath, category)
	if err != nil {
		return Bookmark{}, err
	}
	list, err := parseBookmarksFile(path)
	if err != nil {
		return Bookmark{}, err
	}
	i := indexOf(list, id)
	if i < 0 {
		return Bookmark{}, errf(ErrNotFound, "bookmark not found: %s", id)
	}
	if title != "" {
		list[i].Title = title
	}
	if url != "" {
		list[i].URL = url
	}
	if err := writeBookmarksFile(path, fileTitle, list); err != nil {
		return Bookmark{}, err
	}
	return list[i], nil
}
