package webui

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// tokenAuth middleware requires Bearer token or HTTP Basic password == token.
func tokenAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || !checkToken(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="outline-gate", Basic realm="outline-gate"`)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func checkToken(r *http.Request, want string) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		// also allow query for simple browser asset loads is NOT done — use Bearer from JS
		return false
	}
	const bearer = "Bearer "
	if strings.HasPrefix(auth, bearer) {
		got := strings.TrimSpace(auth[len(bearer):])
		return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
	}
	const basic = "Basic "
	if strings.HasPrefix(auth, basic) {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth[len(basic):]))
		if err != nil {
			return false
		}
		// user:password — password must match token (user ignored)
		parts := strings.SplitN(string(raw), ":", 2)
		if len(parts) != 2 {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(want)) == 1
	}
	return false
}
