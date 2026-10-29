package bookmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	return &Store{Dir: t.TempDir(), Common: true, Limit: 2, URLHost: "10.0.0.5"}
}

func item(id string) Item {
	return Item{ID: id, Name: "n", URL: "http://u", Logo: "http://10.0.0.5/retuner/icon?id=x", Relia: "3",
		Bookmark: "http://10.0.0.5/setupapp/favxml.asp?id=x&fav=add"}
}

func TestAddStoresPortableHostAndDelLink(t *testing.T) {
	s := newStore(t)
	s.Set("m", "add", item("MSB1"))
	raw, _ := os.ReadFile(filepath.Join(s.Dir, "bookmark.xml"))
	x := string(raw)
	if !strings.Contains(x, "http://retunerhost/retuner/icon") || !strings.Contains(x, "fav=del") || strings.Contains(x, "10.0.0.5") {
		t.Fatal(x)
	}
	if s.Count("m") != 1 {
		t.Fatal("count")
	}
}

func TestLimitDuplicateAndLastDeleteRemovesFile(t *testing.T) {
	s := newStore(t)
	s.Set("m", "add", item("MSB1"))
	s.Set("m", "add", item("MSB1")) // duplicate ignored
	s.Set("m", "add", item("MSB2"))
	s.Set("m", "add", item("MSB3")) // over the limit of 2
	if s.Count("m") != 2 {
		t.Fatalf("count %d", s.Count("m"))
	}
	s.Set("m", "del", item("MSB1"))
	s.Set("m", "del", item("MSB2"))
	if _, err := os.Stat(filepath.Join(s.Dir, "bookmark.xml")); err == nil {
		t.Fatal("file should be gone after the last delete")
	}
}

func TestServedHostIsSubstituted(t *testing.T) {
	s := newStore(t)
	s.Set("m", "add", item("MSB1"))
	body, ok := s.StationInfo("m", "MSB1")
	if !ok || !strings.Contains(string(body), "http://10.0.0.5/retuner/icon") {
		t.Fatal(string(body))
	}
}

func TestUnsafeMACNeverEscapesConfigDir(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Limit: 5}
	if p := s.path("../../etc/x"); filepath.Dir(p) != s.Dir {
		t.Fatal(p)
	}
}

// Pascal's IsSafeAVRMAC accepts 1..64 of [A-Za-z0-9_-]; a Denon sends 32 hex.
// Those must keep their own file, and anything else must stay inside Dir.
func TestPerMACFileNames(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Limit: 5, URLHost: "10.0.0.5"}
	hex32 := strings.Repeat("ab", 16)
	long64 := strings.Repeat("c", 64)
	for _, mac := range []string{hex32, long64, "../../evil", "aa:bb", strings.Repeat("d", 65)} {
		s.Set(mac, "add", item("MSB1"))
	}
	for _, want := range []string{hex32 + ".xml", long64 + ".xml"} {
		if _, err := os.Stat(filepath.Join(s.Dir, want)); err != nil {
			t.Errorf("missing %s", want)
		}
	}
	ents, _ := os.ReadDir(s.Dir)
	for _, e := range ents {
		t.Log(e.Name())
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "bookmark.xml")); err != nil {
		t.Error("unsafe MACs should share bookmark.xml")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "..", "evil.xml")); err == nil {
		t.Error("escaped Dir")
	}
}
