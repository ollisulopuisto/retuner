// Package config reads retuner.ini and avr.ini with the same defaults, key
// order and "fill what is missing, keep what is there" behaviour as the
// Pascal build (src/common.pas, src/avr.pas).
package config

import (
	"bytes"
	"hash/crc32"
	"log"
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

// Pascal's TIniFile treats section and key names as case-insensitive: an
// existing entry is used or updated in place, keeping the user's spelling
// and position, and no differently-cased duplicate is added.
func findSection(f *ini.File, name string) *ini.Section {
	for _, sec := range f.Sections() {
		if strings.EqualFold(sec.Name(), name) {
			return sec
		}
	}
	return nil
}

func ensureSection(f *ini.File, name string) *ini.Section {
	if sec := findSection(f, name); sec != nil {
		return sec
	}
	return f.Section(name)
}

func findKey(sec *ini.Section, name string) *ini.Key {
	for _, k := range sec.Keys() {
		if strings.EqualFold(k.Name(), name) {
			return k
		}
	}
	return nil
}

// defaults mirrors the ini the Pascal binary writes on first run.
// Section and key order is the order the Pascal build writes its first-run
// retuner.ini, which is also the order its TIniFile appends missing entries
// to an existing file (ReadINIConfiguration in src/retuner.pas writes
// MessageInfoLevel before INIVersion).
var defaults = []section{
	{"Configuration", []kv{{"MessageInfoLevel", "4"}, {"INIVersion", IniVersion}, {"IPAddress", "default"}, {"ActAsHost", "default"}, {"UseSSL", "1"}, {"ResolvePlaylists", "0"}, {"LocalCountry", ""}, {"RelayHTTPS", "0"}, {"RelayPort", "8888"}, {"RedirectHTTPCode", "302"}, {"IconSize", "200"}, {"IconCache", "1"}, {"IconEndPointExtension", ""}, {"MyToken", "0123456789ABCDEF"}, {"CommonAVRini", "1"}, {"CacheFolderLocation", "default"}, {"ConfigFolderLocation", "default"}, {"DBFolderLocation", "default"}, {"DBLibFile", "default"}}},
	{"MyStations", []kv{{"Enable", "1"}, {"MyStationsFile", "stations.ini"}, {"MyStationsAutoRefreshPeriod", "0"}}},
	{"Presets", []kv{{"Enable", "0"}, {"PresetsCountries", "default"}, {"PresetsURL", "https://raw.githubusercontent.com/ollisulopuisto/retuner/master/presets"}, {"PresetsAutoRefreshPeriod", "0"}}},
	{"Podcasts", []kv{{"Enable", "0"}, {"PodcastsFile", "podcasts.ini"}, {"PodcastEpisodesLimit", "100"}, {"PodcastCacheTTL", "60"}}},
	{"RadioBrowser", []kv{{"Enable", "1"}, {"RBAPIURL", "http://all.api.radio-browser.info"}, {"RBPopularAndSearchStationsLimit", "100"}, {"RBMinStationsPerCategory", "3"}, {"RBUUIDsCacheTTL", "0"}, {"RBUUIDsCacheAutoRefresh", "0"}, {"RBCacheTTL", "0"}, {"RBCacheType", "catFile"}}},
	{"Bookmark", []kv{{"Enable", "1"}, {"CommonBookmark", "1"}, {"BookmarkStationsLimit", "100"}}},
	{"WebServer", []kv{{"WebServerIPAddress", "default"}, {"WebServerPort", "80"}}},
	{"DNSServer", []kv{{"Enable", "1"}, {"DNSServerIPAddress", "default"}, {"DNSServerPort", "53"}, {"InterceptDNs", "*.vtuner.com,*.radiosetup.com,*.my-noxon.net,*.radiomarantz.com,*.radiodenon.com,*.radioharmankardon.com,*.wifiradiofrontier.com"}, {"DNSServers", "8.8.8.8,9.9.9.9"}, {"DNSAdvertiseIP", ""}, {"RestrictForwarding", "0"}, {"AllowedClients", ""}}},
	{"WebGUI", []kv{{"Enable", "0"}, {"WebGUIIPAddress", "127.0.0.1"}, {"WebGUIPort", "8090"}, {"WebGUIUser", "admin"}, {"WebGUIPassword", ""}, {"RemoteAVRAddress", ""}}},
	{"MaintenanceServer", []kv{{"Enable", "0"}, {"MaintenanceServerIPAddress", "127.0.0.1"}, {"MaintenanceServerPort", "8750"}}},
}

type Config struct {
	File *ini.File
	Path string
	Dir  string
}

// Pascal's TIniFile keeps whole-line comments byte-for-byte and in place;
// go-ini would normalise their spacing, so they never reach go-ini as
// comments: parseINI swaps each one for a synthetic key, saveINI swaps the
// original text back on write, and dropPlaceholders removes the stand-ins
// from the in-memory file so no later save can leak them as keys.
const commentPlaceholder = "___retunerComment"

func parseINI(data []byte) (*ini.File, map[string]string, error) {
	comments := map[string]string{}
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			key := commentPlaceholder + strconv.Itoa(i)
			// Pascal's TIniFile keeps the comment text but trims its
			// trailing whitespace.
			comments[key] = strings.TrimRight(strings.TrimSuffix(l, "\r"), " \t")
			lines[i] = key + "="
		}
	}
	f, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, []byte(strings.Join(lines, "\n")))
	if err != nil {
		return nil, nil, err
	}
	return f, comments, nil
}

func dropPlaceholders(f *ini.File) {
	for _, sec := range f.Sections() {
		var drop []string
		for _, k := range sec.Keys() {
			if strings.HasPrefix(k.Name(), commentPlaceholder) {
				drop = append(drop, k.Name())
			}
		}
		for _, name := range drop {
			sec.DeleteKey(name)
		}
	}
}

// saveINI writes f to path the way Pascal's TIniFile leaves the file:
// "Key=Value" without alignment (PrettyFormat off) and comment lines
// byte-for-byte as they were read.
func saveINI(f *ini.File, path string, comments map[string]string) error {
	ini.PrettyFormat = false
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return err
	}
	lines := strings.Split(buf.String(), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, commentPlaceholder) {
			if orig, ok := comments[strings.TrimSuffix(l, "=")]; ok {
				lines[i] = orig
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// Load reads path, adds every missing key in default order, and writes the
// file back only when something was added. A missing file is created.
func Load(path string) (*Config, error) {
	f := ini.Empty(ini.LoadOptions{AllowShadows: false, IgnoreInlineComment: true})
	comments := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		var lerr error
		f, comments, lerr = parseINI(b)
		if lerr != nil {
			return nil, lerr
		}
		defer dropPlaceholders(f)
	}
	// retuner.pas: warn about a missing or outdated INIVersion, then always
	// store the current one.
	changed := false
	if sec := findSection(f, "Configuration"); sec == nil || findKey(sec, "INIVersion") == nil {
		if _, err := os.Stat(path); err == nil {
			log.Printf("WARNING: You are running out of INI file! Some features may not work properly!")
			log.Printf("WARNING: Retuner will try to fill in the missing options, but check their description and usage on https://github.com/ollisulopuisto/retuner/tree/master/cfg.")
		}
	} else if v := findKey(sec, "INIVersion"); v.String() != IniVersion {
		log.Printf("WARNING: Your retuner.ini file is outdated! Some features may not work properly!")
		log.Printf("WARNING: Retuner will try to fill in the missing options, but check their description and usage on https://github.com/ollisulopuisto/retuner/tree/master/cfg.")
		v.SetValue(IniVersion)
		changed = true
	}
	changed = fill(f, defaults) || changed
	if changed {
		if err := saveINI(f, path, comments); err != nil {
			return nil, err
		}
	}
	return &Config{File: f, Path: path, Dir: filepath.Dir(path)}, nil
}

func fill(f *ini.File, spec []section) bool {
	changed := false
	for _, s := range spec {
		sec := ensureSection(f, s.name)
		for _, k := range s.keys {
			if findKey(sec, k.k) == nil {
				sec.NewKey(k.k, k.v)
				changed = true
			}
		}
	}
	return changed
}

func (c *Config) Str(sec, key string) string {
	s2 := findSection(c.File, sec)
	if s2 == nil {
		return ""
	}
	k := findKey(s2, key)
	if k == nil {
		return ""
	}
	return k.String()
}

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
func (c *Config) DBDir() string     { return c.folder("DBFolderLocation", "db") }
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
	// FileCRC is the CRC-32 of the ini file as saved; it names the per-receiver SQL view.
	FileCRC uint32
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
	// IgnoreInlineComment: Pascal's TIniFile neither strips inline comments
	// on read nor quotes values on write; without this, go-ini wraps values
	// containing ';' or '#' in backticks that Pascal never writes.
	f := ini.Empty(ini.LoadOptions{IgnoreInlineComment: true})
	comments := map[string]string{}
	if b, err := os.ReadFile(p); err == nil {
		var err2 error
		if f, comments, err2 = parseINI(b); err2 != nil {
			return nil, err2
		}
		defer dropPlaceholders(f)
	}
	changed := fill(f, avrDefaults)
	sec := ensureSection(f, "MainMenu Items")
	idKey, labelKey := findKey(sec, "IdentifiersList"), findKey(sec, "LabelsList")
	var ids, labels []string
	if idKey != nil && labelKey != nil {
		ids, labels = splitList(idKey.String()), splitList(labelKey.String())
	}
	if len(ids) == 0 || len(ids) != len(labels) {
		if idKey == nil {
			idKey, _ = sec.NewKey("IdentifiersList", "")
		}
		if labelKey == nil {
			labelKey, _ = sec.NewKey("LabelsList", "")
		}
		idKey.SetValue(strings.Join(defaultMenuIDs, ";"))
		labelKey.SetValue(strings.Join(defaultMenuLabels, ";"))
		ids, labels = defaultMenuIDs, defaultMenuLabels
		changed = true
	}
	avrVerKey := findKey(ensureSection(f, "Configuration"), "INIVersion")
	if v := avrVerKey; v != nil && v.String() != avrIniVersion {
		log.Printf("WARNING: Your %s file is outdated! Some features may not work properly!", name)
		log.Printf("WARNING: Your %s file will be updated with new options right now. Check https://github.com/ollisulopuisto/retuner/tree/master/cfg for the description of the appropriate avr.ini file for your version of Retuner", name)
		v.SetValue(avrIniVersion)
		changed = true
	} else if avrVerKey == nil {
		changed = true // fill already queued the key; keep the save below
	}
	if changed {
		if err := saveINI(f, p, comments); err != nil {
			return nil, err
		}
	}
	var fileCRC uint32
	if b, e := os.ReadFile(p); e == nil {
		fileCRC = crc32.ChecksumIEEE(b)
	}
	avrGet := func(secName, keyName string) string {
		if sec := findSection(f, secName); sec != nil {
			if k := findKey(sec, keyName); k != nil {
				return k.String()
			}
		}
		return ""
	}
	a := &AVR{FileCRC: fileCRC, Protocol: strings.ToLower(avrGet("Configuration", "Protocol"))}
	a.AllAsHTTP = a.Protocol == "all-as-http"
	a.AllowedTags, a.NotAllowedTags = filterList(avrGet("RadioBrowser Filtering", "AllowedTags")), filterList(avrGet("RadioBrowser Filtering", "NotAllowedTags"))
	a.AllowedCountries, a.AllowedLanguages = filterList(avrGet("RadioBrowser Filtering", "AllowedCountries")), filterList(avrGet("RadioBrowser Filtering", "AllowedLanguages"))
	a.AllowedCodecs, a.NotAllowedCodecs = filterList(avrGet("RadioBrowser Filtering", "AllowedCodecs")), filterList(avrGet("RadioBrowser Filtering", "NotAllowedCodecs"))
	a.NotAllowedInName, a.NotAllowedInURL = filterList(avrGet("RadioBrowser Filtering", "NotAllowedInName")), filterList(avrGet("RadioBrowser Filtering", "NotAllowedInURL"))
	a.BitrateMax, _ = strconv.Atoi(strings.TrimSpace(avrGet("RadioBrowser Filtering", "BitrateMax")))
	a.Order = avrGet("RadioBrowser Sorting", "Order")
	if v, e := strconv.Atoi(strings.TrimSpace(avrGet("RadioBrowser Sorting", "Reverse"))); e == nil {
		a.Reverse = v != 0
	}
	tb := func(k string) bool {
		v, e := strconv.Atoi(strings.TrimSpace(avrGet("Translator", k)))
		return e == nil && v != 0
	}
	a.Translator = Translator{Enabled: tb("Enable"), Auto: tb("Auto"), LanguageCode: strings.ToLower(avrGet("Translator", "LanguageCode")),
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
