// Package maintenance is the shutdown endpoint (src/maintenance.pas): a listener
// that answers only the configured address and stops the service on
// /retuner/down. Anything else gets 404, other clients get 503.
package maintenance

import (
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const service = "Maintenance Service"

type Server struct {
	Address  string // only a client with exactly this address is served
	Shutdown func() // stops the service (called after the reply is sent)
	Root     string // "retuner"
	AppName  string
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != s.Address {
		log.Printf("%s: rejected %s from %s", service, r.URL.RequestURI(), host)
		body := "Service unavailable.\n"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, body)
		return
	}
	if strings.ToLower(r.URL.RequestURI()) == "/"+s.Root+"/down" {
		body := s.AppName + " service: Shutting down.....\n"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		io.WriteString(w, body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.Shutdown()
		}()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNotFound)
}

// Start listens on every interface (the address check is per connection, as in
// the release build) and logs the configured address.
func (s *Server) Start(port int) error {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	log.Printf("%s: listening on: %s:%d", service, s.Address, port)
	go func() { _ = http.Serve(ln, s) }()
	return nil
}
