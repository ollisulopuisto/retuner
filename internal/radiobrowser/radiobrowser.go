// Package radiobrowser is the Radio-browser.info directory behind the "Radio
// Browser" menu (src/radiobrowser.pas). Stations and categories come from the
// public JSON API, are filtered per receiver (avr.ini [RadioBrowser Filtering])
// and cached in memory.
//
// Not ported: the SQLite cache types (catDB, catMemDB, catPermMemDB) and the
// on-disk catFile cache. Those settings fall back to the in-memory cache and
// the live API; see doc/GO-PORT.md.
package radiobrowser

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"retuner/internal/config"
)

type Kind int

const (
	Genre Kind = iota
	Country
	Language
	Popular
	Search
)

const (
	bulkLimit  = 1000000
	filterNone = "{empty}"
	stripJSON  = "!*'();:@&=+$,/?%#[]-_.~\n\r\t"
)

var categoryPath = [...]string{"tags", "countries", "languages"}

type Category struct {
	Name  string
	Count int
}

type Station struct {
	ID, Name, Homepage, URL, Icon, Tags, Country, CountryCode, Language, Votes, Codec, Bitrate string
}

type Client struct {
	API        string
	UserAgent  string
	PopLimit   int
	MinPerCat  int
	HideBroken bool
	CacheType  string // catNone disables caching; everything else is the memory cache
	TTL        time.Duration
	DB         *DB // set for the SQLite cache types

	mu    sync.Mutex
	cache map[string]cached
	uuids []string
	http  *http.Client
}

type cached struct {
	stations []Station
	cats     []Category
	at       time.Time
}

func New(api, ua string, popLimit, minPerCat int, cacheType string, ttlHours int) *Client {
	return &Client{API: strings.TrimRight(api, "/"), UserAgent: ua, PopLimit: popLimit, MinPerCat: minPerCat, HideBroken: true,
		CacheType: cacheType, TTL: time.Duration(ttlHours) * time.Hour, cache: map[string]cached{},
		http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) caching() bool { return c.CacheType != "catNone" && c.CacheType != "" }

func (c *Client) get(path string) []byte {
	full := c.API + "/json/" + path
	log.Printf("Radio-browser.info: GET %s", path)
	req, err := http.NewRequest("GET", full, nil)
	var body []byte
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		var resp *http.Response
		if resp, err = c.http.Do(req); err == nil {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil && resp.StatusCode/100 != 2 {
				err = fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
		}
	}
	if err != nil {
		// Say what went wrong and name the URL, as the Pascal build does: a refused
		// connection, a timeout and an HTTP error must stay distinguishable.
		log.Printf("Radio-browser.info: Error getting %s (%s)", full, describe(err))
		return []byte("[]")
	}
	return body
}

func describe(err error) string {
	s := err.Error()
	if strings.Contains(s, "connection refused") || strings.Contains(s, "connect:") {
		host := ""
		if u, e := url.Parse(strings.Split(strings.Split(s, `"`)[1], `"`)[0]); e == nil {
			host = u.Host
		}
		return "Connect to " + host + " failed: " + s
	}
	return s
}

func addParam(u, p string) string {
	if strings.Contains(u, "?") {
		return u + "&" + p
	}
	return u + "?" + p
}

func addBulk(u string) string {
	if strings.Contains(u, "limit=") {
		return u
	}
	return addParam(u, "limit="+strconv.Itoa(bulkLimit))
}

func parseArray(b []byte) []map[string]any {
	var raw []any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		log.Printf("Radio-browser.info: unusable answer (%v)", err)
		return nil
	}
	var out []map[string]any
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(v)
	}
}

func clean(s string) string { return config.StripChars(s, stripJSON) }

func toStation(m map[string]any) Station {
	return Station{ID: str(m, "stationuuid"), Name: clean(str(m, "name")), Homepage: str(m, "homepage"), URL: str(m, "url"),
		Icon: str(m, "favicon"), Tags: clean(str(m, "tags")), Country: clean(str(m, "country")), CountryCode: str(m, "countrycode"),
		Language: clean(str(m, "language")), Votes: str(m, "votes"), Codec: str(m, "codec"), Bitrate: str(m, "bitrate")}
}

// wild is IsWild(..., ignore case): * and ? wildcards over the whole string.
func wild(s, pat string) bool {
	var b strings.Builder
	b.WriteString("(?is)^")
	for _, r := range pat {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(s)
}

func haveCommon(s string, list []string) bool {
	for _, e := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '/' || r == ';' }) {
		for _, p := range list {
			if wild(strings.TrimSpace(e), p) {
				return true
			}
		}
	}
	return false
}

func containsIn(s string, list []string) bool {
	ls := strings.ToLower(s)
	for _, p := range list {
		if strings.Contains(ls, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func orEmpty(s string) string {
	if s == "" {
		return filterNone
	}
	return s
}

func categoryAllowed(m map[string]any, k Kind, a *config.AVR, minPerCat int) bool {
	name := str(m, "name")
	n, isNum := m["stationcount"].(json.Number)
	cnt := 0
	if isNum {
		f, _ := n.Float64()
		cnt = int(f)
	}
	if name == "" || cnt < minPerCat {
		return false
	}
	switch k {
	case Genre:
		if len(a.AllowedTags) > 0 && !haveCommon(name, a.AllowedTags) {
			return false
		}
		if len(a.NotAllowedTags) > 0 && haveCommon(name, a.NotAllowedTags) {
			return false
		}
	case Country:
		if len(a.AllowedCountries) > 0 && !haveCommon(name, a.AllowedCountries) {
			return false
		}
	case Language:
		if len(a.AllowedLanguages) > 0 && !haveCommon(name, a.AllowedLanguages) {
			return false
		}
	}
	return true
}

func stationAllowed(m map[string]any, k Kind, a *config.AVR) bool {
	tags, country, lang := orEmpty(str(m, "tags")), orEmpty(str(m, "country")), orEmpty(str(m, "language"))
	tagsBad := func() bool {
		return (len(a.AllowedTags) > 0 && !haveCommon(tags, a.AllowedTags)) || (len(a.NotAllowedTags) > 0 && haveCommon(tags, a.NotAllowedTags))
	}
	countryBad := func() bool { return len(a.AllowedCountries) > 0 && !haveCommon(country, a.AllowedCountries) }
	langBad := func() bool { return len(a.AllowedLanguages) > 0 && !haveCommon(lang, a.AllowedLanguages) }
	switch k {
	case Genre:
		if countryBad() || langBad() {
			return false
		}
	case Country:
		if tagsBad() || langBad() {
			return false
		}
	case Language:
		if tagsBad() || countryBad() {
			return false
		}
	case Popular:
		if tagsBad() || countryBad() || langBad() {
			return false
		}
	}
	br := 0
	if a.BitrateMax > 0 {
		if n, ok := m["bitrate"].(json.Number); ok { // only a JSON number counts; a string bitrate reads as 0
			f, _ := n.Float64()
			br = int(f)
		}
	}
	codec := orEmpty(str(m, "codec"))
	u := str(m, "url")
	switch {
	case len(a.AllowedCodecs) > 0 && !haveCommon(codec, a.AllowedCodecs):
		return false
	case len(a.NotAllowedCodecs) > 0 && haveCommon(codec, a.NotAllowedCodecs):
		return false
	case (a.Protocol == "http" || a.Protocol == "https") && !strings.Contains(u, a.Protocol+"://"):
		return false
	case a.BitrateMax > 0 && br > a.BitrateMax:
		return false
	case len(a.NotAllowedInName) > 0 && containsIn(str(m, "name"), a.NotAllowedInName):
		return false
	case len(a.NotAllowedInURL) > 0 && containsIn(u, a.NotAllowedInURL):
		return false
	}
	return true
}

func hash(s string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(s)))
	return strconv.Itoa(int(h.Sum32()))
}

func (c *Client) lookup(key string) (cached, bool) {
	if !c.caching() {
		return cached{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if ok && c.TTL > 0 && time.Since(e.at) > c.TTL {
		log.Printf("Radio-browser.info: cache TTL expired. Reloading")
		delete(c.cache, key)
		return cached{}, false
	}
	return e, ok
}

func (c *Client) store(key string, e cached) {
	if !c.caching() {
		return
	}
	e.at = time.Now()
	c.mu.Lock()
	c.cache[key] = e
	c.mu.Unlock()
}

func page[T any](all []T, start, howMany int) []T {
	if start >= len(all) {
		return nil
	}
	all = all[start:]
	if howMany > 0 && howMany < len(all) {
		all = all[:howMany]
	}
	return all
}

// Categories lists genres, countries or languages for one receiver (mac "" is
// the common config). total is the count after filtering, before paging.
func (c *Client) Categories(k Kind, a *config.AVR, mac string, start, howMany int) ([]Category, int) {
	if c.DB != nil {
		return c.DB.Categories(k, a, mac, start, howMany)
	}
	key := "RBC-" + mac + "-" + strconv.Itoa(int(k))
	if e, ok := c.lookup(key); ok && len(e.cats) > 0 {
		return page(e.cats, start, howMany), len(e.cats)
	}
	p := categoryPath[k]
	if c.HideBroken {
		p += "?hidebroken=true"
	}
	var all []Category
	for _, m := range parseArray(c.get(addBulk(p))) {
		if categoryAllowed(m, k, a, c.MinPerCat) {
			n, _ := strconv.Atoi(str(m, "stationcount"))
			all = append(all, Category{clean(str(m, "name")), n})
		}
	}
	if len(all) > 0 {
		c.store(key, cached{cats: all})
	}
	return page(all, start, howMany), len(all)
}

// Stations lists the stations of a category, the popular list or a search.
// name is the category as it appears in the request path (still URL-encoded) or
// the search text.
func (c *Client) Stations(k Kind, name string, a *config.AVR, mac string, start, howMany int) ([]Station, int) {
	if c.DB != nil {
		return c.DB.Stations(k, name, a, mac, start, howMany)
	}
	key := "RBS-" + mac + "-" + strconv.Itoa(int(k)) + "-" + hash(name)
	if e, ok := c.lookup(key); ok && len(e.stations) > 0 {
		return page(e.stations, start, howMany), len(e.stations)
	}
	sorting := "?order=" + url.QueryEscape(a.Order) + "&reverse=" + strconv.FormatBool(a.Reverse)
	var u string
	switch k {
	case Genre:
		u = "stations/bytagexact/" + name + sorting
	case Country:
		u = "stations/bycountryexact/" + name + sorting
	case Language:
		u = "stations/bylanguageexact/" + name + sorting
	case Popular:
		u = "stations?order=votes&reverse=true&limit=" + strconv.Itoa(c.PopLimit)
	case Search:
		u = "stations/search?order=votes&reverse=true&limit=" + strconv.Itoa(c.PopLimit) + "&name=" + url.QueryEscape(name)
	}
	if c.HideBroken {
		u += "&hidebroken=true"
	}
	var all []Station
	for _, m := range parseArray(c.get(addBulk(u))) {
		if stationAllowed(m, k, a) {
			all = append(all, toStation(m))
		}
	}
	if len(all) > 0 {
		c.store(key, cached{stations: all})
	}
	return page(all, start, howMany), len(all)
}

const uuidFile = "rbuuids.txt"

// StartUUIDs loads the station id index the way the Pascal build does at start:
// from cache/rbuuids.txt when it is there and fresh (ttlHours 0 = always fresh),
// otherwise from the API in the background, saving the result to that file.
// With autoRefresh and a TTL the list is renewed on that period.
func (c *Client) StartUUIDs(cacheDir string, ttlHours int, autoRefresh bool) {
	path := filepath.Join(cacheDir, uuidFile)
	if b, err := os.ReadFile(path); err == nil {
		line := strings.TrimRight(strings.SplitN(string(b), "\n", 2)[0], "\r")
		if len(line) <= 2 {
			log.Printf("Radio-browser.info UUIDs cache file seems empty..")
		} else {
			ids := strings.Split(line, ",")
			log.Printf("Successfully loaded %d RB UUIDs from cache file", len(ids))
			fresh := ttlHours == 0
			if fi, e := os.Stat(path); e == nil && time.Since(fi.ModTime()) < time.Duration(ttlHours)*time.Hour {
				fresh = true
			}
			if fresh {
				c.mu.Lock()
				c.uuids = ids
				c.mu.Unlock()
			} else {
				log.Printf("Radio-browser.info UUIDs cache file is older then %d hours. Refreshing RB UUIDs list..", ttlHours)
			}
		}
	}
	c.mu.Lock()
	have := c.uuids != nil
	c.mu.Unlock()
	if !have {
		go c.refreshUUIDs(path)
	}
	if autoRefresh && ttlHours > 0 {
		go func() {
			for range time.Tick(time.Duration(ttlHours) * time.Hour) {
				c.refreshUUIDs(path)
			}
		}()
	}
}

func (c *Client) refreshUUIDs(path string) {
	log.Printf("Getting Radio-browser.info UUIDs..")
	raw := c.get(addBulk("stations"))
	var ids []string
	for _, m := range parseArray(raw) {
		if u := str(m, "stationuuid"); u != "" {
			ids = append(ids, u)
		}
	}
	if len(ids) == 0 {
		log.Printf("Radio-browser.info UUIDs not loaded. Empty response.")
		return
	}
	c.mu.Lock()
	c.uuids = ids
	c.mu.Unlock()
	log.Printf("Successfully downloaded %d RB UUIDs", len(ids))
	if err := os.WriteFile(path, []byte(strings.Join(ids, ",")), 0o644); err == nil {
		log.Printf("Successfully saved %d RB UUIDs to cache file", len(ids))
	}
}

// ByID resolves the 12 hex characters of a receiver-side id (RB_xxxxxxxxxxxx)
// to a station. The full UUID list is fetched once.
func (c *Client) ByID(id12 string, a *config.AVR, mac string) (Station, bool) {
	if c.DB != nil {
		return c.DB.ByID(id12, a, mac)
	}
	if len(id12) != 12 {
		return Station{}, false
	}
	want := strings.ToLower(id12[:8] + "-" + id12[8:] + "-")
	uuid := ""
	for tries := 0; tries < 2 && uuid == ""; tries++ {
		c.mu.Lock()
		have := c.uuids
		c.mu.Unlock()
		if have == nil {
			var ids []string
			for _, m := range parseArray(c.get(addBulk("stations"))) {
				if u := str(m, "stationuuid"); u != "" {
					ids = append(ids, u)
				}
			}
			if len(ids) == 0 {
				return Station{}, false
			}
			c.mu.Lock()
			c.uuids, have = ids, ids
			c.mu.Unlock()
		}
		for _, u := range have {
			if strings.HasPrefix(strings.ToLower(u), want) {
				uuid = u
				break
			}
		}
		if uuid == "" {
			break
		}
	}
	if uuid == "" {
		return Station{}, false
	}
	// A station already seen in one of this receiver's cached lists is served from
	// there, as the Pascal build does, without another request.
	if st, ok := c.fromLists(uuid, mac); ok {
		return st, true
	}
	if e, ok := c.lookup("RBU-" + uuid); ok && len(e.stations) == 1 {
		return e.stations[0], true
	}
	arr := parseArray(c.get("stations/byuuid/" + uuid))
	if len(arr) == 0 {
		return Station{}, false
	}
	st := toStation(arr[0])
	c.store("RBU-"+uuid, cached{stations: []Station{st}})
	return st, true
}

func (c *Client) fromLists(uuid, mac string) (Station, bool) {
	if !c.caching() {
		return Station{}, false
	}
	prefix := "RBS-" + mac + "-"
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.cache {
		if !strings.HasPrefix(k, prefix) || (c.TTL > 0 && time.Since(e.at) > c.TTL) {
			continue
		}
		for _, st := range e.stations {
			if st.ID == uuid {
				return st, true
			}
		}
	}
	return Station{}, false
}
