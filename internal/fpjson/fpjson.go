// Package fpjson re-lays compact JSON the way Free Pascal's fpjson AsJSON does:
// "{ "a" : 1, "b" : [{ "c" : 2 }] }", non-ASCII as \uXXXX. Scripts that grep the
// editor's replies depend on that spacing.
package fpjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Marshal encodes v (struct fields and ordered values keep their order).
func Marshal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return Format(strings.TrimSpace(b.String()))
}

// Format reflows compact JSON text.
func Format(s string) string {
	var out strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case c == '\\':
				out.WriteByte(c)
				i++
				out.WriteByte(s[i])
			case c == '"':
				inStr = false
				out.WriteByte(c)
			case c < 0x80:
				out.WriteByte(c)
			default:
				r := []rune(s[i:])[0]
				for _, u := range utf16.Encode([]rune{r}) {
					fmt.Fprintf(&out, "\\u%04x", u)
				}
				i += len(string(r)) - 1
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			out.WriteByte(c)
		case '{':
			if i+1 < len(s) && s[i+1] == '}' {
				out.WriteString("{}")
				i++
			} else {
				out.WriteString("{ ")
			}
		case '}':
			out.WriteString(" }")
		case ':':
			out.WriteString(" : ")
		case ',':
			out.WriteString(", ")
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}
