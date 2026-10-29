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
	"strings"
	"sync"

	"retuner/internal/config"
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

	iconOnce  sync.Once
	iconBytes []byte
}

func New(cfg *config.Config, avr *config.AVR, urlHost string) *Server {
	size := cfg.Int("Configuration", "IconSize", 200)
	if size <= 0 {
		size = 200
	}
	s := &Server{cfg: cfg, avr: avr, urlHost: urlHost, token: cfg.Str("Configuration", "MyToken"), iconSize: size,
		placehold: true, bookmarks: cfg.Bool("Bookmark", "Enable")}
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
			s.display(w, msgNoStations) // bookmark service: station lookup lands in a later stage
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
					s.display(w, msgNoStations)
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
		case "bookmarks":
			if s.bookmarks {
				p.Add(s.dir(mi.Label, pathRoot+"/bookmark", 0))
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

func (s *Server) station(w http.ResponseWriter, r *http.Request) {
	// Ids are resolved in later stages; every id is currently "not found",
	// which is what the Pascal build answers for an unknown or malformed id.
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
