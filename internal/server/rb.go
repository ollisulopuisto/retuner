package server

import (
	"log"
	"net/http"
	"regexp"
	"strings"

	"retuner/internal/config"
	"retuner/internal/radiobrowser"
	"retuner/internal/stations"
	"retuner/internal/vtuner"
)

const maxMACLen = 64

var safeMAC = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// avrFor returns the receiver config for this request: the common avr.ini, or
// with CommonAVRini=0 the <mac>.ini of the ?mac= device (read on first sight).
// An unusable identifier falls back to the common config, like the Pascal build.
// The second value is the cache key (the MAC, or "" for the common config).
func (s *Server) avrFor(r *http.Request) (*config.AVR, string) {
	if s.commonAVR {
		return s.avr, ""
	}
	mac := strings.TrimSpace(r.URL.Query().Get("mac"))
	if mac == "" || len(mac) > maxMACLen || !safeMAC.MatchString(mac) {
		return s.avr, ""
	}
	s.avrMu.Lock()
	defer s.avrMu.Unlock()
	if a, ok := s.avrs[mac]; ok {
		return a, mac
	}
	log.Printf("New AVR connected (%s)", mac)
	a, err := s.cfg.LoadAVRFile(mac + ".ini")
	if err != nil {
		log.Printf("%s.ini: %v", mac, err)
		return s.avr, ""
	}
	s.avrs[mac] = a
	return a, mac
}

var rbKinds = []string{"genre", "country", "language", "popular"}

func (s *Server) rbUID(st radiobrowser.Station) string {
	id := strings.ToUpper(strings.ReplaceAll(st.ID, "-", ""))
	if len(id) > 12 {
		id = id[:12]
	}
	return "RB_" + id
}

// rbByID looks a "RB_xxxxxxxxxxxx" id up for the receiver in the request.
func (s *Server) rbByID(id string, r *http.Request) (radiobrowser.Station, bool) {
	if s.rb == nil || len(id) < 15 || !strings.HasPrefix(id, "RB_") {
		return radiobrowser.Station{}, false
	}
	a, mac := s.avrFor(r)
	return s.rb.ByID(id[3:15], a, mac)
}

func (s *Server) rbStation(st radiobrowser.Station, genre string, r *http.Request) vtuner.Station {
	uid := s.rbUID(st)
	g := st.Tags
	if g == "" {
		g = genre
	}
	return vtuner.Station{
		UID: uid, Name: st.Name, Description: st.Name + " : " + st.Homepage, Location: st.Country,
		URL: s.stripHTTPS(st.URL, r), Genre: g, Mime: strings.ToUpper(st.Codec), Bitrate: st.Bitrate,
		Icon:     "http://" + s.urlHost + "/" + pathRoot + "/icon?id=" + uid,
		Bookmark: "http://" + s.urlHost + "/setupapp/favxml.asp?id=" + uid + "&fav=add",
	}
}

func (s *Server) rbRange(r *http.Request) (start, howMany int) {
	q := r.URL.Query()
	atoi := func(k string) (int, bool) { n, ok := parseInt(q.Get(k)); return n, ok }
	st, ok1 := atoi("start")
	hm, ok2 := atoi("howmany")
	if !(ok1 && ok2) {
		st, ok1 = atoi("startitems")
		hm, ok2 = atoi("enditems")
		if ok1 && ok2 {
			if hm < st {
				hm = 1
			} else {
				hm = hm - st + 1
			}
		} else {
			st, hm = 0, -1
		}
	}
	st--
	if st < 0 {
		st = 0
	}
	return st, hm
}

func parseInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, neg := 0, false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

func (s *Server) rbRoot(w http.ResponseWriter, r *http.Request) {
	a, mac := s.avrFor(r)
	count := func(k radiobrowser.Kind) int { _, n := s.rb.Categories(k, a, mac, 0, 1); return n }
	popular := s.rb.PopLimit
	if s.rb.DB != nil {
		if g, c, l, pop, ok := s.rb.DB.RootCounts(a, mac); ok {
			count = func(k radiobrowser.Kind) int { return [...]int{g, c, l}[k] }
			popular = pop
		}
	}
	p := &vtuner.Page{TotalItemsCount: 4}
	p.Add(s.dir("Genres", pathRoot+"/radiobrowser/genre", count(radiobrowser.Genre)))
	p.Add(s.dir("Countries", pathRoot+"/radiobrowser/country", count(radiobrowser.Country)))
	p.Add(s.dir("Languages", pathRoot+"/radiobrowser/language", count(radiobrowser.Language)))
	p.Add(s.dir("Most popular", pathRoot+"/radiobrowser/popular", popular))
	s.page(w, p)
}

func kindIndex(name string) int {
	for i, k := range rbKinds {
		if strings.EqualFold(k, name) {
			return i
		}
	}
	return -1
}

// rbCategory serves /radiobrowser/<type> and /radiobrowser/<type>/<category>.
func (s *Server) rbCategory(w http.ResponseWriter, r *http.Request, typ, cat string, hasCat bool) {
	idx := kindIndex(typ)
	a, mac := s.avrFor(r)
	start, howMany := s.rbRange(r)
	if !hasCat {
		switch {
		case idx >= 0 && idx <= 2:
			cats, total := s.rb.Categories(radiobrowser.Kind(idx), a, mac, start, howMany)
			if total <= 0 {
				log.Printf("No station(s) found for this category")
				s.display(w, msgNoStations+" for this category")
				return
			}
			p := &vtuner.Page{TotalItemsCount: total}
			for _, c := range cats {
				p.Add(s.dir(c.Name, pathRoot+"/radiobrowser/"+rbKinds[idx]+"/"+urlEncode(c.Name), c.Count))
			}
			s.page(w, p)
			return
		case idx == 3:
			cat = ""
		default:
			// The Pascal handler logs and sends nothing: an empty 200 text/html.
			log.Printf("Radiobrowser category type not supported ?!")
			s.respond(w, http.StatusOK, "text/html", nil)
			return
		}
	}
	if idx < 0 {
		s.respond(w, http.StatusNotFound, "text/html; charset=utf-8", nil)
		return
	}
	sts, total := s.rb.Stations(radiobrowser.Kind(idx), cat, a, mac, start, howMany)
	s.rbStations(w, r, sts, total, httpDecode(cat))
}

func (s *Server) rbStations(w http.ResponseWriter, r *http.Request, sts []radiobrowser.Station, total int, routeCat string) {
	if total <= 0 {
		msg := msgNoStations
		log.Printf("%s", msg+map[bool]string{true: "", false: " : " + routeCat}[routeCat == ""])
		s.display(w, msg)
		return
	}
	p := &vtuner.Page{TotalItemsCount: total}
	for _, st := range sts {
		p.Add(s.rbStation(st, routeCat, r))
	}
	s.page(w, p)
}

// search handles search.asp / /search: sSearchtype=3 means "play" on some
// receivers, anything else is a station search.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := q.Get("search")
	if q.Get("sSearchtype") == "3" {
		id := ""
		switch {
		case len(text) >= 2 && (text[:2] == stations.Prefix || text[:2] == "RB"):
			id = text
		case s.bm != nil:
			dev := q.Get("mac")
			if dev == "" {
				dev = r.Host
			}
			if s.bm.HasFile(dev) {
				id = "UNB" + text
				q.Set("mac", dev)
			} else if g := s.store.Snapshot(); s.mystOn && len(g) > 0 && len(g[0].Stations) > 0 {
				id = g[0].Stations[0].ID
			}
		}
		q.Set("id", id)
		r.URL.RawQuery = q.Encode()
		s.station(w, r)
		return
	}
	if len(text) < 3 {
		s.display(w, "Search query too short.")
		return
	}
	a, mac := s.avrFor(r)
	start, howMany := s.rbRange(r)
	sts, total := s.rb.Stations(radiobrowser.Search, text, a, mac, start, howMany)
	s.rbStations(w, r, sts, total, "")
}
