package config

import (
	"os"
	"regexp"
	"testing"
)

// The release workflow names archives after src/common.pas's APP_VERSION and
// the updater compares that against the release tag, so the Go binary has to
// report the same string.
func TestAppVersionMatchesPascal(t *testing.T) {
	b, err := os.ReadFile("../../src/common.pas")
	if err != nil {
		t.Skip("src/common.pas not available")
	}
	m := regexp.MustCompile(`(?m)^\s*APP_VERSION\s*=\s*'([0-9.]+)'`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no APP_VERSION in src/common.pas")
	}
	if string(m[1]) != AppVersion {
		t.Fatalf("AppVersion %q, src/common.pas %q", AppVersion, m[1])
	}
}
