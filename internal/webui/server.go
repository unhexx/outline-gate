// Package webui provides the embedded bypass management UI and JSON API.
package webui

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"strings"

	"github.com/unhexx/outline-gate/internal/bypass"
)

// Manager is the bypass control plane used by handlers.
type Manager interface {
	Rules() []bypass.Rule
	AddRule(ctx context.Context, raw string) (bypass.Rule, error)
	RemoveRule(ctx context.Context, raw string) (bool, error)
	SetRules(ctx context.Context, raws []string) error
	EffectiveBypassNets() []net.IPNet
	ResolvedNets() []net.IPNet
	LastError() string
	Refresh(ctx context.Context) error
}

// RuntimeStatus is optional process info for the status tab.
type RuntimeStatus struct {
	Version       string
	SOCKSListen   string
	GatewayEnable bool
	HealthListen  string
}

// Server serves /ui/ and /api/v1/*.
type Server struct {
	Manager Manager
	Outline OutlineController // optional: key status / replace
	ConnLog ConnLog           // optional: live connection log
	Status  func() RuntimeStatus
	// Version is the process release string (e.g. "v0.4.0"); exposed publicly.
	Version string
	Token   string
	Static  fs.FS // usually //go:embed static
}

// Mount registers UI and API routes on mux. Health routes stay separate.
// Static UI is public (no secrets); all /api/* calls require Token except /api/v1/version.
func (s *Server) Mount(mux *http.ServeMux) {
	if s == nil {
		return
	}
	// Public: UI needs version before auth.
	mux.HandleFunc("/api/v1/version", s.handleVersion)
	if s.Manager != nil {
		api := http.HandlerFunc(s.routeBypassAPI)
		mux.Handle("/api/v1/bypass", tokenAuth(s.Token, api))
		mux.Handle("/api/v1/bypass/", tokenAuth(s.Token, api))
	}
	if s.Outline != nil {
		mux.Handle("/api/v1/outline", tokenAuth(s.Token, http.HandlerFunc(s.handleOutline)))
		mux.Handle("/api/v1/outline/", tokenAuth(s.Token, http.HandlerFunc(s.handleOutline)))
	}
	if s.ConnLog != nil {
		connAPI := http.HandlerFunc(s.routeConnectionsAPI)
		mux.Handle("/api/v1/connections", tokenAuth(s.Token, connAPI))
		mux.Handle("/api/v1/connections/", tokenAuth(s.Token, connAPI))
	}
	mux.Handle("/api/v1/status", tokenAuth(s.Token, http.HandlerFunc(s.handleStatus)))

	var static http.Handler
	if s.Static != nil {
		static = http.FileServer(http.FS(s.Static))
	} else {
		static = http.NotFoundHandler()
	}
	ui := http.StripPrefix("/ui/", static)
	mux.Handle("/ui/", ui)
	mux.Handle("/ui", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	}))
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	v := s.Version
	if v == "" && s.Status != nil {
		v = s.Status().Version
	}
	if v == "" {
		v = "vdev"
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *Server) routeBypassAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/bypass")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		s.handleBypass(w, r)
		return
	}
	switch path {
	case "/effective":
		s.handleEffective(w, r)
	case "/apply":
		s.handleApply(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleBypass(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := s.Manager.Rules()
		out := make([]map[string]string, 0, len(rules))
		for _, rule := range rules {
			out = append(out, map[string]string{
				"rule": rule.Raw,
				"kind": string(rule.Kind),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": out})

	case http.MethodPost:
		var body struct {
			Rule string `json:"rule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		rule, err := s.Manager.AddRule(r.Context(), body.Rule)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{
			"rule": rule.Raw,
			"kind": string(rule.Kind),
		})

	case http.MethodDelete:
		raw := r.URL.Query().Get("rule")
		if raw == "" {
			var body struct {
				Rule string `json:"rule"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			raw = body.Rule
		}
		if raw == "" {
			writeErr(w, http.StatusBadRequest, "rule is required")
			return
		}
		ok, err := s.Manager.RemoveRule(r.Context(), raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "rule not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})

	case http.MethodPut:
		var body struct {
			Rules []string `json:"rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Rules == nil {
			body.Rules = []string{}
		}
		if err := s.Manager.SetRules(r.Context(), body.Rules); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Rules)})

	default:
		w.Header().Set("Allow", "GET, POST, DELETE, PUT")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleEffective(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nets := s.Manager.EffectiveBypassNets()
	strs := make([]string, 0, len(nets))
	for _, n := range nets {
		strs = append(strs, n.String())
	}
	resolved := s.Manager.ResolvedNets()
	rstrs := make([]string, 0, len(resolved))
	for _, n := range resolved {
		rstrs = append(rstrs, n.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nets":      strs,
		"resolved":  rstrs,
		"dns_error": s.Manager.LastError(),
	})
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	err := s.Manager.Refresh(r.Context())
	// partial DNS failures still return 200 with dns_error
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"dns_error": errString(err),
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
