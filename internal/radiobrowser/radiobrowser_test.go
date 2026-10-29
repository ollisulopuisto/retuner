package radiobrowser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"retuner/internal/config"
)

func TestWildAndLists(t *testing.T) {
	for _, c := range []struct {
		s, p string
		want bool
	}{{"Rock", "rock", true}, {"Hard Rock", "*rock", true}, {"Rocket", "rock", false}, {"ab", "a?", true}, {"x.y", "x.y", true}, {"xzy", "x.y", false}} {
		if wild(c.s, c.p) != c.want {
			t.Errorf("wild(%q,%q)", c.s, c.p)
		}
	}
	if !haveCommon("pop, rock/jazz", []string{"ROCK"}) || haveCommon("pop", []string{"rock"}) {
		t.Error("haveCommon")
	}
	if !containsIn("Some Ad Radio", []string{"ad radio"}) {
		t.Error("containsIn")
	}
}

func TestStationFilter(t *testing.T) {
	a := &config.AVR{Protocol: "https", BitrateMax: 128, NotAllowedInName: []string{"x-rated"}}
	mk := func(js string) map[string]any {
		m := parseArray([]byte("[" + js + "]"))
		return m[0]
	}
	cases := []struct {
		js   string
		want bool
	}{
		{`{"url":"https://a/b","bitrate":128}`, true},
		{`{"url":"http://a/b","bitrate":64}`, false},    // protocol https only
		{`{"url":"https://a/b","bitrate":129}`, false},  // over the limit
		{`{"url":"https://a/b"}`, true},                 // no bitrate
		{`{"url":"https://a/b","bitrate":"999"}`, true}, // string bitrate reads as 0
		{`{"url":"https://a/b","name":"My X-Rated FM"}`, false},
		{`{"url":"https://x-rated/b","name":"Fine"}`, true}, // name only, not the url
	}
	for _, c := range cases {
		if got := stationAllowed(mk(c.js), Country, a); got != c.want {
			t.Errorf("%s: got %v", c.js, got)
		}
	}
}

func TestCacheStopsSecondRequestAndNeverCachesEmpty(t *testing.T) {
	var hits int32
	body := `[{"stationuuid":"0000000a-1111-2222-3333-444444444444","name":"A (b)","url":"http://x/s;","tags":"t","country":"C","codec":"mp3","bitrate":96}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "t", 100, 1, "catMemStr", 0)
	a := &config.AVR{Order: "name"}
	for i := 0; i < 2; i++ {
		sts, n := c.Stations(Country, "C", a, "", 0, -1)
		if n != 1 || sts[0].Name != "A b" || sts[0].URL != "http://x/s;" {
			t.Fatalf("%v %d", sts, n)
		}
	}
	if hits != 1 {
		t.Fatalf("%d upstream requests for two identical lists", hits)
	}
	// A listed station is found again by its 12 character id without a request.
	if st, ok := c.ByID("0000000a1111", ""); !ok || st.Name != "A b" {
		t.Fatal("by id from cache")
	}
	if hits != 2 { // only the uuid index was fetched
		t.Fatalf("hits %d", hits)
	}
	body = `[]`
	if _, n := c.Stations(Genre, "none", a, "", 0, -1); n != 0 {
		t.Fatal("empty")
	}
	if _, n := c.Stations(Genre, "none", a, "", 0, -1); n != 0 || hits != 4 {
		t.Fatalf("an empty answer must not be cached (hits %d)", hits)
	}
}

func TestCatNoneAlwaysAsksUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`[{"stationuuid":"u","name":"n","url":"http://x"}]`))
	}))
	defer srv.Close()
	c := New(srv.URL, "t", 100, 1, "catNone", 0)
	c.Stations(Popular, "", &config.AVR{}, "", 0, -1)
	c.Stations(Popular, "", &config.AVR{}, "", 0, -1)
	if hits != 2 {
		t.Fatal(hits)
	}
}

func TestUnreachableNamesURL(t *testing.T) {
	c := New("http://127.0.0.1:1", "t", 100, 1, "catMemStr", 0)
	if _, n := c.Stations(Search, "abc", &config.AVR{}, "", 0, -1); n != 0 {
		t.Fatal("no stations expected")
	}
	if !strings.Contains(describe(errString("Get \"http://127.0.0.1:1/json/x\": dial tcp 127.0.0.1:1: connect: connection refused")), "Connect to 127.0.0.1:1") {
		t.Fatal("describe")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
