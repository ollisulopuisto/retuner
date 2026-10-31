package radiobrowser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveOldCacheFiles(t *testing.T) {
	d := t.TempDir()
	old := time.Now().Add(-5 * time.Hour)
	mk := func(name string, mt time.Time) {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, mt, mt)
	}
	mk("RBS-aa-1-2-3.cache", old)
	mk("RBC-aa-1-3.cache", old)
	mk("RBS-new-1-2-3.cache", time.Now())
	mk("RBnodash.cache", old) // no '-': pattern RB*-*.cache does not match
	mk("RBS-aa-1.txt", old)   // wrong extension
	mk("icon-old.cache", old) // not RB*
	if n := RemoveOldCacheFiles(d, 0); n != 0 {
		t.Fatal("ttl 0 must remove nothing")
	}
	if n := RemoveOldCacheFiles(d, 1); n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	for _, keep := range []string{"RBS-new-1-2-3.cache", "RBnodash.cache", "RBS-aa-1.txt", "icon-old.cache"} {
		if _, err := os.Stat(filepath.Join(d, keep)); err != nil {
			t.Errorf("%s should remain", keep)
		}
	}
}
