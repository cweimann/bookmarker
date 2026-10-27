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
allow:
  - 127.0.0.1
  - 10.1.32.0/24
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
	nets, err := cfg.AllowedNets()
	if err != nil {
		t.Fatal(err)
	}
	in := func(s string) bool {
		ip := netip.MustParseAddr(s)
		for _, n := range nets {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	for ip, want := range map[string]bool{"127.0.0.1": true, "10.1.32.200": true, "10.1.33.1": false, "127.0.0.2": false} {
		if in(ip) != want {
			t.Errorf("%s allowed=%v, want %v", ip, !want, want)
		}
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
	if len(cfg.Allow) != 2 {
		t.Fatalf("allow default %v", cfg.Allow)
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
		"bad cidr":    "allow: [10.1.32.0/99]\n",
		"bad ip":      "allow: [not-an-ip]\n",
		"empty allow": "allow: []\n",
		"no port":     "listen: [127.0.0.1]\n",
		"typo key":    "listn: [127.0.0.1:8787]\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
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
