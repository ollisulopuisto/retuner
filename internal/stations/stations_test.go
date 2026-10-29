package stations

import "testing"

func TestINIParsingAndID(t *testing.T) {
	g, n := parseINI("[Jazz]\nRadio X=http://a/b|http://logo\nbroken line\n\n[Rock]\nY=https://c\n")
	if n != 2 || len(g) != 2 || g[0].Name != "Jazz" || g[0].Stations[0].Logo != "http://logo" || g[1].Stations[0].URL != "https://c" {
		t.Fatalf("%+v %d", g, n)
	}
	id := g[0].Stations[0].ID
	if len(id) != 15 || id[:3] != "MS_" || id != makeID("Radio X", "http://a/b") {
		t.Fatalf("id %q", id)
	}
}

func TestYAMLLineFormat(t *testing.T) {
	g, n := parseYAML("Cat:\n  Name A: http://x|http://l\n  Name B: http://y\n")
	if n != 2 || g[0].Stations[0].Logo != "http://l" || g[0].Stations[1].URL != "http://y" {
		t.Fatalf("%+v", g)
	}
}

func TestMergeDedupesByIDAndCategoryCase(t *testing.T) {
	var m Group
	a, _ := parseINI("[Jazz]\nA=http://a\n")
	b, _ := parseINI("[jazz]\nA=http://a\nB=http://b\n")
	MergeInto(&m, a)
	if added := MergeInto(&m, b); added != 1 || len(m) != 1 || len(m[0].Stations) != 2 {
		t.Fatalf("%+v %d", m, added)
	}
}
