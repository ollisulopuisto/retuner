package vtuner

import (
	"strings"
	"testing"
)

// Golden strings are the bytes the Pascal 26.08.28.4 build returns.
func TestTokenMatchesPascal(t *testing.T) {
	if got := Token("0123456789ABCDEF"); got != "<EncryptedToken>0123456789ABCDEF</EncryptedToken>\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayPageMatchesPascal(t *testing.T) {
	p := &Page{TotalItemsCount: 1}
	p.Add(Display{Message: "No station(s) found"})
	want := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<ListOfItems>\n  <ItemCount>1</ItemCount>\n  <Item>\n    <ItemType>Display</ItemType>\n    <Display>No station(s) found</Display>\n  </Item>\n</ListOfItems>\n"
	if got := p.XML(); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestStationMimeOnlyKnownCodecs(t *testing.T) {
	p := &Page{TotalItemsCount: 1}
	p.Add(Station{UID: "MS_X", Name: "A & B", Mime: "ogg"})
	x := p.XML()
	if !strings.Contains(x, "<StationMime></StationMime>") || !strings.Contains(x, "A &amp; B") || !strings.Contains(x, "<Relia>3</Relia>") {
		t.Fatalf("unexpected: %s", x)
	}
	p = &Page{TotalItemsCount: 1}
	p.Add(Station{Mime: "mp3"})
	if !strings.Contains(p.XML(), "<StationMime>MP3</StationMime>") {
		t.Fatal("mp3 should normalise to MP3")
	}
}

func TestDirectoryAddsRetunerParameter(t *testing.T) {
	p := &Page{TotalItemsCount: 1}
	p.Add(Directory{Title: "t", Destination: "http://h/retuner/about", ItemCount: 2})
	if !strings.Contains(p.XML(), "<UrlDir>http://h/retuner/about?retuner=true</UrlDir>") {
		t.Fatal(p.XML())
	}
}
