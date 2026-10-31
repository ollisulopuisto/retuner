package presets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCountryCodes(t *testing.T) {
	p := &Presets{Countries: "FI, se;fi  x1 swe d"}
	got := p.CountryCodes()
	if len(got) != 2 || got[0] != "fi" || got[1] != "se" {
		t.Fatalf("%v", got)
	}
}

func TestRubbishNeverReplacesGoodCache(t *testing.T) {
	good := "[A]\nOne=http://x/1\n"
	body := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	p := &Presets{Enabled: true, Countries: "fi", URL: srv.URL, Dir: filepath.Join(t.TempDir(), "presets")}
	if !p.Refresh() {
		t.Fatal("first fetch should work")
	}
	body = "<html>404</html>"
	p.Refresh()
	b, _ := os.ReadFile(filepath.Join(p.Dir, "fi.ini"))
	if string(b) != good {
		t.Fatalf("cache replaced by %q", b)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "fi.ini.tmp")); err == nil {
		t.Fatal("temp file left behind")
	}
}

func TestOversizedAnswerIsRefusedEvenIfPrefixValidates(t *testing.T) {
	good := "[A]\nOne=http://x/1\n"
	body := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	p := &Presets{Enabled: true, Countries: "fi", URL: srv.URL, Dir: filepath.Join(t.TempDir(), "presets")}
	if !p.Refresh() {
		t.Fatal("first fetch should work")
	}
	// A valid list that overflows the cap by one byte: the prefix would pass validation.
	pad := make([]byte, maxBytes+1-len("[B]\nTwo=http://x/2\n"))
	for i := range pad {
		pad[i] = ';'
	}
	body = "[B]\nTwo=http://x/2\n" + string(pad)
	p.Refresh()
	b, _ := os.ReadFile(filepath.Join(p.Dir, "fi.ini"))
	if string(b) != good {
		t.Fatalf("an oversized answer replaced the good cache (%d bytes)", len(b))
	}
	// Exactly at the cap is accepted.
	body = "[B]\nTwo=http://x/2\n" + string(pad[:len(pad)-1])
	p.Refresh()
	b, _ = os.ReadFile(filepath.Join(p.Dir, "fi.ini"))
	if len(b) != maxBytes {
		t.Fatalf("a list exactly at the cap should be accepted, got %d bytes", len(b))
	}
}
