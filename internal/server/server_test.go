package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/cweimann/bookmarker/internal/store"
)

func newTestServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFolder("", "Work"); err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Store: st,
		Allow: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"),
			netip.MustParsePrefix("10.1.32.0/24"),
		},
		Hosts: []string{"bookmarks.lan"},
	}
	return srv.Handler(), st
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	if r.RemoteAddr == "192.0.2.1:1234" {
		r.RemoteAddr = "127.0.0.1:5555"
	}
	if r.Host == "example.com" {
		r.Host = "127.0.0.1:8787"
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func post(path, body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func folderCount(t *testing.T, st *store.Store) int {
	t.Helper()
	l, err := st.List("")
	if err != nil {
		t.Fatal(err)
	}
	return len(l.Folders)
}

func TestCrossSiteWritesRefused(t *testing.T) {
	h, st := newTestServer(t)
	body := `{"path":"work","x":"="}`

	if rr := do(h, post("/api/folders/delete", body, "text/plain")); rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d, want 415", rr.Code)
	}
	r := post("/api/folders/delete", body, "application/json")
	r.Header.Set("Origin", "https://evil.example")
	if rr := do(h, r); rr.Code != http.StatusForbidden {
		t.Errorf("foreign Origin: %d, want 403", rr.Code)
	}
	r = post("/api/folders/delete", body, "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if rr := do(h, r); rr.Code != http.StatusForbidden {
		t.Errorf("Sec-Fetch-Site cross-site: %d, want 403", rr.Code)
	}
	if n := folderCount(t, st); n != 1 {
		t.Fatalf("folder deleted by refused request (folders=%d)", n)
	}

	r = post("/api/folders/delete", body, "application/json; charset=utf-8")
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if rr := do(h, r); rr.Code != http.StatusOK {
		t.Fatalf("same-origin JSON: %d %s", rr.Code, rr.Body)
	}
	if n := folderCount(t, st); n != 0 {
		t.Fatalf("same-origin delete did not apply (folders=%d)", n)
	}
}

func TestClientAllowlist(t *testing.T) {
	h, _ := newTestServer(t)
	cases := map[string]int{
		"127.0.0.1:5000":       http.StatusOK,
		"10.1.32.7:5000":       http.StatusOK,
		"10.1.33.7:5000":       http.StatusForbidden,
		"192.168.1.5:5000":     http.StatusForbidden,
		"[::1]:5000":           http.StatusForbidden,
		"[::ffff:10.1.32.9]:1": http.StatusOK,
	}
	for addr, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		r.RemoteAddr = addr
		r.Host = "127.0.0.1:8787"
		if rr := do(h, r); rr.Code != want {
			t.Errorf("%s: %d, want %d", addr, rr.Code, want)
		}
	}
}

func TestHostHeaderAllowlist(t *testing.T) {
	h, _ := newTestServer(t)
	cases := map[string]int{
		"127.0.0.1:8787":      http.StatusOK,
		"10.1.32.219:8787":    http.StatusOK,
		"localhost:8787":      http.StatusOK,
		"bookmarks.lan:8787":  http.StatusOK,
		"rebind.evil.example": http.StatusMisdirectedRequest,
	}
	for host, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		r.Host = host
		if rr := do(h, r); rr.Code != want {
			t.Errorf("Host %s: %d, want %d", host, rr.Code, want)
		}
	}
}

func TestAddEndpoint(t *testing.T) {
	h, st := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/add?url=https%3A%2F%2Fsaved.example%2Fpage&title=Saved", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.Header.Set("Referer", "https://saved.example/")
	rr := do(h, r)
	if rr.Code != http.StatusFound || rr.Header().Get("Location") != "https://saved.example/page" {
		t.Fatalf("redirect: %d %q", rr.Code, rr.Header().Get("Location"))
	}

	r = httptest.NewRequest(http.MethodGet, "/add?url=https%3A%2F%2Fplanted.example", nil)
	r.Header.Set("Sec-Fetch-Dest", "image")
	if rr := do(h, r); rr.Code != http.StatusForbidden {
		t.Fatalf("img-planted add: %d, want 403", rr.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/add?url=javascript%3Aalert(1)", nil)
	if rr := do(h, r); rr.Code != http.StatusBadRequest {
		t.Fatalf("javascript: add: %d, want 400", rr.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/add?url=ssh%3A%2F%2Fhost&noredirect=1", nil)
	if rr := do(h, r); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "ssh://host") {
		t.Fatalf("ssh add page: %d", rr.Code)
	}

	list, err := st.ListBookmarks("", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("inbox has %d bookmarks, want 2: %#v", len(list), list)
	}
}

func TestErrorStatusCodes(t *testing.T) {
	h, _ := newTestServer(t)
	cases := []struct {
		req  *http.Request
		want int
	}{
		{httptest.NewRequest(http.MethodGet, "/api/bookmarks?category=../x", nil), http.StatusBadRequest},
		{httptest.NewRequest(http.MethodGet, "/api/bookmarks?category=nope", nil), http.StatusNotFound},
		{httptest.NewRequest(http.MethodGet, "/api/listing?path=a/b/c", nil), http.StatusNotFound},
		{post("/api/folders", `{"parentPath":"","title":"Work"}`, "application/json"), http.StatusConflict},
		{post("/api/folders", `not json`, "application/json"), http.StatusBadRequest},
	}
	for _, c := range cases {
		if rr := do(h, c.req); rr.Code != c.want {
			t.Errorf("%s %s: %d, want %d (%s)", c.req.Method, c.req.URL, rr.Code, c.want, rr.Body)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h, _ := newTestServer(t)
	rr := do(h, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	for _, k := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options"} {
		if rr.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}
