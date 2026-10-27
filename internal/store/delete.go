package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeleteFolder removes a folder and promotes its children one level toward root.
// Same-named categories merge bookmarks; same-named folders merge recursively.
// The deleted folder's inbox bookmarks merge into the parent's inbox.
// Root (empty path) cannot be deleted.
func (s *Store) DeleteFolder(folderPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	folderPath = cleanPath(folderPath)
	if folderPath == "" {
		return fmt.Errorf("cannot delete root")
	}
	src := s.abs(folderPath)
	st, err := os.Stat(src)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("folder not found: %s", folderPath)
	}

	parent := ""
	if i := strings.LastIndex(folderPath, "/"); i >= 0 {
		parent = folderPath[:i]
	}
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
