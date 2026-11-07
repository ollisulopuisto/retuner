package config

import (
	"testing"
)

func TestStripChars(t *testing.T) {
	chars := "!\"'():@&=+$,/?%#[]-.~\n\r\t"
	tests := []struct {
		input string
		chars string
		want  string
	}{
		{"", chars, ""},
		{"   ", chars, ""},
		{"!!!", chars, ""},
		{"   !!!   ", chars, ""},
		{"BBC Radio 1", chars, "BBC Radio 1"},
		{"  BBC   Radio  1  ", chars, "BBC Radio 1"},
		{"!BBC (Radio) 1 - London, UK!", chars, "BBC Radio 1 London UK"},
		{"Rock, Pop & Jazz / Classical", chars, "Rock Pop Jazz Classical"},
		{"Station - 101.5 FM [Stereo]", chars, "Station 101 5 FM Stereo"},
		{"öäü € 123", chars, "öäü € 123"},
		{"!@#$%^&*()", chars, "^ *"},
		{"CleanStationName", chars, "CleanStationName"},
		{"abc", "", "abc"},
		{"  abc   def  ", "", "abc def"},
		{";rock;pop;jazz;", ";", "rock pop jazz"},
	}

	for _, tt := range tests {
		got := StripChars(tt.input, tt.chars)
		if got != tt.want {
			t.Errorf("StripChars(%q, %q) = %q, want %q", tt.input, tt.chars, got, tt.want)
		}
	}
}

func BenchmarkStripChars_Clean(b *testing.B) {
	chars := filterStrip
	s := "CleanStationName"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = StripChars(s, chars)
	}
}

func BenchmarkStripChars_Mixed(b *testing.B) {
	chars := filterStrip
	s := "Rock, Pop & Jazz / Classical (80s - 90s)"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = StripChars(s, chars)
	}
}

func BenchmarkStripChars_Stripped(b *testing.B) {
	chars := filterStrip
	s := "!BBC (Radio) 1 - London, UK!"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = StripChars(s, chars)
	}
}
