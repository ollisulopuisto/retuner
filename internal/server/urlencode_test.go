package server

import (
	"testing"
)

func TestURLEncode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"abcXYZ", "abcXYZ"},
		{"Rock & Pop", "Rock%20%26%20Pop"},
		{"123-456", "%31%32%33%2D%34%35%36"},
		{"a/b.c", "a%2Fb%2Ec"},
		{"äö", "%C3%A4%C3%B6"},
	}

	for _, tt := range tests {
		got := urlEncode(tt.input)
		if got != tt.expected {
			t.Errorf("urlEncode(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func BenchmarkURLEncode_LettersOnly(b *testing.B) {
	s := "PopMusicDirectory"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = urlEncode(s)
	}
}

func BenchmarkURLEncode_Mixed(b *testing.B) {
	s := "Rock & Pop (80s - 90s) / Jazz & Blues"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = urlEncode(s)
	}
}
