package server

import (
	"log"
	"net/http"

	"retuner/internal/podcasts"
	"retuner/internal/vtuner"
)

func (s *Server) podOn() bool { return s.cfg.Bool("Podcasts", "Enable") }

func (s *Server) podcastFeeds(w http.ResponseWriter, r *http.Request) {
	feeds := s.pod.Feeds()
	if len(feeds) == 0 {
		log.Printf("No podcast feeds configured")
		s.display(w, "No podcast feeds configured")
		return
	}
	first, last := pageRange(r.URL.Query(), len(feeds))
	p := &vtuner.Page{TotalItemsCount: len(feeds)}
	for i := first; i <= last && i < len(feeds); i++ {
		p.Add(s.dir(feeds[i].Name, pathRoot+"/podcasts/"+feeds[i].ID, s.pod.Limit))
	}
	s.page(w, p)
}

func (s *Server) podcastEpisodes(w http.ResponseWriter, r *http.Request, feedID string) {
	f, ok := s.pod.FeedByID(feedID)
	if !ok {
		log.Printf("Podcast feed not found %s", feedID)
		s.display(w, "Podcast feed not found")
		return
	}
	eps, ok := s.pod.Episodes(f)
	if !ok {
		log.Printf("No episodes found %s", f.Name)
		s.display(w, "No episodes found")
		return
	}
	first, last := pageRange(r.URL.Query(), len(eps))
	p := &vtuner.Page{TotalItemsCount: len(eps)}
	for i := first; i <= last && i < len(eps); i++ {
		p.Add(s.podStation(eps[i], f.Name, r))
	}
	s.page(w, p)
}

// podStation: an episode is a station whose URL is the enclosure.
func (s *Server) podStation(e podcasts.Episode, feedName string, r *http.Request) vtuner.Station {
	desc := feedName
	if e.Date != "" {
		desc = feedName + " - " + e.Date
	}
	return vtuner.Station{
		UID: e.ID, Name: e.Title, Description: desc, Genre: feedName,
		URL:      s.listURL(e.URL, e.ID, r),
		Icon:     "http://" + s.urlHost + "/" + pathRoot + "/icon?id=" + e.ID,
		Bookmark: "http://" + s.urlHost + "/setupapp/favxml.asp?id=" + e.ID + "&fav=add",
	}
}
