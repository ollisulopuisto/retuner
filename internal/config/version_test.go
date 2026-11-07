package config

import (
	"os"
	"regexp"
	"testing"
)

// The release workflow checks that AppVersion and the add-on config.yaml version agree.
func TestAppVersionMatchesAddonConfig(t *testing.T) {
	b, err := os.ReadFile("../../viritin/config.yaml")
	if err != nil {
		t.Skip("viritin/config.yaml not available")
	}
	m := regexp.MustCompile(`(?m)^version:\s*"([0-9.]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no version found in viritin/config.yaml")
	}
	if string(m[1]) != AppVersion {
		t.Fatalf("AppVersion %q, viritin/config.yaml %q", AppVersion, m[1])
	}
}
