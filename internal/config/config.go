// Package config reads retuner.ini and avr.ini with the same defaults, key
// order and "fill what is missing, keep what is there" behaviour as the
// Pascal build (src/common.pas, src/avr.pas).
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	AppName         = "Retuner"
	AppVersion      = "26.08.28.4" // kept equal to the Pascal build under comparison
	UpstreamVersion = "1.2.6"
	IniVersion      = "1.2.2"
	AppCopyright    = "a fork of YTuner " + UpstreamVersion + " by Greg P. (MIT) - https://github.com/ollisulopuisto/retuner"
)

type kv struct{ k, v string }
type section struct {
	name string
	keys []kv
}

// defaults mirrors the ini the Pascal binary writes on first run.
var defaults = []section{
	{"Configuration", []kv{{"INIVersion", IniVersion}, {"MessageInfoLevel", "4"}, {"IPAddress", "default"}, {"ActAsHost", "default"}, {"UseSSL", "1"}, {"ResolvePlaylists", "0"}, {"LocalCountry", ""}, {"RelayHTTPS", "0"}, {"RelayPort", "8888"}, {"RedirectHTTPCode", "302"}, {"IconSize", "200"}, {"IconCache", "1"}, {"IconEndPointExtension", ""}, {"MyToken", "0123456789ABCDEF"}, {"CommonAVRini", "1"}, {"CacheFolderLocation", "default"}, {"ConfigFolderLocation", "default"}, {"DBFolderLocation", "default"}, {"DBLibFile", "default"}}},
	{"WebServer", []kv{{"WebServerPort", "80"}, {"WebServerIPAddress", "default"}}},
	{"DNSServer", []kv{{"Enable", "1"}, {"DNSServerIPAddress", "default"}, {"DNSServerPort", "53"}, {"InterceptDNs", "*.vtuner.com,*.radiosetup.com,*.my-noxon.net,*.radiomarantz.com,*.radiodenon.com,*.radioharmankardon.com,*.wifiradiofrontier.com"}, {"DNSServers", "8.8.8.8,9.9.9.9"}, {"DNSAdvertiseIP", ""}, {"RestrictForwarding", "0"}, {"AllowedClients", ""}}},
	{"RadioBrowser", []kv{{"Enable", "1"}, {"RBAPIURL", "http://all.api.radio-browser.info"}, {"RBPopularAndSearchStationsLimit", "100"}, {"RBMinStationsPerCategory", "3"}, {"RBUUIDsCacheTTL", "0"}, {"RBUUIDsCacheAutoRefresh", "0"}, {"RBCacheTTL", "0"}, {"RBCacheType", "catFile"}}},
	{"MyStations", []kv{{"Enable", "1"}, {"MyStationsFile", "stations.ini"}, {"MyStationsAutoRefreshPeriod", "0"}}},
	{"Presets", []kv{{"Enable", "0"}, {"PresetsCountries", "default"}, {"PresetsURL", "https://raw.githubusercontent.com/ollisulopuisto/retuner/master/presets"}, {"PresetsAutoRefreshPeriod", "0"}}},
	{"Podcasts", []kv{{"Enable", "0"}, {"PodcastsFile", "podcasts.ini"}, {"PodcastEpisodesLimit", "100"}, {"PodcastCacheTTL", "60"}}},
	{"Bookmark", []kv{{"Enable", "1"}, {"CommonBookmark", "1"}, {"BookmarkStationsLimit", "100"}}},
	{"WebGUI", []kv{{"Enable", "0"}, {"WebGUIIPAddress", "127.0.0.1"}, {"WebGUIPort", "8090"}, {"WebGUIUser", "admin"}, {"WebGUIPassword", ""}, {"RemoteAVRAddress", ""}}},
	{"MaintenanceServer", []kv{{"Enable", "0"}, {"MaintenanceServerIPAddress", "127.0.0.1"}, {"MaintenanceServerPort", "8750"}}},
}

type Config struct {
	File *ini.File
	Path string
	Dir  string
}

// Load reads path, adds every missing key in default order, and writes the
// file back only when something was added. A missing file is created.
func Load(path string) (*Config, error) {
	f := ini.Empty(ini.LoadOptions{AllowShadows: false, IgnoreInlineComment: true})
	if _, err := os.Stat(path); err == nil {
		var lerr error
		f, lerr = ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, path)
		if lerr != nil {
			return nil, lerr
		}
	}
	changed := fill(f, defaults)
	if changed {
		ini.PrettyFormat = false // "Key=Value", as the Pascal writer and the shell suites expect
		if err := f.SaveTo(path); err != nil {
			return nil, err
		}
	}
	return &Config{File: f, Path: path, Dir: filepath.Dir(path)}, nil
}

func fill(f *ini.File, spec []section) bool {
	changed := false
	for _, s := range spec {
		sec := f.Section(s.name)
		for _, k := range s.keys {
			if !sec.HasKey(k.k) {
				sec.NewKey(k.k, k.v)
				changed = true
			}
		}
	}
	return changed
}

func (c *Config) Str(sec, key string) string { return c.File.Section(sec).Key(key).String() }

func (c *Config) Int(sec, key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.Str(sec, key)))
	if err != nil {
		return def
	}
	return n
}

func (c *Config) Bool(sec, key string) bool { return c.Int(sec, key, 0) != 0 }

// folder resolves a *FolderLocation key: "default" means beside the ini, and a
// custom location always gets the sub-folder name appended (<loc>/config), as in
// retuner.pas.
func (c *Config) folder(key, sub string) string {
	v := c.Str("Configuration", key)
	if v == "" || v == "default" {
		return filepath.Join(c.Dir, sub)
	}
	return filepath.Join(v, sub)
}

func (c *Config) ConfigDir() string { return c.folder("ConfigFolderLocation", "config") }
func (c *Config) CacheDir() string  { return c.folder("CacheFolderLocation", "cache") }

// MoveConfigFiles moves stations files and *.xml (bookmarks) that sit beside the
// ini into the config folder, as older installs kept them there (MoveConfigFiles
// in retuner.pas). Existing files in config are overwritten.
func (c *Config) MoveConfigFiles() []string {
	dst := c.ConfigDir()
	var moved []string
	own := c.Str("MyStations", "MyStationsFile")
	entries, err := os.ReadDir(c.Dir)
	if err != nil || filepath.Clean(dst) == filepath.Clean(c.Dir) {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		ln := strings.ToLower(n)
		if n == own || strings.HasPrefix(ln, "stations.") || strings.HasSuffix(ln, ".xml") {
			from := filepath.Join(c.Dir, n)
			data, err := os.ReadFile(from)
			if err != nil {
				continue
			}
			if os.WriteFile(filepath.Join(dst, n), data, 0o644) == nil {
				_ = os.Remove(from)
				moved = append(moved, n)
			}
		}
	}
	return moved
}

// ---- avr.ini ----

type MenuItem struct{ Identifier, Label string }

type AVR struct {
	MainMenu  []MenuItem
	Protocol  string // lower-cased avr.ini [Configuration] Protocol
	AllAsHTTP bool   // Protocol=all-as-http: https station URLs are handed to the receiver as http

	// [RadioBrowser Filtering]; every list has been through the Pascal strip-chars cleanup.
	AllowedTags, NotAllowedTags, AllowedCountries, AllowedLanguages    []string
	AllowedCodecs, NotAllowedCodecs, NotAllowedInName, NotAllowedInURL []string
	BitrateMax                                                         int
	// [RadioBrowser Sorting]
	Order   string
	Reverse bool
	// [Translator] - parsed and kept; the translation itself is not ported yet.
	Translator Translator
}

type Translator struct {
	Enabled, Auto                                     bool
	LanguageCode                                      string
	ReplaceLatin1Supplement, ReplaceExtA, ReplaceExtB bool
}

// filterStrip is RB_FILTER_STRIP_CHARS: each is replaced by a space, then the
// result is trimmed and runs of spaces collapse to one (StripChars/DelSpace1).
const filterStrip = "!\"'():@&=+$,/?%#[]-.~\n\r\t"

// StripChars mirrors common.pas StripChars for a set of single-byte characters.
func StripChars(in, chars string) string {
	var b strings.Builder
	for i := 0; i < len(in); i++ {
		if strings.IndexByte(chars, in[i]) >= 0 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(in[i])
		}
	}
	return strings.Join(strings.FieldsFunc(strings.TrimSpace(b.String()), func(r rune) bool { return r == ' ' }), " ")
}

func filterList(raw string) []string {
	var out []string
	for _, e := range strings.Split(StripChars(raw, filterStrip), ";") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

const avrIniVersion = "1.0.2"

var defaultMenuIDs = []string{"about", "bookmarks", "mystations", "podcasts", "local", "radiobrowser"}
var defaultMenuLabels = []string{"*** Retuner ***", "Bookmarks", "My Stations", "Podcasts", "Local Stations", "Radio Browser"}

var avrDefaults = []section{
	{"Configuration", []kv{{"INIVersion", avrIniVersion}, {"Protocol", "all-as-http"}}},
	{"MainMenu Items", []kv{{"IdentifiersList", strings.Join(defaultMenuIDs, ";")}, {"LabelsList", strings.Join(defaultMenuLabels, ";")}}},
	{"RadioBrowser Filtering", []kv{{"AllowedTags", ""}, {"NotAllowedTags", ""}, {"AllowedCountries", ""}, {"AllowedLanguages", ""}, {"AllowedCodecs", ""}, {"NotAllowedCodecs", ""}, {"NotAllowedInName", ""}, {"NotAllowedInURL", ""}, {"BitrateMax", ""}}},
	{"RadioBrowser Sorting", []kv{{"Order", "name"}, {"Reverse", "0"}}},
	{"Translator", []kv{{"Enable", "0"}, {"Auto", "0"}, {"LanguageCode", ""}, {"ReplaceUTF8Latin1Supplement", "0"}, {"ReplaceUTF8Latin1ExtendedA", "0"}, {"ReplaceUTF8Latin1ExtendedB", "0"}}},
}

// LoadAVR reads config/avr.ini, creating it with defaults. Mismatched
// identifier/label counts restore the default menu, as the Pascal build does
// ("Incorrect values in avr.ini [MainMenu Items] group!").
func (c *Config) LoadAVR() (*AVR, error) { return c.LoadAVRFile("avr.ini") }

// LoadAVRFile reads (and completes) one receiver config file in the config folder:
// avr.ini, or <mac>.ini when CommonAVRini is off.
func (c *Config) LoadAVRFile(name string) (*AVR, error) {
	dir := c.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, name)
	f := ini.Empty()
	if _, err := os.Stat(p); err == nil {
		var err2 error
		if f, err2 = ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, p); err2 != nil {
			return nil, err2
		}
	}
	changed := fill(f, avrDefaults)
	sec := f.Section("MainMenu Items")
	ids := splitList(sec.Key("IdentifiersList").String())
	labels := splitList(sec.Key("LabelsList").String())
	if len(ids) == 0 || len(ids) != len(labels) {
		sec.Key("IdentifiersList").SetValue(strings.Join(defaultMenuIDs, ";"))
		sec.Key("LabelsList").SetValue(strings.Join(defaultMenuLabels, ";"))
		ids, labels = defaultMenuIDs, defaultMenuLabels
		changed = true
	}
	if changed {
		ini.PrettyFormat = false
		if err := f.SaveTo(p); err != nil {
			return nil, err
		}
	}
	a := &AVR{Protocol: strings.ToLower(f.Section("Configuration").Key("Protocol").String())}
	a.AllAsHTTP = a.Protocol == "all-as-http"
	rf := f.Section("RadioBrowser Filtering")
	a.AllowedTags, a.NotAllowedTags = filterList(rf.Key("AllowedTags").String()), filterList(rf.Key("NotAllowedTags").String())
	a.AllowedCountries, a.AllowedLanguages = filterList(rf.Key("AllowedCountries").String()), filterList(rf.Key("AllowedLanguages").String())
	a.AllowedCodecs, a.NotAllowedCodecs = filterList(rf.Key("AllowedCodecs").String()), filterList(rf.Key("NotAllowedCodecs").String())
	a.NotAllowedInName, a.NotAllowedInURL = filterList(rf.Key("NotAllowedInName").String()), filterList(rf.Key("NotAllowedInURL").String())
	a.BitrateMax, _ = rf.Key("BitrateMax").Int()
	rs := f.Section("RadioBrowser Sorting")
	a.Order = rs.Key("Order").String()
	a.Reverse, _ = rs.Key("Reverse").Bool()
	if v, e := rs.Key("Reverse").Int(); e == nil {
		a.Reverse = v != 0
	}
	tr := f.Section("Translator")
	tb := func(k string) bool { v, e := tr.Key(k).Int(); return e == nil && v != 0 }
	a.Translator = Translator{Enabled: tb("Enable"), Auto: tb("Auto"), LanguageCode: strings.ToLower(tr.Key("LanguageCode").String()),
		ReplaceLatin1Supplement: tb("ReplaceUTF8Latin1Supplement"), ReplaceExtA: tb("ReplaceUTF8Latin1ExtendedA"), ReplaceExtB: tb("ReplaceUTF8Latin1ExtendedB")}
	for i := range ids {
		a.MainMenu = append(a.MainMenu, MenuItem{ids[i], labels[i]})
	}
	return a, nil
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ";")
}
