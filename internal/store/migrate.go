package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrateLegacyIfNeeded converts:
//  1. data/folders/<name>/...  → data/<name>/...  (tree root = data/)
//  2. unfiled/inbox.md         → data/inbox.md (merge)
//  3. unfiled categories        → data/<cat>.md
//
// Also handles older flat data/categories/ if somehow still present.
func (s *Store) migrateLegacyIfNeeded() error {
	marker := filepath.Join(s.DataDir, ".migrated-to-tree")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	legacyFolders := filepath.Join(s.DataDir, "folders")
	legacyCats := filepath.Join(s.DataDir, "categories")
	legacyInbox := filepath.Join(s.DataDir, "inbox.md")

	hasLegacy := false
	if st, err := os.Stat(legacyFolders); err == nil && st.IsDir() {
		hasLegacy = true
	}
	if entries, err := os.ReadDir(legacyCats); err == nil && len(entries) > 0 {
		hasLegacy = true
	}
	// Flat inbox without folders/ tree already is new layout if no folders/ dir.
	if !hasLegacy {
		// Ensure root meta exists for fresh installs; marker not needed.
		return nil
	}

	if err := s.ensureFolderPath(""); err != nil {
		return err
	}

	// Promote each data/folders/<slug> → data/<slug>
	if entries, err := os.ReadDir(legacyFolders); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			src := filepath.Join(legacyFolders, name)
			if name == "unfiled" {
				if err := s.promoteUnfiled(src); err != nil {
					return err
				}
				_ = os.RemoveAll(src)
				continue
			}
			dst := filepath.Join(s.DataDir, name)
			if _, err := os.Stat(dst); err == nil {
				if err := s.mergeDirInto(src, dst); err != nil {
					return fmt.Errorf("merge %s: %w", name, err)
				}
				_ = os.RemoveAll(src)
			} else {
				if err := os.Rename(src, dst); err != nil {
					if err := s.copyDir(src, dst); err != nil {
						return err
					}
					_ = os.RemoveAll(src)
				}
			}
		}
		_ = os.RemoveAll(legacyFolders)
	}

	// Old flat categories → root
	if entries, err := os.ReadDir(legacyCats); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			src := filepath.Join(legacyCats, e.Name())
			dst := filepath.Join(s.DataDir, e.Name())
			if _, err := os.Stat(dst); err == nil {
				if err := s.mergeFileInto(src, dst, ""); err != nil {
					return err
				}
				_ = os.Remove(src)
			} else {
				_ = os.Rename(src, dst)
			}
		}
		_ = os.Remove(legacyCats)
	}

	// Stray legacy root inbox already at data/inbox.md is fine.
	_ = legacyInbox

	return atomicWrite(marker, "migrated to tree layout (data/ is root)\n")
}

func (s *Store) promoteUnfiled(src string) error {
	// Merge unfiled/inbox.md → root inbox.md
	srcInbox := filepath.Join(src, "inbox.md")
	if _, err := os.Stat(srcInbox); err == nil {
		if err := s.mergeFileInto(srcInbox, s.listPath("", "inbox"), "Inbox"); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// Nested under unfiled → lift to root with same name
			childSrc := filepath.Join(src, name)
			dst := filepath.Join(s.DataDir, name)
			if _, err := os.Stat(dst); err == nil {
				if err := s.mergeDirInto(childSrc, dst); err != nil {
					return err
				}
			} else if err := os.Rename(childSrc, dst); err != nil {
				if err := s.copyDir(childSrc, dst); err != nil {
					return err
				}
			}
			continue
		}
		if name == "inbox.md" || name == "_folder.md" {
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		srcFile := filepath.Join(src, name)
		dstFile := filepath.Join(s.DataDir, name)
		if _, err := os.Stat(dstFile); err == nil {
			if err := s.mergeFileInto(srcFile, dstFile, ""); err != nil {
				return err
			}
		} else {
			_ = os.Rename(srcFile, dstFile)
		}
	}
	return nil
}

func (s *Store) mergeDirInto(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := s.mergeDirInto(sp, dp); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Stat(dp); err == nil {
			if e.Name() == "_folder.md" {
				continue // keep destination title
			}
			title := ""
			if e.Name() == "inbox.md" {
				title = "Inbox"
			}
			if err := s.mergeFileInto(sp, dp, title); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(sp)
			if err != nil {
				return err
			}
			if err := atomicWrite(dp, string(data)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := s.copyDir(sp, dp); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(sp)
		if err != nil {
			return err
		}
		if err := atomicWrite(dp, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) mergeFileInto(src, dst, titleOverride string) error {
	srcList, err := parseBookmarksFile(src)
	if err != nil {
		return err
	}
	dstList, err := parseBookmarksFile(dst)
	if err != nil {
		return err
	}
	title := titleOverride
	if title == "" {
		if data, err := os.ReadFile(dst); err == nil {
			title = parseTitle(string(data), filepath.Base(dst))
		} else if data, err := os.ReadFile(src); err == nil {
			title = parseTitle(string(data), filepath.Base(src))
		} else {
			title = "Category"
		}
	}
	seen := map[string]bool{}
	for _, b := range dstList {
		seen[b.ID] = true
		seen[strings.ToLower(b.URL)] = true
	}
	for _, b := range srcList {
		if seen[b.ID] || seen[strings.ToLower(b.URL)] {
			continue
		}
		dstList = append(dstList, b)
	}
	return writeBookmarksFile(dst, title, dstList)
}
