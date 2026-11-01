package relay

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestedID(t *testing.T) {
	for in, want := range map[string]string{
		"GET /relay?id=MS_ABC HTTP/1.1":        "MS_ABC",
		"GET /relay?x=1&id=RB_1&y=2 HTTP/1.0":  "RB_1",
		"GET /relay HTTP/1.1":                  "",
		"GET /other?id=MS_A HTTP/1.1":          "",
		"GET":                                  "",
		"GET /relay?id= HTTP/1.1":              "",
		"POST /relay?id=MS_X&id=MS_Y HTTP/1.1": "MS_X",
	} {
		if got := RequestedID(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestNeedsRelay(t *testing.T) {
	if !NeedsRelay("HTTPS://a/b") || NeedsRelay("http://a/b") || NeedsRelay("ftp://a") {
		t.Error("only https needs the relay")
	}
}

func relayOnce(t *testing.T, s *Server) string {
	t.Helper()
	c1, c2 := net.Pipe()
	go s.serve(c2)
	_, _ = c1.Write([]byte("GET /relay?id=X HTTP/1.0\r\n\r\n"))
	_ = c1.SetReadDeadline(time.Now().Add(10 * time.Second))
	b, _ := io.ReadAll(c1)
	return string(b)
}

// An https upstream is relayed as plain http. The test CA is trusted through
// Transport; the default transport must refuse the same server (self-signed),
// answering 502 rather than falling back to an unverified connection.
func TestHTTPSUpstream(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) > 1 { // the relay reconnects after the stream ends; refuse so it gives up
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("AUDIOBYTES"))
	}))
	defer up.Close()
	resolve := func(string) string { return up.URL }

	good := &Server{Resolve: resolve, UserAgent: "Retuner/test", Pause: time.Millisecond, Transport: up.Client().Transport}
	out := relayOnce(t, good)
	if !strings.HasPrefix(out, "HTTP/1.0 200 OK") || !strings.Contains(out, "Content-Type: audio/mpeg") || !strings.HasSuffix(out, "AUDIOBYTES") {
		t.Fatalf("trusted https upstream: %q", out)
	}

	hits.Store(-100) // the untrusted server must never be reached successfully
	bad := &Server{Resolve: resolve, UserAgent: "Retuner/test", Pause: time.Millisecond}
	out = relayOnce(t, bad)
	if !strings.Contains(out, "502") || strings.Contains(out, "AUDIOBYTES") {
		t.Fatalf("untrusted certificate must give 502 and no audio: %q", out)
	}
}

// TestBadStreamsWonKillServer verifies that 500s, abrupt EOFs, malformed headers,
// or resolver panics are handled gracefully without taking down the server.
func TestBadStreamsWontKillServer(t *testing.T) {
	// 1. Upstream returning 500 Internal Server Error immediately
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv500.Close()

	s := &Server{
		Resolve:   func(string) string { return srv500.URL },
		UserAgent: "Retuner/test",
		Pause:     time.Millisecond,
		Transport: srv500.Client().Transport,
	}
	out := relayOnce(t, s)
	if !strings.Contains(out, "502 Bad Gateway") {
		t.Fatalf("expected 502 Bad Gateway for 500 error upstream, got: %q", out)
	}

	// 2. Upstream returning malformed icy-metaint header (letters or negative number)
	srvBadIcy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", "invalid-number")
		_, _ = w.Write([]byte("AUDIO"))
	}))
	defer srvBadIcy.Close()

	sBadIcy := &Server{
		Resolve:   func(string) string { return srvBadIcy.URL },
		UserAgent: "Retuner/test",
		Pause:     time.Millisecond,
		Transport: srvBadIcy.Client().Transport,
	}
	c1, c2 := net.Pipe()
	go sBadIcy.serve(c2)
	_, _ = c1.Write([]byte("GET /relay?id=X HTTP/1.0\r\nIcy-MetaData: 1\r\n\r\n"))
	_ = c1.SetReadDeadline(time.Now().Add(5 * time.Second))
	b, _ := io.ReadAll(c1)
	out = string(b)
	if strings.Contains(out, "icy-metaint") {
		t.Fatalf("invalid icy-metaint header should have been dropped: %q", out)
	}
	if !strings.Contains(out, "HTTP/1.0 200 OK") || !strings.Contains(out, "AUDIO") {
		t.Fatalf("expected audio despite invalid icy-metaint: %q", out)
	}

	// 3. Resolver panicking
	sPanic := &Server{
		Resolve: func(string) string {
			panic("simulated resolve panic")
		},
		UserAgent: "Retuner/test",
	}
	c1, c2 = net.Pipe()
	go sPanic.serve(c2)
	_, _ = c1.Write([]byte("GET /relay?id=PANIC HTTP/1.0\r\n\r\n"))
	_ = c1.SetReadDeadline(time.Now().Add(time.Second))
	b, _ = io.ReadAll(c1) // should close cleanly without crashing the test
}
