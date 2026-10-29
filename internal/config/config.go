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
	f := ini.Empty(ini.LoadOptions{AllowShadows: false})
	if _, err := os.Stat(path); err == nil {
		var lerr error
		f, lerr = ini.LoadSources(ini.LoadOptions{}, path)
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

// ConfigDir resolves ConfigFolderLocation ("default" means ./config beside the ini).
func (c *Config) ConfigDir() string {
	v := c.Str("Configuration", "ConfigFolderLocation")
	if v == "" || v == "default" {
		return filepath.Join(c.Dir, "config")
	}
	return v
}

// ---- avr.ini ----

type MenuItem struct{ Identifier, Label string }

type AVR struct {
	MainMenu  []MenuItem
	AllAsHTTP bool // Protocol=all-as-http: https station URLs are handed to the receiver as http
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
func (c *Config) LoadAVR() (*AVR, error) {
	dir := c.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "avr.ini")
	f := ini.Empty()
	if _, err := os.Stat(p); err == nil {
		var err2 error
		if f, err2 = ini.Load(p); err2 != nil {
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
	a := &AVR{AllAsHTTP: strings.EqualFold(f.Section("Configuration").Key("Protocol").String(), "all-as-http")}
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
