package dns

import (
	"fmt"
	"strings"
	"github.com/miekg/dns"
)

type Server struct {
	InterceptDNs []string
	AdvertiseIP  string
	Upstream     string
}

func NewServer(interceptDNs []string, advertiseIP string) *Server {
	return &Server{
		InterceptDNs: interceptDNs,
		AdvertiseIP:  advertiseIP,
		Upstream:     "8.8.8.8:53",
	}
}

func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true

	for _, q := range r.Question {
		name := strings.TrimRight(q.Name, ".")
		if s.shouldIntercept(name) {
			if q.Qtype == dns.TypeA {
				ip := s.AdvertiseIP
				if ip == "" {
					ip = "127.0.0.1" // Fallback if not configured
				}
				rr, err := dns.NewRR(fmt.Sprintf("%s A %s", q.Name, ip))
				if err == nil {
					msg.Answer = append(msg.Answer, rr)
				}
			}
			w.WriteMsg(&msg)
			return
		}
	}

	c := new(dns.Client)
	in, _, err := c.Exchange(r, s.Upstream)
	if err == nil {
		w.WriteMsg(in)
		return
	}

	msg.SetRcode(r, dns.RcodeRefused)
	w.WriteMsg(&msg)
}

func (s *Server) shouldIntercept(domain string) bool {
	for _, pattern := range s.InterceptDNs {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*.")
			if strings.HasSuffix(strings.ToLower(domain), "." + strings.ToLower(suffix)) || strings.EqualFold(domain, suffix) {
				return true
			}
		} else if strings.EqualFold(pattern, domain) {
			return true
		}
	}
	return false
}

func (s *Server) Start(port int) error {
	server := &dns.Server{Addr: fmt.Sprintf(":%d", port), Net: "udp"}
	dns.HandleFunc(".", s.ServeDNS)
	return server.ListenAndServe()
}
