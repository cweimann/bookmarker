package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Legacy flat layout → folder mapping (category slug → folder slug + title).
var legacyCategoryFolders = map[string]string{
	// Work
	"webapp-dev":       "work",
	"k12usa-dev":       "work",
	"k12usa-staging":   "work",
	"k12usa-live":      "work",
	"secureschool-dev": "work",
	"staff":            "work",
	// Home / infrastructure
	"homedev":    "home",
	"vm-home":    "home",
	"vm-servers": "home",
	"entertainment": "home",
	// Languages & tools
	"go":         "languages",
	"python":     "languages",
	"postgres":   "languages",
	"github-dev": "languages",
	// Systems
	"freebsd":            "systems",
	"linux":              "systems",
	"zfs-storage":        "systems",
	"networking":         "systems",
	"email":              "systems",
	"security":           "systems",
	"microsoft-windows":  "systems",
	// Media & reading
	"reading":     "media",
	"comics-fun":  "media",
	"media-video": "media",
	"reddit":      "media",
	"wikipedia":   "media",
	// News
	"news":          "news",
	"news-politics": "news",
	// Personal
	"firearms":     "personal",
	"shopping":     "personal",
	"ships-marine": "personal",
	"tech":         "personal",
}

var folderTitles = map[string]string{
	"work":      "Work",
	"home":      "Home",
	"languages": "Languages",
	"systems":   "Systems",
	"media":     "Media",
	"news":      "News",
	"personal":  "Personal",
	"unfiled":   "Unfiled",
}

// migrateLegacyIfNeeded moves data/inbox.md + data/categories/*.md into folders/.
// Runs once when folders/ is empty (or missing migrated marker) but legacy paths exist.
func (s *Store) migrateLegacyIfNeeded() error {
	foldersRoot := s.foldersRoot()
	legacyCats := filepath.Join(s.DataDir, "categories")
	legacyInbox := filepath.Join(s.DataDir, "inbox.md")
	marker := filepath.Join(s.DataDir, ".migrated-to-folders")

	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	hasLegacy := false
	if _, err := os.Stat(legacyInbox); err == nil {
		hasLegacy = true
	}
	if entries, err := os.ReadDir(legacyCats); err == nil && len(entries) > 0 {
		hasLegacy = true
	}
	if !hasLegacy {
		return nil
	}

	// If folders already have content beyond empty unfiled, skip auto-move.
	if entries, err := os.ReadDir(foldersRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				// Already have folders — only migrate if this looks fresh
				sub, _ := os.ReadDir(filepath.Join(foldersRoot, e.Name()))
				for _, f := range sub {
					name := f.Name()
					if strings.HasSuffix(name, ".md") && name != "inbox.md" && name != "_folder.md" {
						// Real categories already present; write marker and stop.
						return atomicWrite(marker, "skipped: folders already populated\n")
					}
				}
			}
		}
	}

	for slug, title := range folderTitles {
		if err := s.ensureFolder(slug, title); err != nil {
			return err
		}
	}

	// Move root inbox → unfiled/inbox.md (merge if both exist).
	if _, err := os.Stat(legacyInbox); err == nil {
		if err := s.mergeFileInto(legacyInbox, s.listPath("unfiled", "inbox"), "Inbox"); err != nil {
			return fmt.Errorf("migrate inbox: %w", err)
		}
		_ = os.Remove(legacyInbox)
	}

	if entries, err := os.ReadDir(legacyCats); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			catSlug := strings.TrimSuffix(e.Name(), ".md")
			folder := legacyCategoryFolders[catSlug]
			if folder == "" {
				folder = "unfiled"
			}
			if err := s.ensureFolder(folder, folderTitles[folder]); err != nil {
				return err
			}
			src := filepath.Join(legacyCats, e.Name())
			dst := s.listPath(folder, catSlug)
			if _, err := os.Stat(dst); err == nil {
				// Merge into existing
				if err := s.mergeFileInto(src, dst, ""); err != nil {
					return err
				}
				_ = os.Remove(src)
			} else {
				if err := os.Rename(src, dst); err != nil {
					// Cross-device fallback: copy
					data, rerr := os.ReadFile(src)
					if rerr != nil {
						return rerr
					}
					if err := atomicWrite(dst, string(data)); err != nil {
						return err
					}
					_ = os.Remove(src)
				}
			}
		}
		// Remove empty legacy categories dir if possible
		_ = os.Remove(legacyCats)
	}

	return atomicWrite(marker, "migrated from flat categories/\n")
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
			title = "Inbox"
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
