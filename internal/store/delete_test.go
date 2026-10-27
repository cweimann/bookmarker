package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteFolderPromotesAndMerges(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Root already has inbox. Create sibling category "tools" with one bookmark.
	tools, err := s.CreateCategory("", "Tools")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBookmarksFile(s.listPath("", tools.Slug), "Tools", []Bookmark{
		{ID: "root-tools-1", Title: "Root Tool", URL: "https://root.example/tool", Source: "t", Saved: "2020-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}

	work, err := s.CreateFolder("", "Work")
	if err != nil {
		t.Fatal(err)
	}
	// work inbox bookmark
	if _, err := s.AddToFolderInbox(work.Path, "Work Inbox Item", "https://work.example/inbox", "t"); err != nil {
		t.Fatal(err)
	}
	// work category tools (name conflict with root) + unique category
	wTools, err := s.CreateCategory(work.Path, "Tools")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBookmarksFile(s.listPath(work.Path, wTools.Slug), "Tools", []Bookmark{
		{ID: "work-tools-1", Title: "Work Tool", URL: "https://work.example/tool", Source: "t", Saved: "2020-01-02T00:00:00Z"},
		{ID: "dup", Title: "Dup", URL: "https://root.example/tool", Source: "t", Saved: "2020-01-03T00:00:00Z"}, // URL dup
	}); err != nil {
		t.Fatal(err)
	}
	servers, err := s.CreateCategory(work.Path, "Servers")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBookmarksFile(s.listPath(work.Path, servers.Slug), "Servers", []Bookmark{
		{ID: "srv-1", Title: "VM", URL: "ssh://vm.example", Source: "t", Saved: "2020-01-04T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}

	// Nested folder under work that also collides with a root folder we'll create
	personal, err := s.CreateFolder("", "Personal")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := s.CreateFolder(work.Path, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToFolderInbox(nested.Path, "Nested Personal", "https://nested.example", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToFolderInbox(personal.Path, "Root Personal", "https://personal.example", "t"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteFolder(work.Path); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(s.abs("work")); !os.IsNotExist(err) {
		t.Fatalf("work folder still present: %v", err)
	}

	// Root cannot be deleted
	if err := s.DeleteFolder(""); err == nil {
		t.Fatal("expected error deleting root")
	}

	rootListing, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	folderSlugs := map[string]bool{}
	for _, f := range rootListing.Folders {
		folderSlugs[f.Slug] = true
	}
	if folderSlugs["work"] {
		t.Fatal("work still listed")
	}
	if !folderSlugs["personal"] {
		t.Fatal("personal folder missing after merge")
	}
	catSlugs := map[string]bool{}
	for _, c := range rootListing.Categories {
		catSlugs[c.Slug] = true
	}
	if !catSlugs["tools"] || !catSlugs["servers"] {
		t.Fatalf("expected tools+servers at root, got %#v", catSlugs)
	}

	// Inbox: original root empty-ish + work inbox item
	inbox, err := s.ListBookmarks("", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	foundWorkInbox := false
	for _, b := range inbox {
		if b.URL == "https://work.example/inbox" {
			foundWorkInbox = true
		}
	}
	if !foundWorkInbox {
		t.Fatalf("work inbox bookmark not promoted: %#v", inbox)
	}

	// Tools merged (URL dedupe)
	toolsList, err := s.ListBookmarks("", "tools")
	if err != nil {
		t.Fatal(err)
	}
	urls := map[string]int{}
	for _, b := range toolsList {
		urls[b.URL]++
	}
	if urls["https://root.example/tool"] != 1 {
		t.Fatalf("expected one root tool url, got %#v", urls)
	}
	if urls["https://work.example/tool"] != 1 {
		t.Fatalf("expected work tool merged, got %#v", urls)
	}

	// Personal folders merged inboxes
	pInbox, err := s.ListBookmarks("personal", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	foundNested, foundRoot := false, false
	for _, b := range pInbox {
		if b.URL == "https://nested.example" {
			foundNested = true
		}
		if b.URL == "https://personal.example" {
			foundRoot = true
		}
	}
	if !foundNested || !foundRoot {
		t.Fatalf("personal inbox merge failed: %#v", pInbox)
	}

	// Servers promoted as file
	if _, err := os.Stat(filepath.Join(dir, "servers.md")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFolderNestedParent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateFolder("", "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateFolder(a.Path, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToFolderInbox(b.Path, "Deep", "https://deep.example", "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(b.Path); err != nil {
		t.Fatal(err)
	}
	inbox, err := s.ListBookmarks(a.Path, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, bm := range inbox {
		if bm.URL == "https://deep.example" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("expected deep inbox in alpha: %#v", inbox)
	}
	listing, err := s.List(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range listing.Folders {
		if f.Slug == "beta" {
			t.Fatal("beta still present")
		}
	}
}
