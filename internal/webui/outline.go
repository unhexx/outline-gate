package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/unhexx/outline-gate/internal/config"
)

// OutlineController exposes live Outline key status and replace.
type OutlineController interface {
	Status() OutlineStatus
	ReplaceAccessKey(ctx context.Context, accessKey string) (OutlineStatus, error)
}

// OutlineStatus is safe JSON for the UI.
type OutlineStatus struct {
	AccessKeyRedacted string `json:"access_key_redacted"`
	Ready             bool   `json:"ready"`
	ServerIP          string `json:"server_ip,omitempty"`
	PersistFile       string `json:"persist_file,omitempty"`
}

// ClientOutline adapts outline.Client + optional persist path.
type ClientOutline struct {
	Ready       func() bool
	ServerIP    func() net.IP
	AccessKey   func() string
	SetKey      func(ctx context.Context, key string) error
	OnReplaced  func()
	PersistPath string
}

// Status implements OutlineController.
func (c *ClientOutline) Status() OutlineStatus {
	st := OutlineStatus{
		AccessKeyRedacted: config.RedactAccessKey(c.AccessKey()),
		Ready:             c.Ready != nil && c.Ready(),
		PersistFile:       c.PersistPath,
	}
	if c.ServerIP != nil {
		if ip := c.ServerIP(); ip != nil {
			st.ServerIP = ip.String()
		}
	}
	return st
}

// ReplaceAccessKey implements OutlineController.
func (c *ClientOutline) ReplaceAccessKey(ctx context.Context, accessKey string) (OutlineStatus, error) {
	accessKey = strings.TrimSpace(accessKey)
	if c.SetKey == nil {
		return OutlineStatus{}, fmt.Errorf("outline controller not configured")
	}
	if err := c.SetKey(ctx, accessKey); err != nil {
		return OutlineStatus{}, err
	}
	var persistWarn error
	if c.PersistPath != "" {
		if err := config.PersistAccessKey(c.PersistPath, accessKey); err != nil {
			persistWarn = err
		}
	}
	if c.OnReplaced != nil {
		c.OnReplaced()
	}
	st := c.Status()
	if persistWarn != nil {
		return st, fmt.Errorf("key applied but persist failed: %w", persistWarn)
	}
	return st, nil
}

func (s *Server) handleOutline(w http.ResponseWriter, r *http.Request) {
	if s.Outline == nil {
		writeErr(w, http.StatusNotFound, "outline API disabled")
		return
	}
	// Allow /api/v1/outline and /api/v1/outline/
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Outline.Status())
	case http.MethodPut, http.MethodPost:
		var body struct {
			AccessKey string `json:"access_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		st, err := s.Outline.ReplaceAccessKey(r.Context(), body.AccessKey)
		if err != nil {
			if strings.Contains(err.Error(), "persist failed") {
				writeJSON(w, http.StatusOK, map[string]any{
					"ok":      true,
					"warning": err.Error(),
					"status":  st,
				})
				return
			}
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"status": st,
		})
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
