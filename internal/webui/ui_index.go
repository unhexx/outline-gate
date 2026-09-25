package webui

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// serveUI serves embedded assets. index.html is rewritten so the configured
// API token is applied by the page and the operator does not type it.
func (s *Server) serveUI(static http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" || p == "index.html" {
			s.serveIndex(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Static == nil {
		http.NotFound(w, r)
		return
	}
	raw, err := fs.ReadFile(s.Static, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	page := injectUIToken(raw, s.Token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}

// injectUIToken inserts window.__OG_UI_TOKEN__ before </head>.
// json.Marshal escapes <, >, & so the token cannot break out of the script.
func injectUIToken(page []byte, token string) []byte {
	quoted, err := json.Marshal(token)
	if err != nil {
		quoted = []byte(`""`)
	}
	tag := []byte("<script>window.__OG_UI_TOKEN__=" + string(quoted) + ";</script>")
	const needle = "</head>"
	if i := bytes.Index(page, []byte(needle)); i >= 0 {
		out := make([]byte, 0, len(page)+len(tag))
		out = append(out, page[:i]...)
		out = append(out, tag...)
		out = append(out, page[i:]...)
		return out
	}
	return append(tag, page...)
}
