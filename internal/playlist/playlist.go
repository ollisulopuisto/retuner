// Package playlist unwraps .m3u/.pls/.asx/.xspf links to the first stream URL
// (LooksLikePlaylist, FirstStreamURLInPlaylist, ResolveStreamURL in common.pas).
package playlist

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxBytes = 65536
	maxDepth = 2
)

var extensions = []string{".m3u", ".pls", ".asx", ".xspf"}

func Looks(u string) bool {
	p := u
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "#"); i >= 0 {
		p = p[:i]
	}
	p = strings.ToLower(p)
	for _, e := range extensions {
		if strings.HasSuffix(p, e) {
			return true
		}
	}
	return false
}

func isDelim(c byte) bool {
	return c == '"' || c == '\'' || c == '<' || c == '>' || c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// FirstStream is the first http(s) URL of a playlist body.
func FirstStream(body string) string {
	for _, line := range strings.FieldsFunc(body, func(r rune) bool { return r == '\n' || r == '\r' }) {
		c := strings.TrimSpace(line)
		if c == "" || strings.HasPrefix(c, "#") || strings.HasPrefix(c, ";") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(c), "file") && strings.Contains(c, "=") {
			c = strings.TrimSpace(c[strings.Index(c, "=")+1:])
		}
		if strings.HasPrefix(strings.ToLower(c), "http") {
			return c
		}
	}
	low := strings.ToLower(body)
	s := strings.Index(low, "http://")
	if s < 0 {
		s = strings.Index(low, "https://")
	}
	if s < 0 {
		return ""
	}
	e := s
	for e < len(body) && !isDelim(body[e]) {
		e++
	}
	return strings.TrimSpace(body[s:e])
}

type Resolver struct {
	UserAgent string
	Client    *http.Client

	clientOnce sync.Once
}

func (r *Resolver) getClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	r.clientOnce.Do(func() {
		r.Client = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: 10 * time.Second,
				MaxIdleConns:          50,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
			},
		}
	})
	return r.Client
}

func (r *Resolver) fetch(u string) string {
	c := r.getClient()
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", r.UserAgent)
	resp, err := c.Do(req)
	if err != nil {
		log.Printf("Playlist getting error %s (%v)", u, err)
		return ""
	}
	defer resp.Body.Close()
	// Like the Pascal client, stop receiving after the size cap and use what came.
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return string(b)
}

// Resolve follows playlists up to two levels and returns the first stream URL;
// the input comes back unchanged when nothing can be resolved.
func (r *Resolver) Resolve(u string) string {
	res := u
	for depth := 0; depth < maxDepth && Looks(res); depth++ {
		next := FirstStream(r.fetch(res))
		if next == "" || next == res {
			log.Printf("Playlist could not be resolved: %s", res)
			return res
		}
		log.Printf("Playlist resolved: %s -> %s", res, next)
		res = next
	}
	return res
}
