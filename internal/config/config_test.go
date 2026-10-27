package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	p := write(t, `
listen:
  - 127.0.0.1:8787
  - 10.1.32.219:8787
edit:
  - 127.0.0.1
  - 10.1.32.0/24
view:
  - 192.168.1.0/24
data: bm-data
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Listen) != 2 || cfg.Listen[1] != "10.1.32.219:8787" {
		t.Fatalf("listen %v", cfg.Listen)
	}
	if want := filepath.Join(filepath.Dir(p), "bm-data"); cfg.Data != want {
		t.Fatalf("data %q, want %q (relative to config file)", cfg.Data, want)
	}
	edit, err := cfg.EditNets()
	if err != nil {
		t.Fatal(err)
	}
	view, err := cfg.ViewNets()
	if err != nil {
		t.Fatal(err)
	}
	in := func(nets []netip.Prefix, s string) bool {
		ip := netip.MustParseAddr(s)
		for _, n := range nets {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !in(edit, "127.0.0.1") || !in(edit, "10.1.32.200") || in(edit, "10.1.33.1") {
		t.Fatalf("edit nets unexpected: %v", edit)
	}
	if !in(view, "192.168.1.5") || in(view, "10.1.32.1") {
		t.Fatalf("view nets unexpected: %v", view)
	}
}

func TestDefaultsWhenKeysMissing(t *testing.T) {
	cfg, err := Load(write(t, "hosts: [bookmarks.lan]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Listen) != 1 || cfg.Listen[0] != "127.0.0.1:8787" {
		t.Fatalf("listen default %v", cfg.Listen)
	}
	if len(cfg.Edit) != 2 {
		t.Fatalf("edit default %v", cfg.Edit)
	}
	if len(cfg.View) != 0 {
		t.Fatalf("view default %v", cfg.View)
	}
	if _, err := Load(write(t, "")); err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if _, err := Load(""); err != nil {
		t.Fatalf("no file: %v", err)
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	for name, body := range map[string]string{
		"bad cidr":         "edit: [10.1.32.0/99]\n",
		"bad ip":           "edit: [not-an-ip]\n",
		"empty edit+view":  "edit: []\nview: []\n",
		"legacy allow key": "allow: [127.0.0.1]\n",
		"no port":          "listen: [127.0.0.1]\n",
		"typo key":         "listn: [127.0.0.1:8787]\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestViewOnlyAllowed(t *testing.T) {
	cfg, err := Load(write(t, "edit: []\nview: [10.1.32.0/24]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Edit) != 0 || len(cfg.View) != 1 {
		t.Fatalf("edit=%v view=%v", cfg.Edit, cfg.View)
	}
}

func TestFindPrefersExplicit(t *testing.T) {
	p := write(t, "")
	got, err := Find(p)
	if err != nil || got != p {
		t.Fatalf("Find(%q) = %q, %v", p, got, err)
	}
	if _, err := Find(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing explicit config should error")
	}
}
