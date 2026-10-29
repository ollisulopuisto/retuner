// Package stations is the user's own station list (src/my_stations.pas): the
// stations.ini / .yaml parsers, the ID scheme and the merge rules.
package stations

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const Prefix = "MS"

type Station struct{ ID, Name, URL, Logo string }

type Category struct {
	Name     string
	Stations []Station
}

type Group []Category

// makeID is MS_ + first 12 hex of md5(name+url), upper-cased, as the Pascal build.
func makeID(name, url string) string {
	sum := md5.Sum([]byte(name + url))
	return strings.ToUpper(Prefix + "_" + hex.EncodeToString(sum[:])[:12])
}

// ParseFile reads one file. n is the number of stations read, or -1 when the
// file is missing or has an extension this cannot read.
func ParseFile(path string) (g Group, n int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, -1
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ini":
		return parseINI(string(data))
	case ".yaml", ".yml":
		return parseYAML(string(data))
	}
	return nil, -1
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// parseINI mirrors ParseMyStationsINI: every [section] is a category and every
// line holding an '=' is "Name=URL|Logo". Lines without '=' are skipped (they
// used to surface as blank entries on the receiver).
func parseINI(s string) (Group, int) {
	var g Group
	cur := -1
	count := 0
	for _, line := range splitLines(s) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			g = append(g, Category{Name: t[1 : len(t)-1]})
			cur = len(g) - 1
			continue
		}
		if cur < 0 || t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		name := strings.TrimSpace(line[:eq])
		val := line[eq+1:]
		parts := strings.Split(val, "|")
		st := Station{Name: name, URL: strings.TrimSpace(parts[0])}
		if len(parts) > 1 {
			st.Logo = strings.TrimSpace(parts[1])
		}
		st.ID = makeID(st.Name, st.URL)
		g[cur].Stations = append(g[cur].Stations, st)
		count++
	}
	return g, count
}

// parseYAML mirrors ParseMyStationsYAML, which is a line reader, not a YAML
// parser: "Category:" opens a category, "Name: URL|Logo" adds a station.
func parseYAML(s string) (Group, int) {
	var g Group
	count := 0
	for _, line := range splitLines(s) {
		if strings.HasSuffix(line, ":") {
			g = append(g, Category{Name: strings.TrimRight(strings.TrimSpace(line), ":")})
			continue
		}
		if len(g) > 0 && strings.Contains(line, ": ") {
			parts := splitAny(line, []string{": ", "|"})
			if len(parts) > 1 {
				st := Station{Name: strings.TrimSpace(parts[0]), URL: strings.TrimSpace(parts[1])}
				if len(parts) > 2 {
					st.Logo = strings.TrimSpace(parts[2])
				}
				st.ID = makeID(st.Name, st.URL)
				g[len(g)-1].Stations = append(g[len(g)-1].Stations, st)
				count++
			}
		}
	}
	return g, count
}

// splitAny splits on every occurrence of any separator, like TStringArray Split.
func splitAny(s string, seps []string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		hit := ""
		for _, sep := range seps {
			if strings.HasPrefix(s[i:], sep) {
				hit = sep
				break
			}
		}
		if hit != "" {
			out = append(out, s[start:i])
			i += len(hit)
			start = i
		} else {
			i++
		}
	}
	return append(out, s[start:])
}

// MergeInto folds src into dst: categories match case-insensitively, stations
// by ID, first one wins. Returns the number of stations added.
func MergeInto(dst *Group, src Group) int {
	added := 0
	for _, c := range src {
		ci := -1
		for j := range *dst {
			if strings.EqualFold((*dst)[j].Name, c.Name) {
				ci = j
				break
			}
		}
		if ci < 0 {
			*dst = append(*dst, Category{Name: c.Name})
			ci = len(*dst) - 1
		}
		for _, st := range c.Stations {
			found := false
			for _, e := range (*dst)[ci].Stations {
				if e.ID == st.ID {
					found = true
					break
				}
			}
			if !found {
				(*dst)[ci].Stations = append((*dst)[ci].Stations, st)
				added++
			}
		}
	}
	return added
}

// Store holds the published list. Reload is the only way it is built, as in
// the Pascal unit, so no input can reload alone and drop another's stations.
type Store struct {
	mu    sync.RWMutex
	group Group
	Files func() []string // extra files merged after the user's own (presets cache)
	Own   string
}

func (s *Store) Snapshot() Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.group
}

func (s *Store) Reload() (own, preset int) {
	var merged Group
	if g, n := ParseFile(s.Own); n >= 0 {
		own = MergeInto(&merged, g)
	}
	if s.Files != nil {
		for _, f := range s.Files() {
			if g, n := ParseFile(f); n >= 0 {
				preset += MergeInto(&merged, g)
			}
		}
	}
	s.mu.Lock()
	s.group = merged
	s.mu.Unlock()
	return own, preset
}

func (s *Store) ByID(id string) (cat string, st Station, ok bool) {
	for _, c := range s.Snapshot() {
		for _, x := range c.Stations {
			if x.ID == id {
				return c.Name, x, true
			}
		}
	}
	return "", Station{}, false
}
