package radiobrowser

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"retuner/internal/config"
)

// catFile: every station or category list is one file in the cache folder,
// "<RBS|RBC>-<mac>-<kind>[-<hash>]-<crc of avr ini>.cache", in the Pascal
// serialisation (SerializeRBObjects): 'S' or 'C', a uint32 count, then per
// station twelve length-prefixed (uint16) strings, per category one such
// string and an int32 count; all little endian.
//
// The category files are named exactly as Pascal names them. The station
// files carry a hash of the category or search text, and Pascal's is
// string.GetHashCode (not reproduced), so Go uses its own hash there: the
// formats are shared, those file names are not.

func (c *Client) fileMode() bool { return c.CacheType == "catFile" && c.CacheDir != "" }

func (c *Client) filePath(prefix, mac string, a *config.AVR, rest string) string {
	if mac == "" {
		mac = "avr"
	}
	return filepath.Join(c.CacheDir, prefix+"-"+mac+"-"+rest+"-"+strconv.FormatUint(uint64(a.FileCRC), 10)+".cache")
}

func (c *Client) fresh(p string) ([]byte, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	if c.TTL > 0 && time.Since(st.ModTime()) > c.TTL {
		log.Printf("Radio-browser.info: catFile cache TTL expired. Reloading %s", filepath.Base(p))
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

type fr struct {
	b   []byte
	bad bool
}

func (r *fr) u16() int {
	if len(r.b) < 2 {
		r.bad = true
		return 0
	}
	n := int(binary.LittleEndian.Uint16(r.b))
	r.b = r.b[2:]
	return n
}
func (r *fr) str() string {
	n := r.u16()
	if r.bad || len(r.b) < n {
		r.bad = true
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}
func (r *fr) u32() uint32 {
	if len(r.b) < 4 {
		r.bad = true
		return 0
	}
	n := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return n
}

func decodeStations(b []byte) ([]Station, bool) {
	if len(b) < 5 || b[0] != 'S' {
		return nil, false
	}
	r := &fr{b: b[1:]}
	n := r.u32()
	var out []Station
	for i := uint32(0); i < n && !r.bad; i++ {
		out = append(out, Station{ID: r.str(), Name: r.str(), Homepage: r.str(), URL: r.str(), Icon: r.str(), Tags: r.str(),
			Country: r.str(), CountryCode: r.str(), Language: r.str(), Votes: r.str(), Codec: r.str(), Bitrate: r.str()})
	}
	return out, !r.bad && len(out) > 0
}

func decodeCategories(b []byte) ([]Category, bool) {
	if len(b) < 5 || b[0] != 'C' {
		return nil, false
	}
	r := &fr{b: b[1:]}
	n := r.u32()
	var out []Category
	for i := uint32(0); i < n && !r.bad; i++ {
		name := r.str()
		out = append(out, Category{name, int(int32(r.u32()))})
	}
	return out, !r.bad && len(out) > 0
}

func putStr(b *bytes.Buffer, s string) bool {
	if len(s) > 0xFFFF {
		return false
	}
	_ = binary.Write(b, binary.LittleEndian, uint16(len(s)))
	b.WriteString(s)
	return true
}

func encodeStations(sts []Station) ([]byte, bool) {
	var b bytes.Buffer
	b.WriteByte('S')
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(sts)))
	for _, s := range sts {
		for _, v := range []string{s.ID, s.Name, s.Homepage, s.URL, s.Icon, s.Tags, s.Country, s.CountryCode, s.Language, s.Votes, s.Codec, s.Bitrate} {
			if !putStr(&b, v) {
				return nil, false
			}
		}
	}
	return b.Bytes(), true
}

func encodeCategories(cs []Category) ([]byte, bool) {
	var b bytes.Buffer
	b.WriteByte('C')
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(cs)))
	for _, c := range cs {
		if !putStr(&b, c.Name) {
			return nil, false
		}
		_ = binary.Write(&b, binary.LittleEndian, int32(c.Count))
	}
	return b.Bytes(), true
}

func (c *Client) saveFile(p string, b []byte, ok bool) {
	if !ok || len(b) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		err = os.WriteFile(p, b, 0o644)
		if err != nil {
			log.Printf("Radio-browser.info: catFile cache save error (%v)", err)
		}
	}
}

func (c *Client) fileStations(mac string, a *config.AVR, k Kind, name string) (string, []Station, bool) {
	p := c.filePath("RBS", mac, a, strconv.Itoa(int(k))+"-"+hash(name))
	if b, ok := c.fresh(p); ok {
		if s, ok := decodeStations(b); ok {
			return p, s, true
		}
		log.Printf("Radio-browser.info: catFile cache empty or unreadable %s", filepath.Base(p))
	}
	return p, nil, false
}

func (c *Client) fileCategories(mac string, a *config.AVR, k Kind) (string, []Category, bool) {
	p := c.filePath("RBC", mac, a, strconv.Itoa(int(k)))
	if b, ok := c.fresh(p); ok {
		if s, ok := decodeCategories(b); ok {
			return p, s, true
		}
		log.Printf("Radio-browser.info: catFile cache empty or unreadable %s", filepath.Base(p))
	}
	return p, nil, false
}

var _ = strings.ToLower

// fromListFiles finds a station in the receiver's cached station lists on disk.
func (c *Client) fromListFiles(uuid, mac string, a *config.AVR) (Station, bool) {
	if mac == "" {
		mac = "avr"
	}
	files, _ := filepath.Glob(filepath.Join(c.CacheDir, "RBS-"+mac+"-*-"+strconv.FormatUint(uint64(a.FileCRC), 10)+".cache"))
	for _, p := range files {
		b, ok := c.fresh(p)
		if !ok {
			continue
		}
		if sts, ok := decodeStations(b); ok {
			for _, st := range sts {
				if st.ID == uuid {
					return st, true
				}
			}
		}
	}
	return Station{}, false
}
