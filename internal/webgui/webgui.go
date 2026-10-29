// Package webgui is the stations editor and remote page (src/webgui.pas): a second
// HTTP listener with Basic auth, a client allow-list and a JSON API. The HTML
// pages are the unchanged pages of the Pascal build. Receiver remote control
// (src/avrremote.pas) is not ported yet: its endpoints answer with an error.
package webgui

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"retuner/internal/stations"
)

//go:embed gui.html
var guiPage string

//go:embed remote.html
var remotePage string

const (
	failedAuthDelay = time.Second
	realm           = "Retuner"
	service         = "Web GUI"
)

type Server struct {
	Allowed  string // WebGUIIPAddress
	User     string
	Password string
	Store    *stations.Store
	FileName string // the stations file name, as shown in the editor
	AppName  string
	Version  string
	Remote   Remote
}

// Remote is the receiver remote control behind /api/remote/*.
type Remote interface {
	Status() string // JSON
	Command(cmd string) string
	Select(index int) string
	Search(q string) string
}

type unsupportedRemote struct{}

const noRemote = "Receiver remote control is not supported by this Go build yet."

func (unsupportedRemote) Status() string {
	return `{"ok":false,"error":"` + noRemote + `"}`
}
func (unsupportedRemote) Command(string) string { return noRemote }
func (unsupportedRemote) Select(int) string     { return noRemote }
func (unsupportedRemote) Search(string) string  { return noRemote }

func (s *Server) isYAML() bool { return !strings.EqualFold(filepath.Ext(s.Store.Own), ".ini") }

func ipv4(a string) (uint32, bool) {
	p := strings.Split(a, ".")
	if len(p) != 4 {
		return 0, false
	}
	var v uint32
	for _, x := range p {
		if x == "" || len(x) > 3 {
			return 0, false
		}
		n, err := strconv.Atoi(x)
		if err != nil || n < 0 || n > 255 {
			return 0, false
		}
		v = v<<8 | uint32(n)
	}
	return v, true
}

func inRange(remote, entry string) bool {
	i := strings.Index(entry, "/")
	if i < 0 {
		return false
	}
	bits, err := strconv.Atoi(entry[i+1:])
	if err != nil || bits < 0 || bits > 32 {
		return false
	}
	net_, ok1 := ipv4(entry[:i])
	addr, ok2 := ipv4(remote)
	if !ok1 || !ok2 {
		return false
	}
	var mask uint32
	if bits > 0 {
		mask = ^uint32(0) << (32 - bits)
	}
	return net_&mask == addr&mask
}

func (s *Server) clientAllowed(remote string) bool {
	a := s.Allowed
	if a == "" || a == "default" || a == "0.0.0.0" {
		return true
	}
	for _, e := range strings.Split(a, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			if inRange(remote, e) {
				return true
			}
		} else if strings.EqualFold(remote, e) {
			return true
		}
	}
	return false
}

func (s *Server) authorised(r *http.Request) bool {
	u, p, ok := r.BasicAuth()
	if !ok {
		return false
	}
	// both are compared, so a wrong user name costs the same as a wrong password
	uo := subtle.ConstantTimeCompare([]byte(u), []byte(s.User))
	po := subtle.ConstantTimeCompare([]byte(p), []byte(s.Password))
	return uo&po == 1
}

type jobj = map[string]any

func sendJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	io.WriteString(w, body)
}

func errJSON(msg string) string { b, _ := json.Marshal(jobj{"error": msg}); return string(b) }

func remoteErr(msg string) string {
	b, _ := json.Marshal(jobj{"ok": false, "error": msg})
	return string(b)
}

func (s *Server) stationsJSON() string {
	type st struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Logo string `json:"logo"`
	}
	type cat struct {
		Name     string `json:"name"`
		Stations []st   `json:"stations"`
	}
	cats := []cat{}
	for _, c := range s.Store.Snapshot() {
		x := cat{Name: c.Name, Stations: []st{}}
		for _, t := range c.Stations {
			x.Stations = append(x.Stations, st{t.Name, t.URL, t.Logo})
		}
		cats = append(cats, x)
	}
	b, _ := json.Marshal(struct {
		Categories []cat  `json:"categories"`
		File       string `json:"file"`
		ReadOnly   bool   `json:"readonly"`
	}{cats, s.FileName, s.isYAML()})
	return string(b)
}

func (s *Server) statusJSON() string {
	n := 0
	g := s.Store.Snapshot()
	for _, c := range g {
		n += len(c.Stations)
	}
	b, _ := json.Marshal(struct {
		App        string `json:"app"`
		Version    string `json:"version"`
		Categories int    `json:"categories"`
		Stations   int    `json:"stations"`
		File       string `json:"file"`
	}{s.AppName, s.Version, len(g), n, s.FileName})
	return string(b)
}

// save writes the editor's JSON back as a stations.ini (saveStationsFromJSON).
func (s *Server) save(body []byte) string {
	if s.isYAML() {
		return "Editing is only supported for .ini station files."
	}
	var in struct {
		Categories []json.RawMessage `json:"categories"`
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil || top["categories"] == nil || json.Unmarshal(body, &in) != nil || in.Categories == nil {
		return "Malformed request."
	}
	var out []string
	for _, rc := range in.Categories {
		var c struct {
			Name     string            `json:"name"`
			Stations []json.RawMessage `json:"stations"`
		}
		if json.Unmarshal(rc, &c) != nil {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		out = append(out, "["+name+"]")
		for _, rs := range c.Stations {
			var t struct{ Name, URL, Logo string }
			var m map[string]any
			if json.Unmarshal(rs, &m) != nil {
				continue
			}
			str := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
			t.Name, t.URL, t.Logo = str("name"), str("url"), str("logo")
			t.Name = strings.NewReplacer("=", "-", "|", "-").Replace(t.Name)
			t.URL = strings.ReplaceAll(t.URL, "|", "-")
			t.Logo = strings.ReplaceAll(t.Logo, "|", "-")
			if t.Name == "" || t.URL == "" {
				continue
			}
			if t.Logo == "" {
				out = append(out, t.Name+"="+t.URL)
			} else {
				out = append(out, t.Name+"="+t.URL+"|"+t.Logo)
			}
		}
		out = append(out, "")
	}
	if err := os.WriteFile(s.Store.Own, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		return "Could not save: " + err.Error()
	}
	s.Store.Reload()
	return ""
}

func fieldString(body []byte, f string) (string, bool) {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil || m == nil {
		return "", false
	}
	v, _ := m[f].(string)
	return v, true
}

func fieldInt(body []byte, f string) (int, bool) {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return 0, false
	}
	v, ok := m[f].(float64)
	return int(v), ok
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.clientAllowed(host) {
		log.Printf("%s: refused %s from %s (WebGUIIPAddress=%s)", service, r.URL.RequestURI(), host, s.Allowed)
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "Service unavailable.")
		return
	}
	if !s.authorised(r) {
		time.Sleep(failedAuthDelay)
		log.Printf("%s: unauthorised %s from %s", service, r.URL.RequestURI(), host)
		w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
		w.WriteHeader(401)
		io.WriteString(w, "Authentication required.")
		return
	}
	if r.Method == "POST" && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		sendJSON(w, 400, errJSON("Expected an application/json request."))
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	rem := s.Remote
	if rem == nil {
		rem = unsupportedRemote{}
	}
	post := r.Method == "POST"
	reply := func(e string) {
		if e == "" {
			sendJSON(w, 200, `{"ok":true}`)
		} else {
			sendJSON(w, 200, remoteErr(e))
		}
	}
	switch strings.ToLower(r.URL.Path) {
	case "/", "/index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, guiPage)
	case "/api/status":
		sendJSON(w, 200, s.statusJSON())
	case "/remote", "/remote/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, remotePage)
	case "/api/remote/status":
		sendJSON(w, 200, rem.Status())
	case "/api/remote/cmd":
		if !post {
			reply("POST required.")
		} else if v, ok := fieldString(body, "cmd"); !ok {
			reply("Malformed request.")
		} else {
			e := rem.Command(v)
			if e != "" {
				log.Printf("%s: remote refused %s from %s", service, v, host)
			}
			reply(e)
		}
	case "/api/remote/select":
		if !post {
			reply("POST required.")
		} else if v, ok := fieldInt(body, "index"); !ok {
			reply("Malformed request.")
		} else {
			reply(rem.Select(v))
		}
	case "/api/remote/search":
		if !post {
			reply("POST required.")
		} else if v, ok := fieldString(body, "q"); !ok {
			reply("Malformed request.")
		} else {
			reply(rem.Search(v))
		}
	case "/api/stations":
		if post {
			if e := s.save(body); e != "" {
				sendJSON(w, 200, errJSON(e))
			} else {
				sendJSON(w, 200, `{"ok":true}`)
			}
		} else {
			sendJSON(w, 200, s.stationsJSON())
		}
	default:
		sendJSON(w, 404, `{"error":"Not found."}`)
	}
}

// Start listens on ip:port. Without a password it refuses to start.
func (s *Server) Start(ip string, port int) bool {
	if s.Password == "" {
		log.Printf("%s: no password set. Set WebGUIPassword in retuner.ini; not starting.", service)
		return false
	}
	_ = ip // the setting is a client allow-list; the listener takes every interface, as the release build does
	addr := ":" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("%s: error (%v)", service, err)
		return false
	}
	log.Printf("%s: listening on port %d, accepting %s", service, port, map[bool]string{true: "any client", false: s.Allowed}[s.Allowed == "" || s.Allowed == "default" || s.Allowed == "0.0.0.0"])
	go func() { _ = http.Serve(ln, s) }()
	return true
}

var _ = fmt.Sprint
