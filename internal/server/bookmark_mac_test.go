package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"retuner/internal/bookmark"
	"retuner/internal/config"
	"retuner/internal/stations"
)

func newTestServer(t *testing.T) *Server {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "retuner.ini")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	avr, err := cfg.LoadAVR()
	if err != nil {
		t.Fatalf("cfg.LoadAVR failed: %v", err)
	}
	store := &stations.Store{}
	bm := &bookmark.Store{Dir: filepath.Join(dir, "config"), Common: false, Limit: 10, URLHost: "localhost"}
	return New(cfg, avr, "localhost", store, bm, nil, nil)
}

func TestBookmarkMACValidation(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	// 1. bookmarkList endpoint: /retuner/bookmark
	// Valid MAC -> should not return 400 Bad Request
	req := httptest.NewRequest("GET", "/retuner/bookmark?mac=001122334455", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code == http.StatusBadRequest {
		t.Errorf("expected valid MAC to be accepted, got %d", w.Code)
	}

	// Path traversal attempt in bookmarkList -> must return 400 Bad Request
	req = httptest.NewRequest("GET", "/retuner/bookmark?mac=../../etc/passwd", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected invalid MAC with path traversal to be rejected with 400, got %d", w.Code)
	}

	// 2. station endpoint for bookmark ID (starts with MSB): /setupapp/stat/statxml.asp
	req = httptest.NewRequest("GET", "/setupapp/foo/statxml.asp?id=MSB123456789012&mac=../../etc/passwd", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected invalid MAC in statxml bookmark lookup to return 400, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/setupapp/foo/statxml.asp?id=MSB123456789012&mac=001122334455", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code == http.StatusBadRequest {
		t.Errorf("expected valid MAC in statxml bookmark lookup to not return 400, got %d", w.Code)
	}
}
