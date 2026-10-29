// Package presets fetches per-country station lists (src/presets.pas) into
// <config>/presets/<cc>.ini, validates them, and names the cached files the
// station merge should read.
package presets

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"retuner/internal/config"
)

const (
	DefaultURL = "https://raw.githubusercontent.com/ollisulopuisto/retuner/master/presets"
	maxBytes   = 512 * 1024
)

type Presets struct {
	Enabled   bool
	Countries string
	URL       string
	Dir       string // <config>/presets
}

// CountryCodes: two ASCII letters, lower-cased, de-duplicated, order kept.
// Anything else is ignored with a warning.
func (p *Presets) CountryCodes() []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range regexp.MustCompile(`[,; ]+`).Split(p.Countries, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		code := strings.ToLower(part)
		if len(code) == 2 && code[0] >= 'a' && code[0] <= 'z' && code[1] >= 'a' && code[1] <= 'z' {
			if !seen[code] {
				seen[code] = true
				out = append(out, code)
			}
		} else {
			log.Printf("Presets: ignoring '%s' - a country must be a two-letter code such as fi or se", part)
		}
	}
	return out
}

// CachedFiles lists the cached files for the configured countries, in order.
func (p *Presets) CachedFiles() []string {
	if !p.Enabled {
		return nil
	}
	var out []string
	for _, c := range p.CountryCodes() {
		f := filepath.Join(p.Dir, c+".ini")
		if _, err := os.Stat(f); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// looksLikeStationList: some line, in any section, whose value (after the
// first '=') starts with http:// or https://. An error page or an empty file
// must never replace a good cached list.
func looksLikeStationList(data []byte) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "[") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		v := strings.TrimSpace(line[strings.Index(line, "=")+1:])
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return true
		}
	}
	return false
}

func (p *Presets) fetch(code string) bool {
	url := strings.TrimRight(p.URL, "/") + "/" + code + ".ini"
	target := filepath.Join(p.Dir, code+".ini")
	tmp := target + ".tmp"
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Presets: getting error %s (%v)", url, err)
		return false
	}
	req.Header.Set("User-Agent", "Retuner/"+config.AppVersion)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Presets: getting error %s (%v)", url, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Presets: getting error %s (HTTP %d)", url, resp.StatusCode)
		return false
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if len(data) > maxBytes {
		// DELIBERATE DIVERGENCE: the Pascal client terminates the transfer at the cap;
		// what it then does with the truncated prefix was not verified. A list cut
		// off mid-way must not look complete, so an oversized answer is rejected
		// outright and the cached copy stays.
		log.Printf("Presets: %s is larger than %d bytes; refusing it", url, maxBytes)
		return false
	}
	if len(data) == 0 {
		log.Printf("Presets: %s returned nothing", url)
		return false
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("Presets: cannot write %s (%v)", tmp, err)
		return false
	}
	if !looksLikeStationList(data) {
		_ = os.Remove(tmp)
		keep := "nothing cached for " + code
		if _, err := os.Stat(target); err == nil {
			keep = "keeping the cached copy"
		}
		log.Printf("Presets: %s is not a station list; %s", url, keep)
		return false
	}
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		log.Printf("Presets: cannot move %s into place", tmp)
		return false
	}
	log.Printf("Presets: updated %s from %s", code, url)
	return true
}

// Refresh fetches every configured country. True when something usable is cached.
func (p *Presets) Refresh() bool {
	if !p.Enabled {
		return false
	}
	codes := p.CountryCodes()
	if len(codes) == 0 {
		log.Printf("Presets: enabled, but no usable country code is configured")
		return false
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		log.Printf("Presets: cannot create %s", p.Dir)
		return false
	}
	fresh := 0
	for _, c := range codes {
		if p.fetch(c) {
			fresh++
		}
	}
	ok := len(p.CachedFiles()) > 0
	switch {
	case fresh == len(codes):
		log.Printf("Presets: %d of %d up to date", fresh, len(codes))
	case ok:
		log.Printf("Presets: only %d of %d could be fetched; using the cached copies for the rest", fresh, len(codes))
	default:
		log.Printf("Presets: nothing could be fetched and nothing is cached")
	}
	return ok
}
