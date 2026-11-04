package translator

import (
	"testing"
)

func TestReplaceDiacritics(t *testing.T) {
	tests := []struct {
		input             string
		suppl, extA, extB bool
		want              string
	}{
		{"", true, true, true, ""},
		{"Hello World", true, true, true, "Hello World"},
		{"Classic Rock 101", true, true, true, "Classic Rock 101"},
		{"Café", false, false, false, "Café"},
		{"Café", true, false, false, "Cafe"},
		{"Łódź", true, true, false, "lodz"},
		{"Café Łódź ǅ Ƒß", true, true, true, "Cafe lodz Dz Fss"},
		{"Café Łódź ǅ Ƒß", false, false, false, "Café Łódź ǅ Ƒß"},
		{"Invalid \xff\xfe bytes", true, true, true, "Invalid \xff\xfe bytes"},
		{"Emojis 🎵📻 and Chinese 中文", true, true, true, "Emojis 🎵📻 and Chinese 中文"},
	}

	for _, tt := range tests {
		got := ReplaceDiacritics(tt.input, tt.suppl, tt.extA, tt.extB)
		if got != tt.want {
			t.Errorf("ReplaceDiacritics(%q, %v, %v, %v) = %q, want %q", tt.input, tt.suppl, tt.extA, tt.extB, got, tt.want)
		}
	}
}

func BenchmarkReplaceDiacritics_ASCII(b *testing.B) {
	s := "Classic Rock 101 - The Greatest Hits of All Time"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ReplaceDiacritics(s, true, true, true)
	}
}

func BenchmarkReplaceDiacritics_Diacritics(b *testing.B) {
	s := "Café Łódź ǅ Ƒß œ éè café"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ReplaceDiacritics(s, true, true, true)
	}
}
