package relay

import "testing"

func TestRequestedID(t *testing.T) {
	for in, want := range map[string]string{
		"GET /relay?id=MS_ABC HTTP/1.1":        "MS_ABC",
		"GET /relay?x=1&id=RB_1&y=2 HTTP/1.0":  "RB_1",
		"GET /relay HTTP/1.1":                  "",
		"GET /other?id=MS_A HTTP/1.1":          "",
		"GET":                                  "",
		"GET /relay?id= HTTP/1.1":              "",
		"POST /relay?id=MS_X&id=MS_Y HTTP/1.1": "MS_X",
	} {
		if got := RequestedID(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestNeedsRelay(t *testing.T) {
	if !NeedsRelay("HTTPS://a/b") || NeedsRelay("http://a/b") || NeedsRelay("ftp://a") {
		t.Error("only https needs the relay")
	}
}
