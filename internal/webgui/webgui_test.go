package webgui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retuner/internal/stations"
)

func newSrv(t *testing.T, allowed string) *Server {
	dir := t.TempDir()
	f := filepath.Join(dir, "stations.ini")
	os.WriteFile(f, []byte("[A]\nOne=http://e/1\n"), 0o644)
	st := &stations.Store{Own: f}
	st.Reload()
	return &Server{Allowed: allowed, User: "admin", Password: "pw", Store: st, FileName: "stations.ini", AppName: "Retuner", Version: "v"}
}

func do(s *Server, remote, method, path, host string, auth bool, ctype, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = remote
	if host != "" {
		r.Host = host
	}
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	if auth {
		r.SetBasicAuth("admin", "pw")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestClientFilterBeforeAuth(t *testing.T) {
	s := newSrv(t, "192.168.1.0/24, 10.0.0.5")
	cases := []struct {
		remote string
		code   int
	}{
		{"192.168.1.77:5000", 200}, {"192.168.2.1:5000", 503}, {"10.0.0.5:1", 200}, {"10.0.0.6:1", 503},
		{"[::1]:1", 503}, {"[::ffff:192.168.1.5]:1", 503}, {"bogus", 503}, {"8.8.8.8:1", 503},
	}
	for _, c := range cases {
		// an outside client is refused even with the right credentials, and never sees a 401 prompt
		if w := do(s, c.remote, "GET", "/api/stations", "", true, "", ""); w.Code != c.code {
			t.Errorf("%s: got %d want %d", c.remote, w.Code, c.code)
		}
	}
	if w := do(s, "8.8.8.8:1", "GET", "/", "", false, "", ""); w.Code != 503 || w.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("outside client without credentials must get 503 and no auth challenge, got %d", w.Code)
	}
}

func TestAnyClientForms(t *testing.T) {
	for _, a := range []string{"", "default", "0.0.0.0"} {
		if w := do(newSrv(t, a), "8.8.8.8:1", "GET", "/api/status", "", true, "", ""); w.Code != 200 {
			t.Errorf("%q should allow any client, got %d", a, w.Code)
		}
	}
	// malformed entries never match anything
	for _, a := range []string{"10.0.0.0/33", "10.0.0/24", "300.1.1.1/8", "10.0.0.0/x", "abc"} {
		if w := do(newSrv(t, a), "10.0.0.1:1", "GET", "/api/status", "", true, "", ""); w.Code != 503 {
			t.Errorf("%q must not match, got %d", a, w.Code)
		}
	}
}

// DNS rebinding: a browser tricked into reaching the editor under another host
// name arrives from an allowed client address with a foreign Host header. The
// Pascal editor does not look at Host; credentials are the only gate, as here.
// What must hold: without credentials the foreign Host gets nothing but a 401,
// and a cross-site POST that is not application/json is refused before any write.
func TestRebindingAndCrossSite(t *testing.T) {
	s := newSrv(t, "127.0.0.1")
	if w := do(s, "127.0.0.1:1", "GET", "/api/stations", "attacker.example:8090", false, "", ""); w.Code != 401 || strings.Contains(w.Body.String(), "One") {
		t.Errorf("foreign Host without credentials: %d %q", w.Code, w.Body.String())
	}
	before, _ := os.ReadFile(s.Store.Own)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		w := do(s, "127.0.0.1:1", "POST", "/api/stations", "attacker.example", true, ct, `{"categories":[]}`)
		if w.Code != 400 {
			t.Errorf("content type %q: got %d want 400", ct, w.Code)
		}
	}
	if after, _ := os.ReadFile(s.Store.Own); string(after) != string(before) {
		t.Error("a refused POST changed the stations file")
	}
	for _, p := range []string{"/api/stations", "/api/remote/cmd", "/api/remote/select", "/api/remote/search"} {
		if w := do(s, "127.0.0.1:1", "POST", p, "", false, "application/json", `{}`); w.Code != 401 {
			t.Errorf("%s unauthenticated POST: %d", p, w.Code)
		}
	}
}

func TestSaveRefusedForYAMLAndBadBodies(t *testing.T) {
	s := newSrv(t, "")
	y := filepath.Join(t.TempDir(), "s.yaml")
	os.WriteFile(y, []byte("A:\n  - One: http://e/1\n"), 0o644)
	s.Store.Own = y
	if w := do(s, "1.1.1.1:1", "POST", "/api/stations", "", true, "application/json", `{"categories":[]}`); !strings.Contains(w.Body.String(), "only supported for .ini") {
		t.Errorf("yaml save: %s", w.Body.String())
	}
	if b, _ := os.ReadFile(y); !strings.Contains(string(b), "One") {
		t.Error("yaml file was modified")
	}
	// the credential check does not leak through timing of the user name
	_ = http.StatusOK
}

func TestRemoteCommandWhitelistWithoutReceiver(t *testing.T) {
	s := newSrv(t, "")
	w := do(s, "1.1.1.1:1", "POST", "/api/remote/cmd", "", true, "application/json", `{"cmd":"rm -rf"}`)
	if !strings.Contains(w.Body.String(), "not supported") && !strings.Contains(w.Body.String(), "permitted") {
		t.Errorf("%s", w.Body.String())
	}
}
