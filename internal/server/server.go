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
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"retuner/internal/bookmark"
	"retuner/internal/config"
	"retuner/internal/stations"
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

	iconOnce  sync.Once
	iconBytes []byte
}

func New(cfg *config.Config, avr *config.AVR, urlHost string, store *stations.Store, bm *bookmark.Store) *Server {
	size := cfg.Int("Configuration", "IconSize", 200)
	if size <= 0 {
		size = 200
	}
	s := &Server{cfg: cfg, avr: avr, urlHost: urlHost, token: cfg.Str("Configuration", "MyToken"), iconSize: size,
		placehold: true, bookmarks: cfg.Bool("Bookmark", "Enable"), bm: bm, store: store, mystOn: cfg.Bool("MyStations", "Enable")}
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
				case "/" + root + "/play":
					s.play(w, r)
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
	if r.URL.Query().Get("token") == "0" {
		s.respond(w, http.StatusOK, "application/xml", []byte(vtuner.Token(s.token)))
		return
	}
	p := &vtuner.Page{}
	for _, mi := range s.avr.MainMenu {
		switch mi.Identifier {
		case "mystations":
			if s.mystOn {
				p.Add(s.dir(mi.Label, pathRoot+"/mystations", len(s.store.Snapshot())))
			}
		case "bookmarks":
			if s.bookmarks {
				p.Add(s.dir(mi.Label, pathRoot+"/bookmark", s.bm.Count(r.URL.Query().Get("mac"))))
			}
		case "about":
			p.Add(s.dir(mi.Label, pathRoot+"/about", 2))
		}
		// mystations, radiobrowser, local, podcasts: added in later stages.
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
	if len(id) >= 3 && id[2] == 'B' {
		body, ok := s.bm.StationInfo(r.URL.Query().Get("mac"), id)
		if !ok {
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
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) stripHTTPS(u string) string {
	// Protocol=all-as-http is the shipped default of avr.ini; with a common avr.ini
	// (CommonAVRini=1, the default) it applies to every receiver.
	if s.avr.AllAsHTTP {
		for _, p := range []string{"https://", "HTTPS://", "Https://"} {
			if strings.HasPrefix(strings.ToLower(u), strings.ToLower(p)) {
				return "http://" + u[len(p):]
			}
		}
	}
	return u
}

func (s *Server) vtunerStation(cat string, st stations.Station, r *http.Request) vtuner.Station {
	return vtuner.Station{
		UID: st.ID, Name: st.Name,
		Description: "My favorite \"" + st.Name + "\"",
		URL:         s.stripHTTPS(st.URL),
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
			target = s.stripHTTPS(st.URL)
		}
	}
	if target == "" {
		s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		return
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

func (s *Server) bookmarkList(w http.ResponseWriter, r *http.Request) {
	mac := r.URL.Query().Get("mac")
	first, last := pageRange(r.URL.Query(), s.bm.Count(mac))
	body, ok := s.bm.Page(mac, first, last)
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
	if len(id) >= 3 && id[:2] == stations.Prefix {
		if cat, st, ok := s.store.ByID(id); ok {
			vs := s.vtunerStation(cat, st, r)
			it := bookmark.Item{ID: strings.Replace(vs.UID, "_", "B", 1), Name: vs.Name, URL: vs.URL, Desc: vs.Description,
				Logo: vs.Icon, Format: vs.Genre, Location: vs.Location, Bandwidth: vs.Bitrate, Mime: "", Relia: "3", Bookmark: vs.Bookmark}
			s.bm.Set(q.Get("mac"), q.Get("fav"), it)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	s.display(w, msgNoStations)
}
