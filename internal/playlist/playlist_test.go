package playlist

import "testing"

func TestLooks(t *testing.T) {
	for u, want := range map[string]bool{"http://a/x.PLS": true, "http://a/x.m3u?t=1": true, "http://a/x.mp3": false, "http://a/x.mp3#a.m3u": false, "http://a/x.xspf#f": true} {
		if Looks(u) != want {
			t.Errorf("%s: want %v", u, want)
		}
	}
}

func TestFirstStream(t *testing.T) {
	cases := map[string]string{
		"[playlist]\nFile1=http://h/s1\nFile2=http://h/s2\n": "http://h/s1",
		"#EXTM3U\n# c\n;x\nhttp://h/s3\n":                    "http://h/s3",
		`<asx><ref href="http://h/s4"/></asx>`:               "http://h/s4",
		"nothing":                                            "",
		"file1=ftp://h/x\n":                                  "",
	}
	for in, want := range cases {
		if got := FirstStream(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
