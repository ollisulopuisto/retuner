package dnsserver

import (
	"net"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

func serve(t *testing.T, h mdns.Handler) (addr string, stop func()) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &mdns.Server{PacketConn: pc, Handler: h}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	return pc.LocalAddr().String(), func() { _ = srv.Shutdown() }
}

func ask(t *testing.T, addr, name string, qt uint16) *mdns.Msg {
	m := new(mdns.Msg)
	m.SetQuestion(mdns.Fqdn(name), qt)
	c := &mdns.Client{Timeout: 2 * time.Second}
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return r
}

func TestInterceptMatching(t *testing.T) {
	cases := []struct {
		q, p string
		want bool
	}{
		{"radio.vtuner.com", "*.vtuner.com", true}, {"vtuner.com", "*.vtuner.com", true}, {"VTUNER.COM", "*.vtuner.com", true},
		{"notvtuner.com", "*.vtuner.com", false}, {"example.org", "*.vtuner.com", false},
		{"Exact.Example", "exact.example", true}, {"a.exact.example", "exact.example", false},
	}
	for _, c := range cases {
		if got := Matches(c.q, c.p); got != c.want {
			t.Errorf("Matches(%q,%q)=%v", c.q, c.p, got)
		}
	}
}

// upstream stands in for 8.8.8.8: it answers every A query with 9.9.9.9.
func upstream(t *testing.T) (port string, stop func()) {
	addr, stop := serve(t, mdns.HandlerFunc(func(w mdns.ResponseWriter, r *mdns.Msg) {
		m := new(mdns.Msg)
		m.SetReply(r)
		m.Answer = []mdns.RR{&mdns.A{Hdr: mdns.RR_Header{Name: r.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60}, A: net.ParseIP("9.9.9.9")}}
		_ = w.WriteMsg(m)
	}))
	_, p, _ := net.SplitHostPort(addr)
	return p, stop
}

func TestForwardsNonInterceptedNames(t *testing.T) {
	up, stopUp := upstream(t)
	defer stopUp()
	s := New("*.vtuner.com", "127.0.0.1", "10.1.2.3", false, "")
	s.UpstreamPort = up
	addr, stop := serve(t, mdns.HandlerFunc(s.Handle))
	defer stop()
	if r := ask(t, addr, "example.org", mdns.TypeA); len(r.Answer) != 1 || r.Answer[0].(*mdns.A).A.String() != "9.9.9.9" {
		t.Fatalf("forwarded answer: %v", r.Answer)
	}
	if r := ask(t, addr, "radio.vtuner.com", mdns.TypeA); len(r.Answer) != 1 || r.Answer[0].(*mdns.A).A.String() != "10.1.2.3" {
		t.Fatalf("intercepted answer: %v", r.Answer)
	}
}

func TestRestrictForwardingRefusesUnknownClients(t *testing.T) {
	up, stopUp := upstream(t)
	defer stopUp()
	s := New("*.vtuner.com", "127.0.0.1", "10.1.2.3", true, "")
	s.UpstreamPort = up
	addr, stop := serve(t, mdns.HandlerFunc(s.Handle))
	defer stop()
	if r := ask(t, addr, "example.org", mdns.TypeA); r.Rcode != mdns.RcodeRefused || len(r.Answer) != 0 {
		t.Fatalf("unknown client should be refused, got rcode %d answers %d", r.Rcode, len(r.Answer))
	}
	// Intercepted names are always answered: that is the point of the service.
	if r := ask(t, addr, "radio.vtuner.com", mdns.TypeA); len(r.Answer) != 1 {
		t.Fatal("intercept must still work for an unknown client")
	}
	// Reaching the web side (loginxml) makes the address a known client.
	s.NoteWebClient("127.0.0.1")
	if r := ask(t, addr, "example.org", mdns.TypeA); r.Rcode != mdns.RcodeSuccess || len(r.Answer) != 1 {
		t.Fatalf("a known client should be forwarded, got rcode %d", r.Rcode)
	}
}

func TestKnownClientExpiresAndAllowListWorks(t *testing.T) {
	s := New("", "", "", true, " 192.0.2.7 , 192.0.2.8")
	if !s.clientAllowed("192.0.2.8") || s.clientAllowed("192.0.2.9") {
		t.Fatal("allow list")
	}
	s.mu.Lock()
	s.known["198.51.100.1"] = time.Now().Add(-25 * time.Hour)
	s.known["198.51.100.2"] = time.Now().Add(-23 * time.Hour)
	s.mu.Unlock()
	if s.clientAllowed("198.51.100.1") || !s.clientAllowed("198.51.100.2") {
		t.Fatal("24 hour expiry")
	}
}

func TestUpstreamDownGivesNotImplemented(t *testing.T) {
	s := New("", "127.0.0.1", "", false, "")
	s.UpstreamPort = "1" // nothing listens
	addr, stop := serve(t, mdns.HandlerFunc(s.Handle))
	defer stop()
	if r := ask(t, addr, "example.org", mdns.TypeA); r.Rcode != mdns.RcodeNotImplemented {
		t.Fatalf("rcode %d", r.Rcode)
	}
}
