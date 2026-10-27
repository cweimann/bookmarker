package server

import (
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

const maxBodyBytes = 1 << 20

const contentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

var loopback = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// guard applies the client allowlist, Host check, and cross-site write
// protection to every request before routing.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.clientAllowed(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.hostAllowed(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		if r.URL.Path == "/add" {
			// The bookmarklet navigates the tab here. Refuse subresource loads
			// (<img>, fetch, iframes) that other sites could use to plant links.
			if d := r.Header.Get("Sec-Fetch-Dest"); d != "" && d != "document" {
				http.Error(w, "/add must be opened as a page", http.StatusForbidden)
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if code, msg := checkSameOriginJSON(r); code != 0 {
				writeJSONError(w, msg, code)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// checkSameOriginJSON requires writes to be same-origin JSON. A JSON
// Content-Type forces a CORS preflight that this server never grants, and
// Origin / Sec-Fetch-Site reject browsers that say the request is cross-site.
func checkSameOriginJSON(r *http.Request) (int, string) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return http.StatusUnsupportedMediaType, "Content-Type must be application/json"
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return http.StatusForbidden, "cross-site request refused"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
			return http.StatusForbidden, "cross-origin request refused"
		}
	}
	return 0, ""
}

func (s *Server) clientAllowed(remoteAddr string) bool {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := ap.Addr().Unmap().WithZone("")
	nets := s.Allow
	if len(nets) == 0 {
		nets = loopback
	}
	for _, p := range nets {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// hostAllowed accepts IP literals, localhost, and configured names. Anything
// else may be a DNS-rebinding name pointed at this server.
func (s *Server) hostAllowed(host string) bool {
	if host == "" {
		return true
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.Trim(name, "[]"), ".")
	if _, err := netip.ParseAddr(name); err == nil {
		return true
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	for _, h := range s.Hosts {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}
