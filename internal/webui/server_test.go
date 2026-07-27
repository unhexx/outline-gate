package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/unhex/outline-gate/internal/bypass"
	"github.com/unhex/outline-gate/internal/config"
)

type fakeOutline struct {
	key   string
	ready bool
}

func (f *fakeOutline) Status() OutlineStatus {
	return OutlineStatus{
		AccessKeyRedacted: config.RedactAccessKey(f.key),
		Ready:             f.ready,
		ServerIP:          "1.2.3.4",
		PersistFile:       "/tmp/key",
	}
}

func (f *fakeOutline) ReplaceAccessKey(ctx context.Context, accessKey string) (OutlineStatus, error) {
	f.key = accessKey
	f.ready = true
	return f.Status(), nil
}

func TestAPIAuthAndCRUD(t *testing.T) {
	dir := t.TempDir()
	store := bypass.NewStore(filepath.Join(dir, "rules.txt"))
	mgr := bypass.NewManager(bypass.Options{
		Store: store,
		LookupIP: func(ctx context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		},
	})
	fo := &fakeOutline{key: "ss://x@1.1.1.1:1", ready: true}
	srv := &Server{Manager: mgr, Outline: fo, Token: "secret", Static: StaticFS()}
	mux := http.NewServeMux()
	srv.Mount(mux)

	// unauthorized
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/bypass", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}

	// add
	body := bytes.NewBufferString(`{"rule":"example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bypass", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}

	// list
	req = httptest.NewRequest(http.MethodGet, "/api/v1/bypass", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	var list struct {
		Rules []struct {
			Rule string `json:"rule"`
			Kind string `json:"kind"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Rules) != 1 || list.Rules[0].Rule != "example.com" {
		t.Fatalf("%+v", list)
	}

	// outline status
	req = httptest.NewRequest(http.MethodGet, "/api/v1/outline", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("outline status: %d %s", rr.Code, rr.Body.String())
	}

	// replace key
	body = bytes.NewBufferString(`{"access_key":"ss://new@9.9.9.9:443"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/outline", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("outline put: %d %s", rr.Code, rr.Body.String())
	}
	if fo.key != "ss://new@9.9.9.9:443" {
		t.Fatalf("key not updated: %s", fo.key)
	}

	// UI static open
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("ui: %d", rr.Code)
	}
}

// ensure Manager interface is satisfied
var _ Manager = (*bypass.Manager)(nil)
