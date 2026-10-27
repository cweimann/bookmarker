package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := New(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestCategoryTraversalRejected(t *testing.T) {
	s, root := newTestStore(t)
	outside := filepath.Join(root, "notes.md")
	const prose = "# Notes\n\nprose that is not a bookmark\n"
	if err := os.WriteFile(outside, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cat := range []string{"../notes", `..\notes`, "a/../../notes", "C:notes", "Notes", "x.y"} {
		if _, err := s.ListBookmarks("", cat); !errors.Is(err, ErrInvalid) {
			t.Errorf("ListBookmarks(%q) = %v, want ErrInvalid", cat, err)
		}
		if _, err := s.AddBookmark("", cat, "x", "https://x.example", "t"); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddBookmark(%q) = %v, want ErrInvalid", cat, err)
		}
		if _, err := s.RenameCategory("", cat, "Stolen"); !errors.Is(err, ErrInvalid) {
			t.Errorf("RenameCategory(%q) = %v, want ErrInvalid", cat, err)
		}
		if err := s.MoveCategory(cat, "", ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("MoveCategory(%q) = %v, want ErrInvalid", cat, err)
		}
		if err := s.MoveBookmark("id", "", cat, "", "inbox"); !errors.Is(err, ErrInvalid) {
			t.Errorf("MoveBookmark(%q) = %v, want ErrInvalid", cat, err)
		}
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != prose {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}

func TestFolderPathTraversalRejected(t *testing.T) {
	s, _ := newTestStore(t)
	for _, p := range []string{"..", "../x", `..\x`, "work/..", "C:", "Work", "a b", "inbox"} {
		if _, err := s.List(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("List(%q) = %v, want ErrInvalid", p, err)
		}
	}
}

func TestReadsDoNotCreateFolders(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.List("a/b/c/d/e"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("List missing = %v, want ErrNotFound", err)
	}
	if _, err := s.ListBookmarks("ghost", "inbox"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListBookmarks missing folder = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, "a")); !os.IsNotExist(err) {
		t.Fatalf("List created a directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, "ghost")); !os.IsNotExist(err) {
		t.Fatalf("ListBookmarks created a directory: %v", err)
	}
}

func TestMoveCategoryRequiresExistingFolder(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateCategory("", "Tools"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCategory("tools", "", "x/y/z"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MoveCategory to missing folder = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, "x")); !os.IsNotExist(err) {
		t.Fatal("MoveCategory created the destination folder")
	}
}

func TestNewlinesCannotForgeEntries(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddBookmark("", "inbox", "Evil\nurl: https://evil.example\n## Ghost", "https://real.example", "a\nb"); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListBookmarks("", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d bookmarks, want 1: %#v", len(list), list)
	}
	b := list[0]
	if b.URL != "https://real.example" || b.Title != "Evil url: https://evil.example ## Ghost" || b.Source != "a b" {
		t.Fatalf("unexpected bookmark %#v", b)
	}
	if _, err := s.AddBookmark("", "inbox", "x", "https://a.example\nid: forged", "t"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("newline in URL = %v, want ErrInvalid", err)
	}
	cat, err := s.CreateCategory("", "Line\n## one")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Title != "Line ## one" {
		t.Fatalf("category title %q", cat.Title)
	}
}

func TestURLSchemes(t *testing.T) {
	s, _ := newTestStore(t)
	for _, u := range []string{"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,x", "vbscript:x", "java\tscript:x"} {
		if _, err := s.AddBookmark("", "inbox", "x", u, "t"); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddBookmark(%q) = %v, want ErrInvalid", u, err)
		}
	}
	for in, want := range map[string]string{
		"https://ok.example": "https://ok.example",
		"ssh://host":         "ssh://host",
		"example.com/path":   "https://example.com/path",
	} {
		b, err := s.AddBookmark("", "inbox", "", in, "t")
		if err != nil || b.URL != want {
			t.Errorf("AddBookmark(%q) = %q, %v; want %q", in, b.URL, err, want)
		}
	}
	b, _ := s.AddBookmark("", "inbox", "t", "https://x.example", "t")
	if _, err := s.UpdateBookmark("", "inbox", b.ID, "", "javascript:x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UpdateBookmark javascript: = %v, want ErrInvalid", err)
	}
}

func TestMoveBookmarkWritesDestinationFirst(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateCategory("", "Tools"); err != nil {
		t.Fatal(err)
	}
	b, err := s.AddBookmark("", "inbox", "x", "https://x.example", "t")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the destination write: bookmark in both files.
	if err := writeBookmarksFile(s.listPath("", "tools"), "Tools", []Bookmark{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveBookmark(b.ID, "", "inbox", "", "tools"); err != nil {
		t.Fatal(err)
	}
	inbox, _ := s.ListBookmarks("", "inbox")
	tools, _ := s.ListBookmarks("", "tools")
	if len(inbox) != 0 || len(tools) != 1 {
		t.Fatalf("retry should finish the move without duplicating: inbox=%d tools=%d", len(inbox), len(tools))
	}
}

func TestAtomicWriteReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.md")
	if err := atomicWrite(p, "one"); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(p, "two"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "two" {
		t.Fatalf("got %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %d entries", len(entries))
	}
}

func TestRenameCategory(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateCategory("", "Tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory("", "Other"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBookmark("", "tools", "x", "https://x.example", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameCategory("", "tools", "Other"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto existing = %v, want ErrExists", err)
	}
	c, err := s.RenameCategory("", "tools", "Dev Tools")
	if err != nil {
		t.Fatal(err)
	}
	if c.Slug != "dev-tools" {
		t.Fatalf("slug %q", c.Slug)
	}
	if _, err := os.Stat(s.listPath("", "tools")); !os.IsNotExist(err) {
		t.Fatal("old category file still present")
	}
	list, err := s.ListBookmarks("", "dev-tools")
	if err != nil || len(list) != 1 {
		t.Fatalf("renamed list = %v, %v", list, err)
	}
}

func TestDeleteFolderRefusesNonListFiles(t *testing.T) {
	s, _ := newTestStore(t)
	f, err := s.CreateFolder("", "Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.abs(f.Path), "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = s.DeleteFolder(f.Path)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("DeleteFolder = %v, want ErrInvalid naming notes.txt", err)
	}
	if _, err := os.Stat(filepath.Join(s.abs(f.Path), "notes.txt")); err != nil {
		t.Fatal("non-list file was removed")
	}
}

func TestCreateFolderDepthAndReserved(t *testing.T) {
	s, _ := newTestStore(t)
	a, err := s.CreateFolder("", "A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateFolder(a.Path, "B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFolder(b.Path, "C"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("depth 3 = %v, want ErrInvalid", err)
	}
	if _, err := s.CreateFolder("", "Inbox"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("folder named inbox = %v, want ErrInvalid", err)
	}
	if _, err := s.CreateFolder("missing", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing parent = %v, want ErrNotFound", err)
	}
}
