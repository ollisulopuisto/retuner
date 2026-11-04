// Package translator ports ReplaceDiacritics (src/translator.pas): the offline
// part of the Translator. The online Google Translate step is NOT ported and
// never contacts the network.
package translator

import (
	"strings"
	"unicode/utf8"
)

// ReplaceDiacritics maps two-byte UTF-8 code points in the Latin-1 supplement,
// Latin Extended-A and Extended-B ranges to ASCII, each range only when its
// flag is set. Everything else (and invalid UTF-8 bytes) passes through.
// Bolt optimization: Fast-path return when all replacement flags are false or
// string is pure ASCII (<0x80) to avoid any heap allocations; stream into strings.Builder
// pre-allocated with Grow(len(s)) when non-ASCII runes are present.
func ReplaceDiacritics(s string, supplement, extA, extB bool) string {
	if !supplement && !extA && !extB {
		return s
	}

	firstNonASCII := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			firstNonASCII = i
			break
		}
	}
	if firstNonASCII == -1 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:firstNonASCII])

	for i := firstNonASCII; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n <= 1 {
			b.WriteByte(s[i])
			i++
			continue
		}
		switch {
		case n == 2 && supplement && r >= 160 && r <= 255:
			b.WriteString(latin1Supplement[r-160])
		case n == 2 && extA && r >= 256 && r <= 383:
			b.WriteString(latin1ExtendedA[r-256])
		case n == 2 && extB && r >= 384 && r <= 639:
			b.WriteString(latin1ExtendedB[r-384])
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}
