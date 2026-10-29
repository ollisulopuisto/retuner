// Package translator ports ReplaceDiacritics (src/translator.pas): the offline
// part of the Translator. The online Google Translate step is NOT ported and
// never contacts the network.
package translator

import "unicode/utf8"

// ReplaceDiacritics maps two-byte UTF-8 code points in the Latin-1 supplement,
// Latin Extended-A and Extended-B ranges to ASCII, each range only when its
// flag is set. Everything else (and invalid UTF-8 bytes) passes through.
func ReplaceDiacritics(s string, supplement, extA, extB bool) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n <= 1 {
			out = append(out, s[i])
			i++
			continue
		}
		switch {
		case n == 2 && supplement && r >= 160 && r <= 255:
			out = append(out, latin1Supplement[r-160]...)
		case n == 2 && extA && r >= 256 && r <= 383:
			out = append(out, latin1ExtendedA[r-256]...)
		case n == 2 && extB && r >= 384 && r <= 639:
			out = append(out, latin1ExtendedB[r-384]...)
		default:
			out = append(out, s[i:i+n]...)
		}
		i += n
	}
	return string(out)
}
