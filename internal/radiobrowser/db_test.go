package radiobrowser

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"retuner/internal/config"
)

func dbMock(t *testing.T) (*httptest.Server, *int64) {
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&n, 1)
		switch r.URL.Path {
		case "/json/countries":
			w.Write([]byte(`[{"name":"Testland","stationcount":2}]`))
		case "/json/languages":
			w.Write([]byte(`[{"name":"testish","stationcount":2}]`))
		case "/json/codecs":
			w.Write([]byte(`[{"name":"MP3","stationcount":2}]`))
		case "/json/tags":
			w.Write([]byte(`[{"name":"jazz","stationcount":2}]`))
		case "/json/stations":
			w.Write([]byte(`[{"stationuuid":"00000001-0000-0000-0000-000000000001","name":"Jazz O'One","url":"http://e.com/a","url_resolved":"http://e.com/a","homepage":"","favicon":"","tags":"jazz","country":"Testland","language":"testish","votes":5,"codec":"MP3","bitrate":128,"lastcheckok":1},{"stationuuid":"00000002-0000-0000-0000-000000000002","name":"Two","url":"http://e.com/b","url_resolved":"http://e.com/b","homepage":"","favicon":"","tags":"jazz","country":"Testland","language":"testish","votes":9,"codec":"MP3","bitrate":64,"lastcheckok":1}]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func newDBClient(api, typ string) *Client { return New(api, "test", 100, 1, typ, 0) }

func avrNoFilter() *config.AVR { return &config.AVR{Order: "name", FileCRC: 1} }

func TestDBCacheTypes(t *testing.T) {
	for _, typ := range []string{"catDB", "catMemDB", "catPermMemDB"} {
		t.Run(typ, func(t *testing.T) {
			srv, n := dbMock(t)
			dir := t.TempDir()
			c := newDBClient(srv.URL, typ)
			db, err := c.PrepareDB(dir, typ, 0)
			if err != nil {
				t.Fatal(err)
			}
			c.DB = db
			build := atomic.LoadInt64(n)
			if build != 5 {
				t.Fatalf("build should make 5 upstream requests, made %d", build)
			}
			a := avrNoFilter()
			st, total := c.Stations(Genre, "jazz", a, "AA", 0, 10)
			if total != 2 || len(st) != 2 {
				t.Fatalf("stations: %d %v", total, st)
			}
			if st[0].Name != "Jazz OOne" { // FIX_DATA script drops the apostrophe
				t.Fatalf("name handling: %q", st[0].Name)
			}
			if cats, tot := c.Categories(Country, a, "AA", 0, 10); tot != 1 || cats[0].Name != "Testland" {
				t.Fatalf("categories: %v %d", cats, tot)
			}
			if s, ok := c.ByID("000000020000", a, "AA"); !ok || s.Name != "Two" {
				t.Fatalf("by id: %v %v", s, ok)
			}
			if pop, _ := c.Stations(Popular, "", a, "AA", 0, 10); len(pop) != 2 || pop[0].Name != "Two" {
				t.Fatalf("popular order: %v", pop)
			}
			if got, _ := c.Stations(Search, "wo", a, "AA", 0, 10); len(got) != 1 {
				t.Fatalf("search: %v", got)
			}
			if atomic.LoadInt64(n) != build {
				t.Fatalf("queries must not reach upstream (%d -> %d)", build, atomic.LoadInt64(n))
			}
			_, e := os.Stat(filepath.Join(dir, "rb.db"))
			if (typ == "catMemDB") == (e == nil) {
				t.Fatalf("rb.db presence wrong for %s: %v", typ, e)
			}
			// restart: persistent types reuse the file with no upstream traffic (TTL 0 = fresh)
			c2 := newDBClient(srv.URL, typ)
			db2, err := c2.PrepareDB(dir, typ, 0)
			if err != nil {
				t.Fatal(err)
			}
			c2.DB = db2
			want := build
			if typ == "catMemDB" {
				want = build * 2
			}
			if atomic.LoadInt64(n) != want {
				t.Fatalf("restart upstream requests: got %d want %d", atomic.LoadInt64(n), want)
			}
			if _, tot := c2.Stations(Genre, "jazz", a, "AA", 0, 10); tot != 2 && typ != "catMemDB" {
				t.Fatalf("restored data missing")
			}
		})
	}
}

func TestDBTTLRebuildAndOffline(t *testing.T) {
	srv, n := dbMock(t)
	dir := t.TempDir()
	c := newDBClient(srv.URL, "catPermMemDB")
	if _, err := c.PrepareDB(dir, "catPermMemDB", 0); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "rb.db"), old, old)
	before := atomic.LoadInt64(n)
	c2 := newDBClient(srv.URL, "catPermMemDB")
	if _, err := c2.PrepareDB(dir, "catPermMemDB", 1); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt64(n) != before+5 {
		t.Fatalf("expired database must be rebuilt")
	}
	// offline with a fresh file: serve from it, no traffic
	srv.Close()
	c3 := newDBClient(srv.URL, "catPermMemDB")
	db, err := c3.PrepareDB(dir, "catPermMemDB", 1)
	if err != nil {
		t.Fatal(err)
	}
	c3.DB = db
	if _, tot := c3.Stations(Genre, "jazz", avrNoFilter(), "AA", 0, 10); tot != 2 {
		t.Fatalf("offline restore failed")
	}
}

func TestDBBuildFailureIsAnError(t *testing.T) {
	srv, _ := dbMock(t)
	srv.Close()
	c := newDBClient(srv.URL, "catDB")
	if _, err := c.PrepareDB(t.TempDir(), "catDB", 0); err == nil {
		t.Fatal("unreachable API with no database must fail, not fall back")
	}
}
