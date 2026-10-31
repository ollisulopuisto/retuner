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

var validMimes = map[string]string{"mp3": "MP3", "aac": "AAC", "wma": "WMA"}

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

func esc(s string) string {
	// FPC's WriteXML escapes & < > in text and leaves quotes alone; encoding/xml
	// would write &#34; for a quote, which the parity diff caught.
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func el(b *bytes.Buffer, name, val string) {
	b.WriteString("    <" + name + ">" + esc(val) + "</" + name + ">\n")
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

func (s Station) write(b *bytes.Buffer) {
	codec := validMimes[lower(s.Mime)]
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

func lower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}

// XML renders the page. The Pascal writer ends the document with a newline (Content-Length counts it).
func (p *Page) XML() string {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<ListOfItems>\n")
	b.WriteString("  <ItemCount>" + strconv.Itoa(p.TotalItemsCount) + "</ItemCount>\n")
	for _, i := range p.Items {
		i.write(&b)
	}
	b.WriteString("</ListOfItems>\n")
	return b.String()
}

// Token is the body of the token handshake.
func Token(t string) string { return "<EncryptedToken>" + t + "</EncryptedToken>\n" }
