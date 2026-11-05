package radiobrowser

import (
	"path/filepath"
	"strings"
	"testing"

	"retuner/internal/config"
)

func TestUnsafeMACNeverEscapesCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	c := &Client{CacheDir: cacheDir, CacheType: "catFile"}
	a := &config.AVR{FileCRC: 12345}

	unsafeMACs := []string{
		"../../../../../../etc/passwd",
		"bad/name",
		"bad\\name",
		"../escape",
		strings.Repeat("a", 65),
	}

	for _, mac := range unsafeMACs {
		p := c.filePath("RBS", mac, a, "0-hash")
		if filepath.Dir(p) != cacheDir {
			t.Fatalf("unsafe MAC %q resulted in path %q outside CacheDir %q", mac, p, cacheDir)
		}
	}
}
