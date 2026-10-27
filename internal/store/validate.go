package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Error kinds; match with errors.Is.
var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
)

// Error carries a user-facing message plus one of the Err* kinds.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

func errf(kind error, format string, a ...any) error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, a...)}
}

// Every folder segment and category name on disk must match this; Slugify
// only produces matching names. It keeps ".." and drive or stream syntax out of paths.
var validSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// parseFolderPath validates a client-supplied folder path ("" is root).
func parseFolderPath(p string) (string, error) {
	p = strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
	if p == "" || p == "." {
		return "", nil
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if !validSlug.MatchString(part) || part == "inbox" {
			return "", errf(ErrInvalid, "invalid folder path %q", p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// parseCategory validates a client-supplied category slug ("" is inbox).
func parseCategory(c string) (string, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "inbox", nil
	}
	if !validSlug.MatchString(c) {
		return "", errf(ErrInvalid, "invalid category %q", c)
	}
	return c, nil
}

// cleanLine makes s safe for a single Markdown line: control characters and
// runs of whitespace (including newlines) collapse to one space.
func cleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var schemeRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*):`)

var blockedSchemes = map[string]bool{
	"javascript": true,
	"vbscript":   true,
	"data":       true,
	"blob":       true,
}

// cleanURL validates a bookmark URL. A URL without a scheme gets https://.
func cleanURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", errf(ErrInvalid, "url required")
	}
	if strings.IndexFunc(u, unicode.IsControl) >= 0 {
		return "", errf(ErrInvalid, "url must be a single line without control characters")
	}
	m := schemeRe.FindStringSubmatch(u)
	if m == nil {
		return "https://" + u, nil
	}
	if blockedSchemes[strings.ToLower(m[1])] {
		return "", errf(ErrInvalid, "%s: URLs are not allowed", strings.ToLower(m[1]))
	}
	return u, nil
}

func displayPath(p string) string {
	if p == "" {
		return "(root)"
	}
	return p
}
