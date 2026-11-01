// Package server is the HTTP side of Retuner: the vTuner endpoints a receiver
// calls. Routing follows httpserver.pas RegisterServerRoutes, including the
// case-insensitive asp names, the Frontier Silicon deep path shape and the
// legacy /ytuner/ prefix kept alive for bookmarks saved before the rename.
package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"retuner/internal/bookmark"
	"retuner/internal/config"
	"retuner/internal/dnsserver"
	"retuner/internal/playlist"
	"retuner/internal/podcasts"
	"retuner/internal/radiobrowser"
	"retuner/internal/relay"
	"retuner/internal/stations"
	"retuner/internal/translator"
	"retuner/internal/vtuner"
)

const (
	pathRoot       = "retuner"
	pathRootLegacy = "ytuner"
	msgEmptyFolder = "This folder is intentionally empty..."
	msgNoStations  = "No station(s) found"
)

type Server struct {
	cfg       *config.Config
	avr       *config.AVR
	urlHost   string
	token     string
	iconSize  int
	placehold bool
	bookmarks bool
	store     *stations.Store
	mystOn    bool
	bm        *bookmark.Store
	dns       *dnsserver.Server
	rb        *radiobrowser.Client
	localCtry string
	commonAVR bool
	avrMu     sync.Mutex
	avrs      map[string]*config.AVR

	resolver   *playlist.Resolver
	pod        *podcasts.Store
	iconOnce   sync.Once
	iconBytes  []byte
	httpClient *http.Client
}

func New(cfg *config.Config, avr *config.AVR, urlHost string, store *stations.Store, bm *bookmark.Store, dns *dnsserver.Server, rb *radiobrowser.Client) *Server {
	size := cfg.Int("Configuration", "IconSize", 200)
	if size <= 0 {
		size = 200
	}
	s := &Server{resolver: &playlist.Resolver{UserAgent: "Retuner/" + config.AppVersion}, cfg: cfg, avr: avr, urlHost: urlHost, token: cfg.Str("Configuration", "MyToken"), iconSize: size,
		placehold: true, bookmarks: cfg.Bool("Bookmark", "Enable"), bm: bm, dns: dns, rb: rb, avrs: map[string]*config.AVR{}, commonAVR: cfg.Int("Configuration", "CommonAVRini", 1) != 0, localCtry: strings.TrimSpace(cfg.Str("Configuration", "LocalCountry")), store: store, mystOn: cfg.Bool("MyStations", "Enable"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: 10 * time.Second,
				MaxIdleConns:          50,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
			},
		}}
	s.pod = &podcasts.Store{File: filepath.Join(cfg.ConfigDir(), cfg.Str("Podcasts", "PodcastsFile")), UserAgent: "Retuner/" + config.AppVersion,
		Limit: cfg.Int("Podcasts", "PodcastEpisodesLimit", 100), TTL: time.Duration(cfg.Int("Podcasts", "PodcastCacheTTL", 60)) * time.Minute}
	if s.pod.Limit < 1 {
		s.pod.Limit = 100
	}
	if cfg.Bool("Podcasts", "Enable") {
		s.pod.Load()
	}
	if s.token == "" {
		s.token = "0123456789ABCDEF"
	}
	if v := cfg.File.Section("Configuration").Key("IconPlaceholder"); v.String() != "" {
		s.placehold = v.String() != "0"
	}
	return s
}

func (s *Server) respond(w http.ResponseWriter, code int, ctype string, body []byte) {
	w.Header().Set("Server", "Retuner/"+config.AppVersion)
	w.Header().Set("Content-Type", ctype)
	// Pascal answers with a Content-Length, never chunked; some receivers need that.
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	// Pascal (fphttp) adds a "Status: <code> OK" header, with the text "OK"
	// whatever the code ("Status: 404 OK"). Kept so the replies match.
	w.Header().Set("Status", strconv.Itoa(code)+" OK")
	w.WriteHeader(code)
	if body != nil {
		_, _ = w.Write(body)
	}
}

func (s *Server) page(w http.ResponseWriter, p *vtuner.Page) {
	s.respond(w, http.StatusOK, "application/xml", []byte(p.XML()))
}

func (s *Server) display(w http.ResponseWriter, msg string) {
	p := &vtuner.Page{TotalItemsCount: 1}
	p.Add(vtuner.Display{Message: msg})
	s.page(w, p)
}

func (s *Server) dir(title, dest string, count int) vtuner.Directory {
	return vtuner.Directory{Title: title, Destination: "http://" + s.urlHost + "/" + dest, ItemCount: count}
}

// Handler dispatches like the Pascal router: segment-aware, case-insensitive on
// the asp file names, anything unmatched answers 404 with an empty body.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		lp := strings.ToLower(path)
		switch {
		case strings.HasPrefix(lp, "/setupapp/") && strings.HasSuffix(lp, "/loginxml.asp") && lp != "/setupapp/loginxml.asp":
			s.login(w, r)
		case strings.HasPrefix(lp, "/setupapp/") && strings.HasSuffix(lp, "/statxml.asp") && lp != "/setupapp/statxml.asp":
			s.station(w, r)
		case strings.HasPrefix(lp, "/setupapp/") && strings.HasSuffix(lp, "/search.asp") && lp != "/setupapp/search.asp" && s.rb != nil:
			s.search(w, r)
		case strings.HasPrefix(lp, "/setupapp/") && strings.HasSuffix(lp, "/navxml.asp") && lp != "/setupapp/navxml.asp" && s.rb != nil:
			s.rbRoot(w, r)
		case lp == "/setupapp/favxml.asp":
			s.favorite(w, r)
		default:
			for _, root := range []string{pathRoot, pathRootLegacy} {
				switch lp {
				case "/" + root + "/about":
					s.about(w, r)
					return
				case "/" + root + "/empty":
					s.display(w, msgEmptyFolder)
					return
				case "/" + root + "/icon":
					s.icon(w, r)
					return
				case "/" + root + "/mystations":
					s.myStations(w, r)
					return
				case "/" + root + "/bookmark":
					s.bookmarkList(w, r)
					return
				case "/" + root + "/podcasts":
					s.podcastFeeds(w, r)
					return
				case "/" + root + "/play":
					s.play(w, r)
					return
				case "/" + root + "/search":
					if s.rb != nil {
						s.search(w, r)
						return
					}
				case "/" + root + "/radiobrowser":
					if s.rb != nil {
						s.rbRoot(w, r)
						return
					}
				}
				if pre := "/" + root + "/radiobrowser/"; s.rb != nil && strings.HasPrefix(lp, pre) && len(lp) > len(pre) {
					parts := strings.Split(r.URL.EscapedPath()[len(pre):], "/")
					if len(parts) == 1 {
						s.rbCategory(w, r, httpDecode(parts[0]), "", false)
						return
					}
					if len(parts) == 2 && parts[1] != "" {
						s.rbCategory(w, r, httpDecode(parts[0]), parts[1], true)
						return
					}
				}
				if pre := "/" + root + "/podcasts/"; strings.HasPrefix(lp, pre) && len(lp) > len(pre) && !strings.Contains(lp[len(pre):], "/") {
					s.podcastEpisodes(w, r, httpDecode(r.URL.EscapedPath()[len(pre):]))
					return
				}
				if pre := "/" + root + "/mystations/"; strings.HasPrefix(lp, pre) && len(lp) > len(pre) && !strings.Contains(lp[len(pre):], "/") {
					esc := r.URL.EscapedPath()
					seg := esc[len(pre):]
					s.myStationsOfCategory(w, r, httpDecode(seg))
					return
				}
			}
			s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		}
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// The AVR's entry point is where we learn the address a device really reaches
	// us from; on a public host that address may then have its lookups forwarded.
	if s.dns != nil {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			s.dns.NoteWebClient(host)
		}
	}
	if r.URL.Query().Get("token") == "0" {
		s.respond(w, http.StatusOK, "application/xml", []byte(vtuner.Token(s.token)))
		return
	}
	p := &vtuner.Page{}
	a, _ := s.avrFor(r)
	for _, mi := range a.MainMenu {
		switch mi.Identifier {
		case "mystations":
			if s.mystOn {
				p.Add(s.dir(mi.Label, pathRoot+"/mystations", len(s.store.Snapshot())))
			}
		case "bookmarks":
			if s.bookmarks {
				cleanMAC, _ := validMAC(r)
				p.Add(s.dir(mi.Label, pathRoot+"/bookmark", s.bm.Count(cleanMAC)))
			}
		case "radiobrowser":
			if s.rb != nil {
				p.Add(s.dir(mi.Label, pathRoot+"/radiobrowser", 4))
			}
		case "local":
			if s.rb != nil && s.localCtry != "" {
				p.Add(s.dir(mi.Label, pathRoot+"/radiobrowser/country/"+urlEncode(s.localCtry), s.rb.PopLimit))
			}
		case "about":
			p.Add(s.dir(mi.Label, pathRoot+"/about", 2))
		case "podcasts":
			if n := len(s.pod.Feeds()); s.podOn() && n > 0 {
				p.Add(s.dir(mi.Label, pathRoot+"/podcasts", n))
			}
		}
	}
	// Pascal order follows the avr.ini list; about comes first by default.
	p.TotalItemsCount = len(p.Items)
	s.page(w, p)
}

// station answers a lookup by id. Ids shorter than three characters, unknown
// ids and prefixes not ported yet all end in "No station(s) found" - the
// Pascal build once left such a request hanging until the receiver timed out.
func (s *Server) station(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if len(id) >= 3 && id[2] == '_' && id[:2] == stations.Prefix {
		if cat, st, ok := s.store.ByID(id); ok {
			p := &vtuner.Page{TotalItemsCount: 1}
			p.Add(s.vtunerStation(cat, st, r))
			s.page(w, p)
			return
		}
	}
	if len(id) >= 3 && id[:2] == podcasts.Prefix {
		if e, ok := s.pod.EpisodeByID(id); ok {
			p := &vtuner.Page{TotalItemsCount: 1}
			p.Add(s.podStation(e, e.Description, r))
			s.page(w, p)
			return
		}
	}
	if len(id) >= 3 && strings.HasPrefix(id, "RB_") && s.rb != nil {
		if st, ok := s.rbByID(id, r); ok && st.ID != "" {
			p := &vtuner.Page{TotalItemsCount: 1}
			p.Add(s.rbStation(st, "", r))
			s.page(w, p)
			return
		}
		s.display(w, msgNoStations)
		return
	}
	if len(id) >= 3 && id[2] == 'B' {
		mac, ok := validMAC(r)
		if !ok {
			s.respond(w, http.StatusBadRequest, "text/html; charset=utf-8", nil)
			return
		}
		body, found := s.bm.StationInfo(mac, id)
		if !found {
			s.respond(w, http.StatusNotFound, "application/xml", nil)
		} else {
			s.respond(w, http.StatusOK, "application/xml", body)
		}
		return
	}
	s.display(w, msgNoStations)
}

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	p := &vtuner.Page{TotalItemsCount: 2}
	p.Add(s.dir("Welcome to "+config.AppName+" v"+config.AppVersion, pathRoot+"/empty", 1))
	p.Add(s.dir(config.AppCopyright, pathRoot+"/empty", 1))
	s.page(w, p)
}

// icon answers with the placeholder mark. Real station logos arrive with the
// station sources; this is the path a station without a logo takes. Both GET
// and HEAD must carry type and length - receivers HEAD first and give up on 404.
const (
	iconMaxBytes  = 4 * 1024 * 1024
	iconMaxEdge   = 8192
	iconMaxPixels = 16 * 1024 * 1024
)

var safeIconKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *Server) servePlaceholder(w http.ResponseWriter) {
	b := s.placeholder()
	if !s.placehold || len(b) == 0 {
		s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		return
	}
	w.Header().Set("Content-Length", itoa(len(b)))
	// curl -X HEAD (what the smoke test uses) waits for a body that a HEAD
	// response never carries unless the server closes the connection; the
	// Pascal server closes after every response, so do the same here.
	w.Header().Set("Connection", "close")
	s.respond(w, http.StatusOK, "image/jpeg", b)
}

// icon serves a station logo: from the cache folder when cached, else fetched
// from the station's logo URL (4 MB cap, redirects followed), decoded (JPEG,
// PNG, GIF), shrunk to IconSize and sent as JPEG, then cached under the id.
// Anything that fails ends in the placeholder (or 404 when that is off).
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !safeIconKey.MatchString(id) {
		log.Printf("Icon id refused, it is not an id")
		// Pascal answers ctNone here, which its response layer renders as
		// text/html; charset=utf-8 - not an absent Content-Type.
		s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		return
	}
	cacheOn := s.cfg.Int("Configuration", "IconCache", 1) != 0
	cachePath := filepath.Join(s.cfg.CacheDir(), id)
	if cacheOn {
		if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
			s.respond(w, http.StatusOK, "image/jpeg", b)
			return
		}
	}
	logoURL := ""
	switch {
	case strings.HasPrefix(id, stations.Prefix):
		if _, st, ok := s.store.ByID(id); ok {
			logoURL = st.Logo
		}
	case strings.HasPrefix(id, podcasts.Prefix):
		logoURL = s.pod.LogoForEpisode(id)
	case strings.HasPrefix(id, "RB_"):
		if st, ok := s.rbByID(id, r); ok {
			logoURL = st.Icon
		}
	}
	if logoURL != "" {
		if out, ok := s.fetchLogo(logoURL); ok {
			if cacheOn {
				_ = os.MkdirAll(s.cfg.CacheDir(), 0o755)
				if err := os.WriteFile(cachePath, out, 0o644); err != nil {
					log.Printf("%s file save error (%v)", id, err)
				}
			}
			s.respond(w, http.StatusOK, "image/jpeg", out)
			return
		}
		log.Printf("Cannot process image from %q", logoURL)
	}
	s.servePlaceholder(w)
}

func (s *Server) fetchLogo(u string) ([]byte, bool) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		log.Printf("Error getting image from: %q? (%v)", u, err)
		return nil, false
	}
	req.Header.Set("Accept", "image/jpeg,image/png,image/gif")
	req.Header.Set("User-Agent", "Retuner/"+config.AppVersion)
	cl := s.httpClient
	if cl == nil {
		cl = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		log.Printf("Error getting image from: %q? (%v)", u, err)
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		log.Printf("Error getting image from: %q? (HTTP %d)", u, resp.StatusCode)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, iconMaxBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > iconMaxBytes {
		log.Printf("Error getting image from: %q? (empty, unreadable or over %d bytes)", u, iconMaxBytes)
		return nil, false
	}
	cfgImg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		log.Printf("Unsupported image type: %q", u)
		return nil, false
	}
	if cfgImg.Width > iconMaxEdge || cfgImg.Height > iconMaxEdge || cfgImg.Width*cfgImg.Height > iconMaxPixels {
		log.Printf("Unsupported stream with image type: %q? /image too large/", u)
		return nil, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		log.Printf("Unsupported stream with image type: %q? /%v/", u, err)
		return nil, false
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w > s.iconSize || h > s.iconSize {
		f := float64(s.iconSize) / float64(h)
		if w >= h {
			f = float64(s.iconSize) / float64(w)
		}
		nw, nh := int(math.Round(float64(w)*f)), int(math.Round(float64(h)*f))
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		src = scale(src, nw, nh)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, flatten(src), &jpeg.Options{Quality: 75}); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// flatten puts the image over white; JPEG has no alpha.
func flatten(src image.Image) image.Image {
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	return dst
}

func (s *Server) placeholder() []byte {
	s.iconOnce.Do(func() {
		raw, err := base64.StdEncoding.DecodeString(placeholderIconPNGBase64)
		if err != nil {
			return
		}
		src, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return
		}
		dst := scale(src, s.iconSize, s.iconSize)
		var out bytes.Buffer
		if jpeg.Encode(&out, dst, &jpeg.Options{Quality: 75}) == nil {
			s.iconBytes = out.Bytes()
		}
	})
	return s.iconBytes
}

// scale is a box filter, enough for shrinking a 192px mark.
func scale(src image.Image, w, h int) image.Image {
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // flatten alpha onto white, as JPEG has none
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+(x+1)*sb.Dx()/w
			y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+(y+1)*sb.Dy()/h
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var r, g, b, n uint32
			for yy := y0; yy < y1 && yy < sb.Max.Y; yy++ {
				for xx := x0; xx < x1 && xx < sb.Max.X; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					// composite over white
					cr = cr + (0xffff - ca)
					cg = cg + (0xffff - ca)
					cb = cb + (0xffff - ca)
					r += cr >> 8
					g += cg >> 8
					b += cb >> 8
					n++
				}
			}
			if n > 0 {
				i := dst.PixOffset(x, y)
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
			}
		}
	}
	return dst
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// httpDecode is HTTPDecode from fphttp: '+' is a space, %XX is a byte.
func httpDecode(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if d, err := url.PathUnescape(s); err == nil {
		return d
	}
	return s
}

// urlEncode is URLEncode in common.pas: every byte that is not an ASCII letter
// becomes %XX - digits, dots and dashes included.
func urlEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(hex2(c)))
		}
	}
	return b.String()
}

func hex2(c byte) string {
	const d = "0123456789ABCDEF"
	return string([]byte{d[c>>4], d[c&15]})
}

// pageRange is GetPageRange in httpserver.pas, indices zero-based inclusive.
// Receivers page with start/howmany or startitems/enditems.
func pageRange(q url.Values, n int) (first, last int) {
	atoi := func(k string) (int, bool) { v, err := strconv.Atoi(q.Get(k)); return v, err == nil }
	if st, ok := atoi("start"); ok {
		if hm, ok2 := atoi("howmany"); ok2 {
			first, last = st, st+hm-1
			goto done
		}
	}
	first, _ = atoi("startitems")
	last, _ = atoi("enditems")
	if last < first {
		last = first
	}
done:
	first--
	last--
	if first < 0 {
		first = 0
	}
	if last > n-1 {
		last = n - 1
	} else if last < 0 {
		last = n - 1 // issue coffeegreg/YTuner#28: a missing end means "to the end"
	}
	return
}

func indexFoldASCII(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func (s *Server) stripHTTPS(u string, r *http.Request) string {
	// Protocol=all-as-http is the shipped default of avr.ini; with a common avr.ini
	// (CommonAVRini=1, the default) it applies to every receiver.
	if a, _ := s.avrFor(r); a.AllAsHTTP {
		// Pascal's StringReplace(..., [rfIgnoreCase]) without rfReplaceAll: the
		// first "https://" anywhere in the URL, not only a leading one.
		if i := indexFoldASCII(u, "https://"); i >= 0 {
			return u[:i] + "http://" + u[i+len("https://"):]
		}
	}
	return u
}

// listURL is the URL a listing hands the receiver: the stripped stream URL, or
// our play endpoint when it is a playlist that must be unwrapped on selection.
func (s *Server) listURL(raw, uid string, r *http.Request) string {
	if s.cfg.Bool("Configuration", "RelayHTTPS") && relay.NeedsRelay(raw) {
		return relay.URLFor(s.urlHost, s.cfg.Int("Configuration", "RelayPort", 8888), uid)
	}
	u := s.stripHTTPS(raw, r)
	if s.cfg.Bool("Configuration", "ResolvePlaylists") && playlist.Looks(u) {
		return "http://" + s.urlHost + "/" + pathRoot + "/play?id=" + uid
	}
	return u
}

func (s *Server) vtunerStation(cat string, st stations.Station, r *http.Request) vtuner.Station {
	return vtuner.Station{
		UID: st.ID, Name: st.Name,
		Description: "My favorite \"" + st.Name + "\"",
		URL:         s.listURL(st.URL, st.ID, r),
		Icon:        "http://" + s.urlHost + "/" + pathRoot + "/icon?id=" + st.ID,
		Genre:       cat,
		Bookmark:    "http://" + s.urlHost + "/setupapp/favxml.asp?id=" + st.ID + "&fav=add",
	}
}

func (s *Server) myStations(w http.ResponseWriter, r *http.Request) {
	g := s.store.Snapshot()
	if len(g) == 0 {
		s.display(w, msgNoStations)
		return
	}
	first, last := pageRange(r.URL.Query(), len(g))
	p := &vtuner.Page{TotalItemsCount: len(g)}
	for i := first; i <= last && i < len(g); i++ {
		p.Add(s.dir(g[i].Name, pathRoot+"/mystations/"+urlEncode(g[i].Name), len(g[i].Stations)))
	}
	s.page(w, p)
}

func (s *Server) myStationsOfCategory(w http.ResponseWriter, r *http.Request, cat string) {
	g := s.store.Snapshot()
	if len(g) == 0 {
		s.display(w, msgNoStations)
		return
	}
	for _, c := range g {
		if !strings.EqualFold(c.Name, cat) {
			continue
		}
		if len(c.Stations) == 0 {
			break
		}
		first, last := pageRange(r.URL.Query(), len(c.Stations))
		p := &vtuner.Page{TotalItemsCount: len(c.Stations)}
		for i := first; i <= last && i < len(c.Stations); i++ {
			// Genre carries the category as the receiver spelled it in the URL, not as
			// stored (the Pascal handler reuses the request value), so match that.
			p.Add(s.vtunerStation(cat, c.Stations[i], r))
		}
		s.page(w, p)
		return
	}
	s.display(w, msgNoStations+" : "+cat)
}

// play redirects a receiver to the station's stream. Playlist resolution
// (ResolvePlaylists) and the other id prefixes arrive with their own stages.
func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	target := ""
	if len(id) >= 2 && id[:2] == stations.Prefix {
		if _, st, ok := s.store.ByID(id); ok {
			target = s.stripHTTPS(st.URL, r)
		}
	}
	if len(id) >= 2 && id[:2] == podcasts.Prefix {
		if e, ok := s.pod.EpisodeByID(id); ok {
			target = s.stripHTTPS(e.URL, r)
		}
	}
	if strings.HasPrefix(id, "RB_") {
		if st, ok := s.rbByID(id, r); ok {
			target = s.stripHTTPS(st.URL, r)
		}
	}
	if target == "" {
		s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		return
	}
	// Unwrap here, when the station is selected, not when a listing is built.
	if s.cfg.Bool("Configuration", "ResolvePlaylists") && playlist.Looks(target) {
		target = s.stripHTTPS(s.resolver.Resolve(target), r)
	}
	w.Header().Set("Location", target)
	s.respond(w, redirectCode(s.cfg), "text/html; charset=utf-8", nil)
}

func redirectCode(c *config.Config) int {
	n := c.Int("Configuration", "RedirectHTTPCode", 302)
	if n < 300 || n > 399 {
		return 302
	}
	return n
}

// safeBookmarkMAC enforces safe receiver MAC identifiers to prevent path traversal.
var safeBookmarkMAC = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validMAC validates the mac query parameter. If empty, it returns ("", true).
// If invalid characters or path traversal are present, it returns ("", false).
func validMAC(r *http.Request) (string, bool) {
	mac := r.URL.Query().Get("mac")
	if mac == "" {
		return "", true
	}
	if !safeBookmarkMAC.MatchString(mac) {
		return "", false
	}
	return mac, true
}

func (s *Server) bookmarkList(w http.ResponseWriter, r *http.Request) {
	mac, ok := validMAC(r)
	if !ok {
		s.respond(w, http.StatusBadRequest, "text/html; charset=utf-8", nil)
		return
	}
	first, last := pageRange(r.URL.Query(), s.bm.Count(mac))
	body, ok := s.bm.Page(mac, first, last, s.nameFn(r))
	if !ok {
		s.respond(w, http.StatusNotFound, "application/xml", nil)
		return
	}
	s.respond(w, http.StatusOK, "application/xml", body)
}

// favorite is the receiver's add/remove-favourite call. Only stations of the
// user's own list resolve here so far; the Pascal handler answers "No
// station(s) found" for an id it cannot look up, and otherwise answers an empty
// 200 once the file is updated (it never writes a body for the call itself).

func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	mac := q.Get("mac")
	fav := q.Get("fav")
	if !safeBookmarkMAC.MatchString(mac) {
		s.respond(w, http.StatusBadRequest, "text/html; charset=utf-8", nil)
		return
	}
	// Resolve the station info exactly as statxml does, for every source
	// Pascal's GetStationInfo knows: my stations, podcast episodes, radio
	// browser. Only the lookup differs; the stored item is the same shape.
	var vs vtuner.Station
	found := false
	switch {
	case len(id) >= 3 && id[:2] == stations.Prefix:
		if cat, st, ok := s.store.ByID(id); ok {
			vs, found = s.vtunerStation(cat, st, r), true
		}
	case len(id) >= 3 && id[:2] == podcasts.Prefix:
		if e, ok := s.pod.EpisodeByID(id); ok {
			vs, found = s.podStation(e, e.Description, r), true
		}
	case len(id) >= 3 && strings.HasPrefix(id, "RB_") && s.rb != nil:
		if st, ok := s.rbByID(id, r); ok && st.ID != "" {
			vs, found = s.rbStation(st, "", r), true
		}
	}
	if found {
		// Mime comes from the station info (RB codec), as Pascal's
		// SetVTunerStation(ARBStation) fills it; empty for MS/podcast items.
		it := bookmark.Item{ID: strings.Replace(vs.UID, "_", "B", 1), Name: vs.Name, URL: vs.URL, Desc: vs.Description,
			Logo: vs.Icon, Format: vs.Genre, Location: vs.Location, Bandwidth: vs.Bitrate, Mime: vs.Mime, Relia: "3", Bookmark: vs.Bookmark}
		s.bm.Set(mac, fav, it)
		// Pascal's wire answer here is exactly "200 OK / Status / Content-Type:
		// text/html" and a closed connection - no Content-Length, no Server,
		// no Date. net/http would add Content-Length: 0, so write it raw.
		if hj, ok := w.(http.Hijacker); ok {
			if conn, rw, err := hj.Hijack(); err == nil {
				_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nStatus: 200 OK\r\nContent-Type: text/html\r\n\r\n")
				_ = rw.Flush()
				_ = conn.Close()
				return
			}
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		return
	}
	s.display(w, msgNoStations)
}

// RelayResolve maps a station id to its stream URL for the relay listener.
func (s *Server) RelayResolve(id string) string {
	switch {
	case strings.HasPrefix(id, stations.Prefix+"_"):
		if _, st, ok := s.store.ByID(id); ok {
			return st.URL
		}
	case strings.HasPrefix(id, podcasts.Prefix+"_"):
		if e, ok := s.pod.EpisodeByID(id); ok {
			return e.URL
		}
	case strings.HasPrefix(id, "RB_") && s.rb != nil && len(id) >= 15:
		if st, ok := s.rb.ByID(id[3:15], s.avr, ""); ok {
			return st.URL
		}
	}
	return ""
}

// nameFn is the bookmark listing's ReplaceDiacritics step for the receiver in
// the request, or nil when none of its three flags is set.
func (s *Server) nameFn(r *http.Request) func(string) string {
	a, _ := s.avrFor(r)
	if a == nil {
		return nil
	}
	t := a.Translator
	if !(t.ReplaceLatin1Supplement || t.ReplaceExtA || t.ReplaceExtB) {
		return nil
	}
	return func(n string) string {
		return translator.ReplaceDiacritics(n, t.ReplaceLatin1Supplement, t.ReplaceExtA, t.ReplaceExtB)
	}
}
