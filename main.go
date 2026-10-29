// Retuner (Go port, in progress). See doc/GO-PORT.md for stage and parity status.
package main

import (
	"hash/crc32"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"retuner/internal/avrremote"
	"retuner/internal/bookmark"
	"retuner/internal/config"
	"retuner/internal/dnsserver"
	"retuner/internal/playlist"
	"retuner/internal/presets"
	"retuner/internal/radiobrowser"
	"retuner/internal/relay"
	"retuner/internal/server"
	"retuner/internal/stations"
	"retuner/internal/webgui"
)

func localIP(v string) string {
	if v != "" && v != "default" {
		return v
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return "127.0.0.1"
}

func main() {
	// Like the Pascal build: no command line options (anything given is ignored),
	// retuner.ini lives beside the executable.
	iniPath := "retuner.ini"
	if exe, err := os.Executable(); err == nil {
		iniPath = filepath.Join(filepath.Dir(exe), "retuner.ini")
	}
	log.Printf("Retuner (Go port) - %s", config.AppCopyright)

	cfg, err := config.Load(iniPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	_ = os.MkdirAll(cfg.CacheDir(), 0o755)
	_ = os.MkdirAll(cfg.ConfigDir(), 0o755)
	if moved := cfg.MoveConfigFiles(); len(moved) > 0 {
		log.Printf("Moving files to config directory ..")
	}
	avr, err := cfg.LoadAVR()
	if err != nil {
		log.Fatalf("avr.ini: %v", err)
	}
	var rb *radiobrowser.Client
	if cfg.Int("RadioBrowser", "Enable", 1) != 0 {
		ct := cfg.Str("RadioBrowser", "RBCacheType")
		rb = radiobrowser.New(cfg.Str("RadioBrowser", "RBAPIURL"), "Retuner/"+config.AppVersion,
			cfg.Int("RadioBrowser", "RBPopularAndSearchStationsLimit", 100), cfg.Int("RadioBrowser", "RBMinStationsPerCategory", 3), ct, cfg.Int("RadioBrowser", "RBCacheTTL", 0))
		if ct == "catDB" || ct == "catMemDB" || ct == "catPermMemDB" {
			// As in Pascal the database is prepared before serving. A failure
			// disables Radio Browser; it never falls back to per-request live calls.
			dbDir := cfg.DBDir()
			db, err := rb.PrepareDB(dbDir, ct, cfg.Int("RadioBrowser", "RBCacheTTL", 0))
			if err != nil {
				log.Printf("ERROR: Radio Browser database: %v. Radio Browser is DISABLED", err)
				rb = nil
			} else {
				rb.DB = db
				if ttl := cfg.Int("RadioBrowser", "RBCacheTTL", 0); ttl > 0 {
					go func() {
						for range time.Tick(time.Duration(ttl) * time.Hour) {
							if err := db.Refresh(); err != nil {
								log.Printf("Radio Browser database refresh failed, keeping the old one: %v", err)
							}
						}
					}()
				}
			}
		}
	}
	if rb != nil && rb.DB == nil { // the database answers id lookups itself
		rb.StartUUIDs(cfg.CacheDir(), cfg.Int("RadioBrowser", "RBUUIDsCacheTTL", 0), cfg.Bool("RadioBrowser", "RBUUIDsCacheAutoRefresh"))
	}
	my := localIP(cfg.Str("Configuration", "IPAddress"))
	urlHost := cfg.Str("Configuration", "ActAsHost")
	if urlHost == "" || urlHost == "default" {
		urlHost = my
	}

	pre := &presets.Presets{Enabled: cfg.Bool("Presets", "Enable"), URL: cfg.Str("Presets", "PresetsURL"),
		Countries: cfg.Str("Presets", "PresetsCountries"), Dir: filepath.Join(cfg.ConfigDir(), "presets")}
	if pre.Countries == "default" {
		pre.Countries = ""
	}
	if pre.URL == "" {
		pre.URL = presets.DefaultURL
	}
	store := &stations.Store{Own: filepath.Join(cfg.ConfigDir(), cfg.Str("MyStations", "MyStationsFile")), Files: pre.CachedFiles}
	reload := func() {
		own, pc := store.Reload()
		if own+pc == 0 {
			log.Printf("My stations: no stations loaded")
		} else if pc > 0 {
			log.Printf("My stations: successfully loaded %d my stations (%d from presets)", own+pc, pc)
		} else {
			log.Printf("My stations: successfully loaded %d my stations", own)
		}
	}
	// Before the first station load, so a fresh install serves presets in its
	// very first menu; synchronous on purpose (nothing is serving yet).
	if pre.Enabled {
		pre.Refresh()
	}
	if cfg.Bool("MyStations", "Enable") {
		reload()
		// Auto refresh: re-read the own file only when its content changed.
		if mins := cfg.Int("MyStations", "MyStationsAutoRefreshPeriod", 0); mins > 0 {
			go func() {
				last := fileSum(store.Own)
				for range time.Tick(time.Duration(mins) * time.Minute) {
					if sum := fileSum(store.Own); sum != last {
						log.Printf("\"My Stations\" file has been changed. Refreshing..")
						reload()
						last = sum
					}
				}
			}()
		}
		if mins := cfg.Int("Presets", "PresetsAutoRefreshPeriod", 0); pre.Enabled && mins > 0 {
			go func() {
				for range time.Tick(time.Duration(mins) * time.Minute) {
					pre.Refresh()
					reload()
				}
			}()
		}
	}

	if cfg.Bool("WebGUI", "Enable") {
		(&webgui.Server{Allowed: cfg.Str("WebGUI", "WebGUIIPAddress"), User: cfg.Str("WebGUI", "WebGUIUser"), Password: cfg.Str("WebGUI", "WebGUIPassword"),
			Remote: avrremote.New(cfg.Str("WebGUI", "RemoteAVRAddress")), Store: store, FileName: cfg.Str("MyStations", "MyStationsFile"), AppName: config.AppName, Version: config.AppVersion,
		}).Start(cfg.Str("WebGUI", "WebGUIIPAddress"), cfg.Int("WebGUI", "WebGUIPort", 8090))
	}

	lim := cfg.Int("Bookmark", "BookmarkStationsLimit", 100)
	bm := &bookmark.Store{Dir: cfg.ConfigDir(), Common: cfg.Bool("Bookmark", "CommonBookmark"), Limit: lim, URLHost: urlHost}

	var dnsSrv *dnsserver.Server
	if cfg.Bool("DNSServer", "Enable") {
		dnsSrv = dnsserver.New(cfg.Str("DNSServer", "InterceptDNs"), cfg.Str("DNSServer", "DNSServers"),
			cfg.Str("DNSServer", "DNSAdvertiseIP"), cfg.Bool("DNSServer", "RestrictForwarding"), cfg.Str("DNSServer", "AllowedClients"))
		dip := cfg.Str("DNSServer", "DNSServerIPAddress")
		if dip == "default" {
			dip = my
		}
		dport := cfg.Int("DNSServer", "DNSServerPort", 53)
		go func() {
			log.Printf("DNS Service: listening on: %s:%d", dip, dport)
			if err := dnsSrv.Start(dip, dport); err != nil {
				log.Printf("DNS Server error: %v", err)
			}
		}()
	}

	wp := cfg.Int("WebServer", "WebServerPort", 80)
	bind := cfg.Str("WebServer", "WebServerIPAddress")
	if bind == "default" {
		bind = my
	}
	addr := net.JoinHostPort(bind, strconv.Itoa(wp))
	log.Printf("Web Service: listening on: %s", addr)
	srv := server.New(cfg, avr, urlHost, store, bm, dnsSrv, rb)
	if cfg.Bool("Configuration", "RelayHTTPS") {
		rl := &relay.Server{Resolve: srv.RelayResolve, Playlists: cfg.Bool("Configuration", "ResolvePlaylists"),
			Resolver: &playlist.Resolver{UserAgent: "Retuner/" + config.AppVersion}, UserAgent: "Retuner/" + config.AppVersion}
		if err := rl.Start(cfg.Str("WebServer", "WebServerIPAddress"), cfg.Int("Configuration", "RelayPort", 8888)); err != nil {
			log.Printf("Relay Service: error (%v)", err)
		}
	}
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Printf("web service: %v", err)
		os.Exit(1)
	}
}

func fileSum(path string) uint32 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return crc32.ChecksumIEEE(b)
}
