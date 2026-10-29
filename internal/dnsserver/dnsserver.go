// Package dnsserver answers the receiver's DNS lookups for the vendor portals
// it must reach Retuner through (src/dnsserver.pas). Names matching InterceptDNs
// get Retuner's own address; everything else is forwarded to DNSServers, unless
// RestrictForwarding is on and the client has not been seen on the web side.
package dnsserver

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

const clientTTL = 24 * time.Hour

type Server struct {
	Patterns           []string
	Upstream           []string
	AdvertiseIP        string // empty: the address the query arrived on
	RestrictForwarding bool
	AllowedClients     []string

	mu    sync.Mutex
	known map[string]time.Time
	conns []*mdns.Server
}

func New(intercept, upstream, advertise string, restrict bool, allowed string) *Server {
	return &Server{
		Patterns: splitList(intercept), Upstream: splitList(upstream), AdvertiseIP: strings.TrimSpace(advertise),
		RestrictForwarding: restrict, AllowedClients: splitList(allowed), known: map[string]time.Time{},
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NoteWebClient records an address that reached the web service, which is what
// lets a public host forward that device's other lookups without an allow-list.
func (s *Server) NoteWebClient(addr string) {
	if strings.TrimSpace(addr) == "" {
		return
	}
	s.mu.Lock()
	s.known[addr] = time.Now()
	s.mu.Unlock()
}

func (s *Server) clientAllowed(addr string) bool {
	for _, a := range s.AllowedClients {
		if a == addr {
			return true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.known[addr]
	return ok && time.Since(t) < clientTTL
}

// Matches mirrors InterceptMatches: "*.x.com" covers subdomains and the apex,
// anything else is an exact, case-insensitive match.
func Matches(query, pattern string) bool {
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(strings.ToLower(query), strings.ToLower(suffix)) ||
			strings.EqualFold(query, strings.TrimLeft(suffix, "."))
	}
	return strings.EqualFold(query, pattern)
}

func (s *Server) intercepted(name string) bool {
	for _, p := range s.Patterns {
		if Matches(name, p) {
			return true
		}
	}
	return false
}

func safeName(n string) string {
	var b strings.Builder
	for _, r := range []byte(n) {
		if r >= 32 && r <= 126 {
			b.WriteByte(r)
		} else {
			b.WriteString("\\x" + strings.ToUpper(string("0123456789abcdef"[r>>4])+string("0123456789abcdef"[r&15])))
		}
	}
	out := b.String()
	if len(out) > 300 {
		out = out[:300] + "..."
	}
	return out
}

// Handle is the dns.Handler. Anything it cannot parse is dropped silently, as a
// resolver does - the fuzz suite's point is that one bad packet never stops
// the next good one being answered.
func (s *Server) Handle(w mdns.ResponseWriter, req *mdns.Msg) {
	defer func() { _ = recover() }()
	if len(req.Question) == 0 {
		return
	}
	q := req.Question[0]
	name := strings.TrimSuffix(q.Name, ".")
	peer, _, _ := net.SplitHostPort(w.RemoteAddr().String())
	local, _, _ := net.SplitHostPort(w.LocalAddr().String())

	if s.intercepted(name) {
		// Only the plain case is answered: standard query, one question, A/IN.
		if req.Opcode == mdns.OpcodeQuery && len(req.Question) == 1 && q.Qtype == mdns.TypeA && q.Qclass == mdns.ClassINET {
			log.Printf("DNS Query intercept : %s", safeName(name))
			ip := s.AdvertiseIP
			if ip == "" {
				ip = local
			}
			parsed := net.ParseIP(ip).To4()
			if parsed == nil {
				return
			}
			m := new(mdns.Msg)
			m.SetReply(req)
			m.Compress = true // the Pascal answer points back at the question name (c00c)
			m.RecursionAvailable = true
			m.Answer = []mdns.RR{&mdns.A{Hdr: mdns.RR_Header{Name: q.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 3600}, A: parsed}}
			_ = w.WriteMsg(m)
			return
		}
		// Intercepted name, other type (AAAA and friends): the Pascal build hands the
		// query back unchanged ("NA"), so the packet goes out still marked as a
		// query and the resolver treats it as no answer. Reproduced byte for byte
		// (checked against the 26.08.28.4 binary); answering NODATA instead would
		// be kinder to resolvers - an open question for the owner.
		if raw, err := req.Pack(); err == nil {
			_, _ = w.Write(raw)
		}
		return
	}
	log.Printf("DNS query not intercepted: %s", safeName(name))

	if s.RestrictForwarding && !s.clientAllowed(peer) {
		log.Printf("DNS forwarding refused for %s (%s)", peer, safeName(name))
		m := new(mdns.Msg)
		m.SetRcode(req, mdns.RcodeRefused)
		m.RecursionAvailable = true // flags 0x8185 as in the Pascal refusal
		_ = w.WriteMsg(m)
		return
	}
	c := &mdns.Client{Timeout: 3 * time.Second}
	for _, up := range s.Upstream {
		r, _, err := c.Exchange(req, net.JoinHostPort(up, "53"))
		if err == nil && r != nil {
			_ = w.WriteMsg(r)
			return
		}
	}
	// Nothing to forward to (DNSServers empty) or nobody answered: what the
	// Pascal build puts on the wire is flags 0x8104, rcode 4 "not implemented".
	m := new(mdns.Msg)
	m.SetRcode(req, mdns.RcodeNotImplemented)
	m.RecursionAvailable = false
	_ = w.WriteMsg(m)
}

// Start listens on UDP (and TCP, which Indy's server did not offer; harmless
// and needed for large answers from upstream). It returns once listening failed
// or the server stopped.
func (s *Server) Start(ip string, port int) error {
	addr := net.JoinHostPort(ip, itoa(port))
	srv := &mdns.Server{Addr: addr, Net: "udp", Handler: mdns.HandlerFunc(s.Handle)}
	s.mu.Lock()
	s.conns = append(s.conns, srv)
	s.mu.Unlock()
	return srv.ListenAndServe()
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Shutdown()
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	d := ""
	for n > 0 {
		d = string(rune('0'+n%10)) + d
		n /= 10
	}
	return d
}
