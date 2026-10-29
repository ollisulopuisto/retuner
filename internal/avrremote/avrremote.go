// Package avrremote drives a Denon/Marantz receiver's own net-audio menu over its
// web interface (src/avrremote.pas). Behaviour, wire format and messages follow
// the Pascal unit; error texts that quote a network exception differ in wording.
package avrremote

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"retuner/internal/fpjson"
)

const (
	statusPath   = "/goform/formNetAudio_StatusXml.xml"
	putPath      = "/NetAudio/index.put.asp"
	cmdPrefix    = "PutNetAudioCommand/"
	searchPrefix = "PutNetFuncSearchiRadio/"
	updateCmd    = "aspMainZone_WebUpdateStatus/"
	Lines        = 10
	flagSelect   = 1
	flagCursor   = 8
	maxQuery     = 30
	noAddress    = "No receiver address configured. Set RemoteAVRAddress in retuner.ini."
	noList       = "The receiver did not return a list."
)

var allowed = map[string]bool{"CurUp": true, "CurDown": true, "CurLeft": true, "CurRight": true, "CurEnter": true,
	"CmdPageUp": true, "CmdPageDown": true, "CmdStop": true, "PresetCall": true, "PresetMemo": true}

type Remote struct {
	Address string
	client  *http.Client
}

func New(addr string) *Remote {
	return &Remote{Address: addr, client: &http.Client{Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (r *Remote) base() string {
	b := strings.TrimSpace(r.Address)
	if b == "" {
		return ""
	}
	if !strings.HasPrefix(b, "http://") && !strings.HasPrefix(b, "https://") {
		b = "http://" + b
	}
	return strings.TrimRight(b, "/")
}

func errJSON(m string) string {
	return fpjson.Marshal(struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{false, m})
}

func section(x, tag string) string {
	s := strings.Index(x, "<"+tag+">")
	if s < 0 {
		return ""
	}
	s += len(tag) + 2
	e := strings.Index(x[s:], "</"+tag+">")
	if e < 0 {
		return ""
	}
	return x[s : s+e]
}

func values(sec string) []string {
	var out []string
	for len(out) < Lines {
		s := strings.Index(sec, "<value>")
		if s < 0 {
			break
		}
		sec = sec[s+7:]
		e := strings.Index(sec, "</value>")
		if e < 0 {
			break
		}
		out = append(out, sec[:e])
		sec = sec[e+8:]
	}
	return out
}

func xmlDecode(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}

func pageCounter(t string) (string, bool) {
	o, c := strings.Index(t, "["), strings.Index(t, "]")
	if o < 0 || c < o {
		return "", false
	}
	in := strings.TrimSpace(t[o+1 : c])
	return in, strings.Contains(in, "/")
}

func (r *Remote) fetchStatus() (string, error) {
	resp, err := r.client.Get(r.base() + statusPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Unexpected response status code: %d", resp.StatusCode)
	}
	return string(b), nil
}

func (r *Remote) unreachable(err error) string {
	return "Cannot reach the receiver at " + r.Address + " (" + err.Error() + ")."
}

func (r *Remote) Status() string {
	if r.base() == "" {
		return errJSON(noAddress)
	}
	x, err := r.fetchStatus()
	if err != nil {
		return errJSON(r.unreachable(err))
	}
	lines, flags := values(section(x, "szLine")), values(section(x, "chFlag"))
	if len(lines) == 0 {
		return errJSON(noList)
	}
	type entry struct {
		Index      int    `json:"index"`
		Text       string `json:"text"`
		Flag       int    `json:"flag"`
		Cursor     bool   `json:"cursor"`
		Selectable bool   `json:"selectable"`
	}
	items := []entry{}
	page := ""
	for i, l := range lines {
		t := xmlDecode(l)
		if strings.TrimSpace(t) == "" {
			continue
		}
		if p, ok := pageCounter(t); ok {
			page = p
			continue
		}
		f := 0
		if i < len(flags) {
			f, _ = strconv.Atoi(strings.TrimSpace(flags[i]))
		}
		items = append(items, entry{i, t, f, f&flagCursor != 0, f&flagSelect != 0})
	}
	return fpjson.Marshal(struct {
		OK    bool    `json:"ok"`
		Lines []entry `json:"lines"`
		Page  string  `json:"page"`
	}{true, items, page})
}

func formEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func (r *Remote) put(cmd0 string) string {
	if r.base() == "" {
		return noAddress
	}
	body := "cmd0=" + formEncode(cmd0) + "&cmd1=" + formEncode(updateCmd)
	resp, err := r.client.Post(r.base()+putPath, "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		return r.unreachable(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return r.unreachable(fmt.Errorf("Unexpected response status code: %d", resp.StatusCode))
	}
	return ""
}

func (r *Remote) Command(cmd string) string {
	if !allowed[cmd] {
		return "Not a permitted command."
	}
	return r.put(cmdPrefix + cmd)
}

func (r *Remote) cursorLine() (cursor, count int) {
	x, err := r.fetchStatus()
	if err != nil {
		return -1, 0
	}
	flags := values(section(x, "chFlag"))
	for i, f := range flags {
		if n, _ := strconv.Atoi(strings.TrimSpace(f)); n&flagCursor != 0 {
			return i, len(flags)
		}
	}
	return -1, len(flags)
}

func (r *Remote) Select(index int) string {
	if r.base() == "" {
		return noAddress
	}
	if index < 0 || index >= Lines {
		return "There is no line " + strconv.Itoa(index) + "."
	}
	cur, count := r.cursorLine()
	if count == 0 {
		return noList
	}
	if cur < 0 {
		return "The receiver is not showing a list with a cursor."
	}
	if index >= count {
		return "There is no line " + strconv.Itoa(index) + "."
	}
	c := "CurDown"
	if index < cur {
		c = "CurUp"
	}
	step := index - cur
	if step < 0 {
		step = -step
	}
	for i := 0; i < step; i++ {
		if e := r.Command(c); e != "" {
			return e
		}
	}
	return r.Command("CurEnter")
}

func (r *Remote) Search(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return "Nothing to search for."
	}
	if len([]rune(q)) > maxQuery {
		return "Search term is too long."
	}
	return r.put(searchPrefix + q)
}
