// Package bookmark is the receiver's favourites list (src/bookmark.pas),
// kept in config/bookmark.xml (CommonBookmark=1) or config/<mac>.xml, in the
// same XML the Pascal build writes so an existing install keeps its favourites.
package bookmark

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	Host       = "retunerhost" // stands in for the server address inside a stored file
	HostLegacy = "ytunerhost"
)

type Item struct {
	ID, Name, URL, Desc, Logo, Format, Location, Bandwidth, Mime, Relia, Bookmark string
	// Fresh marks an item built in memory rather than read from the file. FPC's
	// DOM writer prints an empty parsed element as <X/> and an empty created one
	// as <X></X>, so a file mixes both after an add; match it to keep files identical.
	Fresh bool
}

type file struct {
	Count int
	Items []Item
}

type Store struct {
	Dir     string
	Common  bool
	Limit   int
	URLHost string
	mu      sync.Mutex
}

var safeMAC = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// path names the favourites file. A MAC is only trusted as a file name when it
// is plain; the Pascal build used it unchecked, so "../x" wrote outside config/.
// filepath.IsLocal additionally rejects absolute paths and any ".." element, so
// the file name handed to filepath.Join is provably a single local component.
func (s *Store) path(mac string) string {
	fallback := filepath.Join(s.Dir, "bookmark.xml")
	if !s.Common && safeMAC.MatchString(mac) {
		name := mac + ".xml"
		if filepath.IsLocal(name) {
			candidate := filepath.Join(s.Dir, name)
			baseAbs, err1 := filepath.Abs(s.Dir)
			candidateAbs, err2 := filepath.Abs(candidate)
			if err1 == nil && err2 == nil {
				rel, err := filepath.Rel(baseAbs, candidateAbs)
				if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
					return candidateAbs
				}
			}
		}
	}
	return fallback
}

type xmlItem struct {
	Type      string `xml:"ItemType"`
	ID        string `xml:"StationId"`
	Name      string `xml:"StationName"`
	URL       string `xml:"StationUrl"`
	Desc      string `xml:"StationDesc"`
	Logo      string `xml:"Logo"`
	Format    string `xml:"StationFormat"`
	Location  string `xml:"StationLocation"`
	Bandwidth string `xml:"StationBandWidth"`
	Mime      string `xml:"StationMime"`
	Relia     string `xml:"Relia"`
	Bookmark  string `xml:"Bookmark"`
}

type xmlFile struct {
	XMLName xml.Name  `xml:"ListOfItems"`
	Count   string    `xml:"ItemCount"`
	Items   []xmlItem `xml:"Item"`
}

func (s *Store) read(mac string) (*file, bool, error) {
	data, err := os.ReadFile(s.path(mac))
	if err != nil {
		return nil, false, nil
	}
	var x xmlFile
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, true, err
	}
	f := &file{}
	f.Count, _ = strconv.Atoi(strings.TrimSpace(x.Count))
	for _, i := range x.Items {
		f.Items = append(f.Items, Item{i.ID, i.Name, i.URL, i.Desc, i.Logo, i.Format, i.Location, i.Bandwidth, i.Mime, i.Relia, i.Bookmark, false})
	}
	return f, true, nil
}

// xmlEscaper replaces XML entities matching FPC's WriteXML escaping behavior.
// Reusing a single replacer instance avoids allocating a new replacer trie on every field.
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func esc(s string) string {
	return xmlEscaper.Replace(s)
}

func el(b *bytes.Buffer, name, val string, selfClose bool) {
	if selfClose && val == "" {
		b.WriteString("    <" + name + "/>\n")
		return
	}
	b.WriteString("    <" + name + ">" + esc(val) + "</" + name + ">\n")
}

func render(count int, items []Item) []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<ListOfItems>\n  <ItemCount>" + strconv.Itoa(count) + "</ItemCount>\n")
	for _, i := range items {
		selfClose := !i.Fresh
		b.WriteString("  <Item>\n")
		el(&b, "ItemType", "Station", selfClose)
		el(&b, "StationId", i.ID, selfClose)
		el(&b, "StationName", i.Name, selfClose)
		el(&b, "StationUrl", i.URL, selfClose)
		el(&b, "StationDesc", i.Desc, selfClose)
		el(&b, "Logo", i.Logo, selfClose)
		el(&b, "StationFormat", i.Format, selfClose)
		el(&b, "StationLocation", i.Location, selfClose)
		el(&b, "StationBandWidth", i.Bandwidth, selfClose)
		el(&b, "StationMime", i.Mime, selfClose)
		el(&b, "Relia", i.Relia, selfClose)
		el(&b, "Bookmark", i.Bookmark, selfClose)
		b.WriteString("  </Item>\n")
	}
	b.WriteString("</ListOfItems>\n")
	return b.Bytes()
}

var hostRe = regexp.MustCompile(`(?i)` + Host)
var hostLegacyRe = regexp.MustCompile(`(?i)` + HostLegacy)

func (s *Store) subst(v string) string {
	v = hostRe.ReplaceAllLiteralString(v, s.URLHost)
	return hostLegacyRe.ReplaceAllLiteralString(v, s.URLHost)
}

// Count is the ItemCount stored in the file (what the main menu shows), not the
// number of items actually present.
func (s *Store) Count(mac string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, _, err := s.read(mac)
	if err != nil || f == nil {
		return 0
	}
	return f.Count
}

// Page renders items first..last (zero-based, inclusive). ok is false when
// there is no file or it cannot be read - the handler answers 404 then.
func (s *Store) Page(mac string, first, last int, nameFn func(string) string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, exists, err := s.read(mac)
	if !exists || err != nil || f == nil {
		return nil, false
	}
	var out []Item
	for i := first; i <= last; i++ {
		if i < 0 || i >= len(f.Items) {
			return nil, false // the Pascal build raises on a child that is not there and answers 404
		}
		it := f.Items[i]
		it.Logo, it.Bookmark = s.subst(it.Logo), s.subst(it.Bookmark)
		if nameFn != nil && it.Name != "" {
			it.Name = nameFn(it.Name)
		}
		out = append(out, it)
	}
	// ItemCount in the page is the file's count, as in Pascal (child 0 is imported whole).
	return render(f.Count, out), true
}

// StationInfo renders one stored station by its (B-form) id. A missing file is
// a 404; a file without that id answers 200 with an empty body.
func (s *Store) StationInfo(mac, id string) (body []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, exists, err := s.read(mac)
	if !exists || err != nil || f == nil {
		return nil, false
	}
	for _, it := range f.Items {
		if it.ID == id {
			it.Logo, it.Bookmark = s.subst(it.Logo), s.subst(it.Bookmark)
			return render(1, []Item{it}), true
		}
	}
	return nil, true
}

// Set adds or removes a station. it.ID must already be in the stored "B" form.
func (s *Store) Set(mac, action string, it Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	safeMac := mac
	if s.Common || !safeMAC.MatchString(safeMac) {
		safeMac = "bookmark"
	}
	f, exists, err := s.read(safeMac)
	if err != nil {
		return
	}
	if !exists {
		if action != "add" {
			return
		}
		f = &file{}
	}
	idx := -1
	for i, e := range f.Items {
		if e.ID == it.ID {
			idx = i
			break
		}
	}
	p := s.path(safeMac)
	// A file created from scratch keeps <X></X> for empty values; once it has
	// been read back and rewritten, the Pascal DOM writer emits <X/> for every
	// empty element, old entries included. Match both so files stay identical.
	switch action {
	case "add":
		if idx < 0 && f.Count < s.Limit {
			it.Bookmark = strings.NewReplacer("fav=add", "fav=del", "FAV=ADD", "fav=del").Replace(it.Bookmark)
			it.Logo = replaceFold(it.Logo, s.URLHost, Host)
			it.Bookmark = replaceFold(it.Bookmark, s.URLHost, Host)
			it.Fresh = true
			f.Items = append(f.Items, it)
			f.Count++
			_ = os.WriteFile(p, render(f.Count, f.Items), 0o644)
		}
	case "del":
		if idx >= 0 {
			if f.Count > 1 {
				f.Items = append(f.Items[:idx], f.Items[idx+1:]...)
				f.Count--
				_ = os.WriteFile(p, render(f.Count, f.Items), 0o644)
			} else {
				_ = os.Remove(p)
			}
		}
	}
}

func replaceFold(s, old, new string) string {
	if old == "" {
		return s
	}
	return regexp.MustCompile(`(?i)`+regexp.QuoteMeta(old)).ReplaceAllLiteralString(s, new)
}

// HasFile reports whether <dev>.xml exists in the config folder, which is how a
// "play" search decides whether the device has bookmarks (FileExists in the
// Pascal build). Names that are not plain file names never match.
func (s *Store) HasFile(dev string) bool {
	if !safeMAC.MatchString(dev) {
		return false
	}
	_, err := os.Stat(filepath.Join(s.Dir, dev+".xml"))
	return err == nil
}
