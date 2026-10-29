// Package podcasts lists RSS feeds from config/podcasts.ini and turns each
// episode's audio enclosure into a station (src/podcasts.pas).
package podcasts

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

const (
	FeedPrefix = "PF"
	Prefix     = "PC"
	maxBytes   = 4 * 1024 * 1024
)

type Episode struct{ ID, Title, URL, Description, Date string }
type Feed struct{ ID, Name, URL, Logo, Group string }

type cacheEntry struct {
	feedID   string
	episodes []Episode
	eol      time.Time
}

type Store struct {
	File      string // path of podcasts.ini
	Limit     int    // episodes per feed
	TTL       time.Duration
	UserAgent string
	Client    *http.Client

	mu    sync.Mutex
	feeds []Feed
	cache []cacheEntry
}

func ID(prefix, text string) string {
	sum := md5.Sum([]byte(text))
	return prefix + "_" + strings.ToUpper(hex.EncodeToString(sum[:])[:12])
}

func (s *Store) Feeds() []Feed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feeds
}

func (s *Store) FeedByID(id string) (Feed, bool) {
	for _, f := range s.Feeds() {
		if f.ID == id {
			return f, true
		}
	}
	return Feed{}, false
}

// Load reads podcasts.ini: every [section] is a group, every "Name=URL|Logo"
// line a feed. It clears the episode cache.
func (s *Store) Load() bool {
	if _, err := os.Stat(s.File); err != nil {
		log.Printf("Podcasts: file not found %s", s.File)
		return false
	}
	f, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true, AllowShadows: false, SkipUnrecognizableLines: true}, s.File)
	if err != nil {
		log.Printf("Podcasts: error %s (%v)", s.File, err)
		return false
	}
	var feeds []Feed
	for _, sec := range f.Sections() {
		if sec.Name() == ini.DefaultSection && len(sec.Keys()) == 0 {
			continue
		}
		for _, k := range sec.Keys() {
			name, val := strings.TrimSpace(k.Name()), strings.TrimSpace(k.Value())
			if name == "" || val == "" {
				continue
			}
			parts := strings.Split(val, "|")
			fd := Feed{Name: name, Group: sec.Name(), URL: strings.TrimSpace(parts[0])}
			if len(parts) > 1 {
				fd.Logo = strings.TrimSpace(parts[1])
			}
			fd.ID = ID(FeedPrefix, fd.URL)
			if fd.URL != "" {
				feeds = append(feeds, fd)
			}
		}
	}
	s.mu.Lock()
	s.feeds, s.cache = feeds, nil
	s.mu.Unlock()
	log.Printf("Podcasts: successfully loaded %d feed(s)", len(feeds))
	return len(feeds) > 0
}

type node struct {
	name     string
	attrs    map[string]string
	children []*node
	text     string // character data when this node is a text child
	isText   bool
}

func parseXML(b []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = true
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	root := &node{}
	cur := root
	var stack []*node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			cur.children = append(cur.children, n)
			stack = append(stack, cur)
			cur = n
		case xml.EndElement:
			cur = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) > 0 {
				cur.children = append(cur.children, &node{isText: true, text: string(t)})
			}
		}
	}
	if len(root.children) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return root.children[0], nil
}

func (n *node) find(name string) *node {
	for _, c := range n.children {
		if !c.isText && c.name == name {
			return c
		}
	}
	return nil
}

func nodeText(n *node, name string) string {
	if n == nil {
		return ""
	}
	c := n.find(name)
	if c != nil && len(c.children) > 0 && c.children[0].isText {
		return c.children[0].text
	}
	return ""
}

func shortDate(p string) string {
	parts := strings.Fields(strings.ReplaceAll(strings.TrimSpace(p), ",", " "))
	if len(parts) < 4 {
		return strings.TrimSpace(p)
	}
	if n, _ := strconv.Atoi(parts[0]); n > 0 {
		return strings.Join(parts[:3], " ")
	}
	return strings.Join(parts[1:4], " ")
}

func (s *Store) parseFeed(b []byte) []Episode {
	doc, err := parseXML(b)
	if err != nil {
		log.Printf("Podcasts: feed is not readable XML (%v)", err)
		return nil
	}
	ch := doc.find("channel")
	if ch == nil {
		return nil
	}
	var eps []Episode
	for _, it := range ch.children {
		if len(eps) >= s.Limit {
			break
		}
		if it.isText || it.name != "item" {
			continue
		}
		enc := it.find("enclosure")
		var u, typ string
		if enc != nil {
			u, typ = strings.TrimSpace(enc.attrs["url"]), strings.ToLower(enc.attrs["type"])
		}
		if u == "" || (typ != "" && !strings.HasPrefix(typ, "audio")) {
			continue
		}
		e := Episode{Title: strings.TrimSpace(nodeText(it, "title")), URL: u, Date: shortDate(nodeText(it, "pubDate"))}
		if e.Title == "" {
			e.Title = "Episode " + strconv.Itoa(len(eps)+1)
		}
		e.Description = e.Date
		e.ID = ID(Prefix, u)
		eps = append(eps, e)
	}
	return eps
}

func (s *Store) fetch(u string) []byte {
	c := s.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", s.UserAgent)
	resp, err := c.Do(req)
	if err != nil {
		log.Printf("Podcasts: getting error %s (%v)", u, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("Podcasts: getting error %s (unexpected status %d)", u, resp.StatusCode)
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if len(b) > maxBytes {
		log.Printf("Podcasts: getting error %s (feed larger than %d bytes)", u, maxBytes)
		return nil
	}
	return b
}

func (s *Store) cached(feedID string) ([]Episode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.cache {
		if c.feedID == feedID && c.eol.After(time.Now()) {
			return c.episodes, true
		}
	}
	return nil, false
}

func (s *Store) put(feedID string, eps []Episode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cache {
		if s.cache[i].feedID == feedID {
			s.cache[i].episodes, s.cache[i].eol = eps, time.Now().Add(s.TTL)
			return
		}
	}
	s.cache = append(s.cache, cacheEntry{feedID, eps, time.Now().Add(s.TTL)})
}

// Episodes of one feed, from the cache while it is fresh.
func (s *Store) Episodes(f Feed) ([]Episode, bool) {
	if e, ok := s.cached(f.ID); ok {
		return e, true
	}
	b := s.fetch(f.URL)
	if len(b) == 0 {
		return nil, false
	}
	eps := s.parseFeed(b)
	if len(eps) == 0 {
		log.Printf("Podcasts: no episodes in %s", f.Name)
		return nil, false
	}
	s.put(f.ID, eps)
	return eps, true
}

func (s *Store) EpisodeByID(id string) (Episode, bool) {
	s.mu.Lock()
	for _, c := range s.cache {
		for _, e := range c.episodes {
			if e.ID == id {
				s.mu.Unlock()
				return e, true
			}
		}
	}
	s.mu.Unlock()
	for _, f := range s.Feeds() {
		if eps, ok := s.Episodes(f); ok {
			for _, e := range eps {
				if e.ID == id {
					return e, true
				}
			}
		}
	}
	return Episode{}, false
}

func (s *Store) LogoForEpisode(id string) string {
	s.mu.Lock()
	feedID := ""
	for _, c := range s.cache {
		for _, e := range c.episodes {
			if e.ID == id {
				feedID = c.feedID
			}
		}
	}
	s.mu.Unlock()
	if f, ok := s.FeedByID(feedID); ok && feedID != "" {
		return f.Logo
	}
	return ""
}

var _ = filepath.Join
