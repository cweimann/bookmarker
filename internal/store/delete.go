package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DeleteFolder removes a folder and promotes its children one level toward root.
// Same-named categories merge bookmarks; same-named folders merge recursively.
// The deleted folder's inbox bookmarks merge into the parent's inbox.
// Root (empty path) cannot be deleted. A folder holding files other than
// Markdown lists is refused, since the final RemoveAll would discard them.
func (s *Store) DeleteFolder(folderPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath, src, err := s.folder(folderPath)
	if err != nil {
		return err
	}
	if folderPath == "" {
		return errf(ErrInvalid, "cannot delete root")
	}
	if extra, err := nonListFiles(src); err != nil {
		return err
	} else if len(extra) > 0 {
		return errf(ErrInvalid, "folder %s contains non-bookmark files (%s); move them out first",
			folderPath, strings.Join(extra, ", "))
	}

	parent := parentOf(folderPath)
	if err := s.ensureFolderPath(parent); err != nil {
		return err
	}

	// Promote inbox bookmarks into the parent inbox.
	srcInbox := filepath.Join(src, "inbox.md")
	if _, err := os.Stat(srcInbox); err == nil {
		if err := s.mergeFileInto(srcInbox, s.listPath(parent, "inbox"), "Inbox"); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	parentAbs := s.abs(parent)
	for _, e := range entries {
		name := e.Name()
		if name == "inbox.md" || name == "_folder.md" {
			continue
		}
		sp := filepath.Join(src, name)
		dp := filepath.Join(parentAbs, name)
		if e.IsDir() {
			if _, err := os.Stat(dp); err == nil {
				if err := s.mergeDirInto(sp, dp); err != nil {
					return err
				}
			} else if err := os.Rename(sp, dp); err != nil {
				if err := s.copyDir(sp, dp); err != nil {
					return err
				}
			}
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if _, err := os.Stat(dp); err == nil {
			if err := s.mergeFileInto(sp, dp, ""); err != nil {
				return err
			}
		} else if err := os.Rename(sp, dp); err != nil {
			data, rerr := os.ReadFile(sp)
			if rerr != nil {
				return rerr
			}
			if err := atomicWrite(dp, string(data)); err != nil {
				return err
			}
		}
	}

	return os.RemoveAll(src)
}

// nonListFiles returns files under dir that are neither .md lists nor
// leftover atomicWrite temp files.
func nonListFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".tmp-") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}
