package player

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestCachePathSanitizesVideoID(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	path := c.Path("../../etc/passwd", "mp4")
	if filepath.Dir(path) != dir {
		t.Fatalf("cache path escaped the cache dir: %s", path)
	}
	if got := filepath.Base(path); got != "passwd.mp4" {
		t.Fatalf("cache filename = %q, want passwd.mp4", got)
	}
	if got := c.Path("..", "mp4"); filepath.Base(got) != "invalid.mp4" {
		t.Fatalf("cache path for .. = %q, want invalid.mp4", got)
	}
}

func TestCacheSaveAndLookupStayInsideCacheDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "cache")
	c := NewCache(dir, 0)
	info := &ytmusic.StreamInfo{VideoID: "../escape"}
	if err := c.Save("../escape", info, []byte("audio")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.aac")); !os.IsNotExist(err) {
		t.Fatal("cache wrote outside its directory")
	}
	if _, _, ok := c.Lookup("../escape"); !ok {
		t.Fatal("sanitized cache entry was not found")
	}
}

func TestCachePrunesAllMediaTypes(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 10)
	oldPath := filepath.Join(dir, "old.mp4")
	newPath := filepath.Join(dir, "new.heaac.mp4")
	if err := os.WriteFile(oldPath, make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(oldPath, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	c.Prune()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old mp4 was not pruned")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatal("newer heaac mp4 was pruned")
	}
}

func TestCacheSizeCountsAllMedia(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	for name, size := range map[string]int{"a.mp4": 3, "b.heaac.m4a": 4, "c.aac": 5, "d.json": 99, "e.part": 7} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Size(); got != 12 {
		t.Fatalf("Size() = %d, want 12", got)
	}
}

func TestCacheClearExceptKeepsCurrentTrack(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	for _, name := range []string{"keep.mp4", "keep.heaac.m4a", "drop.mp4", "drop.aac", "drop.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c.ClearExcept("keep")
	for _, name := range []string{"keep.mp4", "keep.heaac.m4a"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s was removed", name)
		}
	}
	for _, name := range []string{"drop.mp4", "drop.aac", "drop.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was kept", name)
		}
	}
}
