package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The shipped avr.ini holds ';'-separated lists: ';' inside a value is not a comment
// (Pascal's TIniFile has no inline comments).
func TestAVRListsSurviveSemicolonsAndPerMACFilesAreSeparate(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "retuner.ini")
	_ = os.WriteFile(ini, []byte("[Configuration]\nCommonAVRini=0\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config", "avr.ini"), []byte("[MainMenu Items]\nIdentifiersList=about;radiobrowser\nLabelsList=A;B\n[RadioBrowser Filtering]\nAllowedTags=rock; jazz!;{empty}\nBitrateMax=128\n[RadioBrowser Sorting]\nOrder=votes\nReverse=1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "config", "aa11.ini"), []byte("[RadioBrowser Filtering]\nNotAllowedInName=ads\n"), 0o644)
	c, err := Load(ini)
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.LoadAVR()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.MainMenu) != 2 || a.MainMenu[1].Identifier != "radiobrowser" {
		t.Fatalf("%+v", a.MainMenu)
	}
	if len(a.AllowedTags) != 3 || a.AllowedTags[1] != "jazz" || a.BitrateMax != 128 || a.Order != "votes" || !a.Reverse {
		t.Fatalf("%+v", a)
	}
	m, err := c.LoadAVRFile("aa11.ini")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.AllowedTags) != 0 || len(m.NotAllowedInName) != 1 || len(m.MainMenu) != 6 || m.Order != "name" {
		t.Fatalf("per-MAC config should be independent and filled with defaults: %+v", m)
	}
}
