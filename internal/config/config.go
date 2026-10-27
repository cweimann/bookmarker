// Package config loads bookmarker.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is looked up in the working directory, then next to the executable.
const FileName = "bookmarker.yaml"

// Config is the on-disk configuration. Keys missing from the file keep their defaults.
type Config struct {
	// Listen is one or more host:port addresses; each gets its own listener.
	Listen []string `yaml:"listen"`
	// Edit lists client IPs/CIDRs that may mutate (and therefore also view).
	Edit []string `yaml:"edit"`
	// View lists client IPs/CIDRs that may browse read-only. Edit implies view,
	// so entries need not be repeated here.
	View []string `yaml:"view"`
	// Hosts are extra DNS names accepted in the Host header. IP literals and
	// localhost are always accepted; other names are refused (DNS rebinding).
	Hosts []string `yaml:"hosts"`
	// Data is the bookmark directory; relative paths resolve against the
	// config file's directory (or the working directory with no config file).
	Data string `yaml:"data"`
}

// Default is used when no config file exists: loopback editors only.
func Default() Config {
	return Config{
		Listen: []string{"127.0.0.1:8787"},
		Edit:   []string{"127.0.0.1", "::1"},
		View:   nil,
		Data:   "data",
	}
}

// Find returns the config path to load: explicit (flag or env) if set,
// else ./bookmarker.yaml, else bookmarker.yaml beside the executable.
// It returns "" when none exists and explicit is empty.
func Find(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config %s: %w", explicit, err)
		}
		return explicit, nil
	}
	candidates := []string{FileName}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), FileName))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", nil
}

// Load reads path (or returns Default when path is ""), validates it, and
// makes Data absolute.
func Load(path string) (Config, error) {
	cfg := Default()
	base, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return Config{}, err
		}
		base = filepath.Dir(abs)
	}
	if err := cfg.Validate(); err != nil {
		if path != "" {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		return Config{}, err
	}
	if !filepath.IsAbs(cfg.Data) {
		cfg.Data = filepath.Join(base, cfg.Data)
	}
	return cfg, nil
}

// Validate checks listen addresses and the client allowlists.
func (c Config) Validate() error {
	if len(c.Listen) == 0 {
		return errors.New("listen: at least one address required")
	}
	for _, a := range c.Listen {
		if _, _, err := net.SplitHostPort(a); err != nil {
			return fmt.Errorf("listen %q: %w", a, err)
		}
	}
	if len(c.Edit) == 0 && len(c.View) == 0 {
		return errors.New("edit/view: at least one client IP or CIDR required")
	}
	if _, err := c.EditNets(); err != nil {
		return err
	}
	if _, err := c.ViewNets(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Data) == "" {
		return errors.New("data: directory required")
	}
	return nil
}

// EditNets parses Edit; a bare IP becomes a single-address prefix.
func (c Config) EditNets() ([]netip.Prefix, error) {
	return parseNets("edit", c.Edit)
}

// ViewNets parses View (view-only extras; edit is not included).
func (c Config) ViewNets() ([]netip.Prefix, error) {
	return parseNets("view", c.View)
}

func parseNets(label string, list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, a := range list {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.Contains(a, "/") {
			p, err := netip.ParsePrefix(a)
			if err != nil {
				return nil, fmt.Errorf("%s %q: %w", label, a, err)
			}
			out = append(out, p.Masked())
			continue
		}
		ip, err := netip.ParseAddr(a)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", label, a, err)
		}
		out = append(out, netip.PrefixFrom(ip.Unmap(), ip.Unmap().BitLen()))
	}
	return out, nil
}
