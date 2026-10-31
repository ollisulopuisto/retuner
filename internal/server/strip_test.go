package server

import "testing"

func TestIndexFoldASCII(t *testing.T) {
	u := "http://x.example/proxy/?q=HTTPS://y.example/a.m3u8"
	i := indexFoldASCII(u, "https://")
	if i != len("http://x.example/proxy/?q=") {
		t.Fatalf("index %d", i)
	}
	if indexFoldASCII("http://a", "https://") != -1 {
		t.Fatal("found in a plain http url")
	}
}
