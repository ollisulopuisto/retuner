// Package relay is the HTTPS-to-HTTP relay (src/relayserver.pas): receivers that
// cannot play https:// streams are given an http://host:port/relay?id=<station>
// URL, and this listener fetches the real stream and forwards the audio.
package relay

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"retuner/internal/playlist"
)

const (
	Path              = "/relay"
	service           = "Relay Service"
	requestMax        = 4096
	defaultType       = "audio/mpeg"
	reconnectAttempts = 5
	reconnectPause    = 2 * time.Second
	initialAttempts   = 2
	relayBufferSize   = 16 * 1024
)

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, relayBufferSize)
		return &b
	},
}

type Server struct {
	Resolve   func(id string) string // station id -> stream URL, "" when unknown
	Playlists bool                   // ResolvePlaylists
	Resolver  *playlist.Resolver
	UserAgent string // Retuner/<version>
	Pause     time.Duration
	// Transport overrides the upstream transport (tests only). Nil: the default
	// one, which verifies TLS certificates against the system roots.
	Transport http.RoundTripper
	ln        net.Listener

	clientOnce sync.Once
	client     *http.Client
}

func (s *Server) getClient() *http.Client {
	s.clientOnce.Do(func() {
		rt := s.Transport
		if rt == nil {
			rt = &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: 10 * time.Second,
				MaxIdleConns:          50,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
			}
		}
		s.client = &http.Client{Transport: rt}
	})
	return s.client
}

// NeedsRelay: only https URLs, and only when RelayHTTPS is on (callers check that).
func NeedsRelay(u string) bool { return strings.HasPrefix(strings.ToLower(u), "https://") }

func URLFor(host string, port int, id string) string {
	return "http://" + host + ":" + strconv.Itoa(port) + Path + "?id=" + id
}

// RequestedID pulls the id out of "GET /relay?id=X&.. HTTP/1.x".
func RequestedID(line string) string {
	f := strings.Split(line, " ")
	if len(f) < 2 || !strings.HasPrefix(f[1], Path) {
		return ""
	}
	i := strings.Index(f[1], "id=")
	if i < 0 {
		return ""
	}
	return strings.Split(f[1][i+3:], "&")[0]
}

func readLine(r *bufio.Reader, count *int) string {
	var b []byte
	for *count < requestMax {
		c, err := r.ReadByte()
		if err != nil {
			break
		}
		*count++
		if c == 10 {
			break
		}
		if c != 13 {
			b = append(b, c)
		}
	}
	return string(b)
}

func readRequest(r *bufio.Reader) (line string, wantIcy bool) {
	n := 0
	line = readLine(r, &n)
	for {
		l := readLine(r, &n)
		if strings.HasPrefix(strings.ToLower(l), "icy-metadata:") && strings.Contains(l, "1") {
			wantIcy = true
		}
		if l == "" || n >= requestMax {
			return
		}
	}
}

func (s *Server) status(c net.Conn, st string) {
	io.WriteString(c, "HTTP/1.0 "+st+"\r\nServer: "+s.UserAgent+"\r\nConnection: close\r\n\r\n")
}

// forward writes the downstream headers on the first audio byte, then the audio.
type forward struct {
	c           net.Conn
	resp        *http.Response
	wantIcy     bool
	server      string
	headersSent bool
	icyActive   bool
	downFailed  bool
	bytes       int64
}

func (f *forward) headers() {
	up := func(n string) string { return strings.TrimSpace(f.resp.Header.Get(n)) }
	ct := up("Content-Type")
	if ct == "" {
		ct = defaultType
	}
	h := "HTTP/1.0 200 OK\r\nServer: " + f.server + "\r\nContent-Type: " + ct + "\r\n"
	for _, n := range []string{"icy-name", "icy-genre", "icy-br"} {
		if v := up(n); v != "" {
			h += n + ": " + v + "\r\n"
		}
	}
	if mi := up("icy-metaint"); f.wantIcy && mi != "" {
		h += "icy-metaint: " + mi + "\r\n"
		f.icyActive = true
	}
	io.WriteString(f.c, h+"Connection: close\r\n\r\n")
}

func (f *forward) Write(p []byte) (int, error) {
	if !f.headersSent {
		f.headers()
		f.headersSent = true
	}
	n, err := f.c.Write(p)
	if err != nil {
		f.downFailed = true
		return n, err
	}
	f.bytes += int64(n)
	return n, nil
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("%s: panic recovered in serve: %v", service, r)
		}
	}()
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	line, wantIcy := readRequest(br)
	_ = c.SetReadDeadline(time.Time{})
	id := RequestedID(line)
	if id == "" || s.Resolve == nil {
		s.status(c, "404 Not Found")
		return
	}
	u := s.Resolve(id)
	if s.Playlists && playlist.Looks(u) {
		u = s.Resolver.Resolve(u)
	}
	if u == "" {
		log.Printf("%s: stations not found %s", service, id)
		s.status(c, "404 Not Found")
		return
	}
	log.Printf("%s: %s -> %s", service, id, u)
	f := &forward{c: c, wantIcy: wantIcy, server: s.UserAgent}
	pause := s.Pause
	if pause == 0 {
		pause = reconnectPause
	}
	attempts, limit := 0, initialAttempts
	client := s.getClient()
	bufPtr := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(bufPtr)
	buf := *bufPtr
	for {
		before := f.bytes
		req, err := http.NewRequest("GET", u, nil)
		if err == nil {
			req.Header.Set("User-Agent", s.UserAgent)
			if wantIcy {
				req.Header.Set("Icy-MetaData", "1")
			}
			var resp *http.Response
			if resp, err = client.Do(req); err == nil {
				if resp.StatusCode == 200 {
					// Validate icy-metaint header: if malformed (non-positive integer), ignore it
					if mi := resp.Header.Get("icy-metaint"); mi != "" {
						if n, perr := strconv.Atoi(strings.TrimSpace(mi)); perr != nil || n <= 0 {
							resp.Header.Del("icy-metaint")
						}
					}
					f.resp = resp
					_, err = io.CopyBuffer(f, resp.Body, buf)
				} else {
					err = io.ErrUnexpectedEOF
				}
				io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				f.resp = nil
			}
		}
		if err != nil {
			log.Printf("%s: upstream ended %s (%v)", service, id, err)
		}
		if f.bytes > before {
			attempts = 0
		} else {
			attempts++
		}
		if f.bytes > 0 {
			limit = reconnectAttempts
		} else {
			limit = initialAttempts
		}
		if f.downFailed {
			break
		}
		if f.icyActive {
			log.Printf("%s: not resuming a metadata stream %s", service, id)
			break
		}
		if attempts >= limit {
			log.Printf("%s: giving up on %s", service, id)
			break
		}
		time.Sleep(pause)
		log.Printf("%s: reconnecting %s", service, id)
	}
	if !f.headersSent {
		log.Printf("%s: no audio from %s -> %s", service, id, u)
		s.status(c, "502 Bad Gateway")
	}
}

// Start listens on ip:port (ip "" means every interface).
func (s *Server) Start(ip string, port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	s.ln = ln
	shown := ip
	if shown == "" {
		shown = "*"
	}
	log.Printf("%s: listening on: %s:%d", service, shown, port)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return nil
}

func (s *Server) Stop() {
	if s.ln != nil {
		s.ln.Close()
	}
}
