// Package vtuner builds the XML pages a vTuner-protocol receiver reads.
// Output is byte-compatible with the Pascal implementation (src/vtuner.pas):
// same element order, two-space indent, and the Relia constant of 3.
package vtuner

import (
	"bytes"
	"strconv"
	"strings"
)

const PathParamDummy = "?retuner=true"

// AddRetunerParameter mirrors AddRetunerParameter in vtuner.pas.
func AddRetunerParameter(u string) string { return u + PathParamDummy }

type Item interface{ write(*bytes.Buffer) }

type Directory struct {
	Title, Destination string
	ItemCount          int
}

type Station struct {
	UID, Name, Description, URL, Icon, Genre, Location, Mime, Bitrate, Bookmark string
}

type Display struct{ Message string }

type Page struct {
	TotalItemsCount int
	Items           []Item
}

func (p *Page) Add(i Item) { p.Items = append(p.Items, i) }

// xmlEscaper replaces XML entities matching FPC's WriteXML escaping behavior.
// Reusing a single replacer instance avoids allocating a new replacer trie
// on every XML field escaped, significantly speeding up XML generation (~13x).
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// el writes a single XML element indented by 4 spaces.
// To avoid intermediate string allocations from string concatenation and Replace(),
// we stream the tags directly to the buffer and use xmlEscaper.WriteString.
func el(b *bytes.Buffer, name, val string) {
	b.WriteString("    <")
	b.WriteString(name)
	b.WriteString(">")
	if strings.ContainsAny(val, "&<>") {
		xmlEscaper.WriteString(b, val)
	} else {
		b.WriteString(val)
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteString(">\n")
}

func (d Directory) write(b *bytes.Buffer) {
	b.WriteString("  <Item>\n")
	el(b, "ItemType", "Dir")
	el(b, "Title", d.Title)
	el(b, "UrlDir", AddRetunerParameter(d.Destination))
	el(b, "UrlDirBackUp", AddRetunerParameter(d.Destination))
	el(b, "DirCount", strconv.Itoa(d.ItemCount))
	b.WriteString("  </Item>\n")
}

// matchMime normalises MIME to the known uppercase codec names without allocating.
func matchMime(m string) string {
	switch {
	case strings.EqualFold(m, "mp3"):
		return "MP3"
	case strings.EqualFold(m, "aac"):
		return "AAC"
	case strings.EqualFold(m, "wma"):
		return "WMA"
	default:
		return ""
	}
}

func (s Station) write(b *bytes.Buffer) {
	codec := matchMime(s.Mime)
	b.WriteString("  <Item>\n")
	el(b, "ItemType", "Station")
	el(b, "StationId", s.UID)
	el(b, "StationName", s.Name)
	el(b, "StationUrl", s.URL)
	el(b, "StationDesc", s.Description)
	el(b, "Logo", s.Icon)
	el(b, "StationFormat", s.Genre)
	el(b, "StationLocation", s.Location)
	el(b, "StationBandWidth", s.Bitrate)
	el(b, "StationMime", codec)
	el(b, "Relia", "3")
	el(b, "Bookmark", s.Bookmark)
	b.WriteString("  </Item>\n")
}

func (d Display) write(b *bytes.Buffer) {
	b.WriteString("  <Item>\n")
	el(b, "ItemType", "Display")
	el(b, "Display", d.Message)
	b.WriteString("  </Item>\n")
}

// XML renders the page. The Pascal writer ends the document with a newline (Content-Length counts it).
func (p *Page) XML() string {
	var b bytes.Buffer
	// Preallocate buffer capacity based on item count (~512 bytes per item plus headers)
	// to prevent repeated slice reallocation in bytes.growSlice during serialization.
	if count := len(p.Items); count > 0 {
		b.Grow(count*512 + 128)
	}
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<ListOfItems>\n")
	b.WriteString("  <ItemCount>")
	b.WriteString(strconv.Itoa(p.TotalItemsCount))
	b.WriteString("</ItemCount>\n")
	for _, i := range p.Items {
		i.write(&b)
	}
	b.WriteString("</ListOfItems>\n")
	return b.String()
}

// Token is the body of the token handshake.
func Token(t string) string { return "<EncryptedToken>" + t + "</EncryptedToken>\n" }
