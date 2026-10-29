// Retuner (Go port, in progress). See doc/GO-PORT.md for stage and parity status.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"

	"path/filepath"
	"retuner/internal/config"
	"retuner/internal/server"
	"retuner/internal/stations"
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
	port := flag.Int("p", 0, "web server port (overrides the ini)")
	iniPath := flag.String("c", "retuner.ini", "path to retuner.ini")
	flag.Parse()
	log.Printf("Retuner (Go port) - %s", config.AppCopyright)

	cfg, err := config.Load(*iniPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	avr, err := cfg.LoadAVR()
	if err != nil {
		log.Fatalf("avr.ini: %v", err)
	}
	my := localIP(cfg.Str("Configuration", "IPAddress"))
	urlHost := cfg.Str("Configuration", "ActAsHost")
	if urlHost == "" || urlHost == "default" {
		urlHost = my
	}

	store := &stations.Store{Own: filepath.Join(cfg.ConfigDir(), cfg.Str("MyStations", "MyStationsFile"))}
	if cfg.Bool("MyStations", "Enable") {
		own, pre := store.Reload()
		log.Printf("My stations: loaded %d (%d from presets)", own+pre, pre)
	}

	wp := cfg.Int("WebServer", "WebServerPort", 80)
	if *port != 0 {
		wp = *port
	}
	bind := cfg.Str("WebServer", "WebServerIPAddress")
	if bind == "default" {
		bind = my
	}
	addr := net.JoinHostPort(bind, strconv.Itoa(wp))
	log.Printf("Web Service: listening on: %s", addr)
	if err := http.ListenAndServe(addr, server.New(cfg, avr, urlHost, store).Handler()); err != nil {
		log.Printf("web service: %v", err)
		os.Exit(1)
	}
}
