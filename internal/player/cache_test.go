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
	newPath := filepath.Join(dir, "new.alt.mp4")
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
		t.Fatal("newer alt mp4 was pruned")
	}
}

func TestCacheSizesCountEverythingTheCacheHolds(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	writeCacheTestFile(t, filepath.Join(dir, "a.mp4"), 3, 0)
	writeCacheTestFile(t, filepath.Join(dir, "b.alt.m4a"), 4, 0)
	writeCacheTestFile(t, filepath.Join(dir, "c.aac"), 5, 0)
	writeCacheTestFile(t, filepath.Join(dir, "d.json"), 99, 0)
	writeCacheTestFile(t, filepath.Join(dir, "e.part"), 7, 0)
	writeCacheTestFile(t, filepath.Join(c.SabrDir(), "s.m4a"), 11, 0)
	writeCacheTestFile(t, filepath.Join(c.SabrDir(), "s.m4a.ok"), 2, 0)
	writeCacheTestFile(t, filepath.Join(c.ArtworkDir(), "cover.jpg"), 13, 0)

	c.Refresh()
	want := Sizes{Songs: 3 + 4 + 5 + 99 + 11 + 2, Artwork: 13, Temp: 7}
	if got := c.Sizes(); got != want {
		t.Fatalf("Sizes() = %+v, want %+v", got, want)
	}
	if got, want := c.Size(), want.Total(); got != want {
		t.Fatalf("Size() = %d, want %d", got, want)
	}
}

func TestCachePruneEvictsCoversBeforeTracks(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 10)
	song := filepath.Join(c.SabrDir(), "song.m4a")
	cover := filepath.Join(c.ArtworkDir(), "cover.jpg")
	writeCacheTestFile(t, song, 8, -2*time.Hour)
	writeCacheTestFile(t, cover, 8, -time.Hour)

	c.Prune()

	if _, err := os.Stat(cover); !os.IsNotExist(err) {
		t.Fatal("the cover newer than the track was not pruned first")
	}
	if _, err := os.Stat(song); err != nil {
		t.Fatalf("the track was pruned: %v", err)
	}
	if got := c.Size(); got > c.Limit() {
		t.Fatalf("Size() = %d after a prune, over the limit %d", got, c.Limit())
	}
}

func TestCachePruneKeepsTheTrackPlaying(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1)
	playing := filepath.Join(c.SabrDir(), "playing.m4a")
	other := filepath.Join(c.SabrDir(), "other.m4a")
	writeCacheTestFile(t, playing, 8, -time.Hour)
	writeCacheTestFile(t, playing+".ok", 2, -time.Hour)
	writeCacheTestFile(t, other, 8, -2*time.Hour)

	c.SetKeep("playing")
	c.Prune()

	if _, err := os.Stat(playing); err != nil {
		t.Fatalf("the track playing was pruned: %v", err)
	}
	if _, err := os.Stat(playing + ".ok"); err != nil {
		t.Fatalf("the marker of the track playing was pruned: %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("the track used longest ago was not pruned")
	}
}

func TestCachePruneDropsAbandonedDownloads(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 4)
	abandoned := filepath.Join(dir, "old.mp4.part")
	writing := filepath.Join(dir, "new.mp4.part")
	writeCacheTestFile(t, abandoned, 8, -time.Hour)
	writeCacheTestFile(t, writing, 8, 0)

	c.Prune()

	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatal("the download abandoned was not pruned")
	}
	if _, err := os.Stat(writing); err != nil {
		t.Fatalf("the download still writing was pruned: %v", err)
	}
}

func TestCacheClearExceptKeepsTheTrackPlayingEverywhere(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	keep := []string{
		filepath.Join(c.SabrDir(), "keep.m4a"),
		filepath.Join(c.SabrDir(), "keep.m4a.ok"),
		filepath.Join(dir, "keep.json"),
	}
	drop := []string{
		filepath.Join(c.SabrDir(), "drop.m4a"),
		filepath.Join(c.SabrDir(), "drop.m4a.ok"),
		filepath.Join(dir, "drop.mp4"),
		filepath.Join(c.ArtworkDir(), "cover.jpg"),
	}
	for _, path := range keep {
		writeCacheTestFile(t, path, 4, 0)
	}
	for _, path := range drop {
		writeCacheTestFile(t, path, 4, 0)
	}

	c.ClearExcept("keep")

	for _, path := range keep {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed", filepath.Base(path))
		}
	}
	for _, path := range drop {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was kept", filepath.Base(path))
		}
	}
}

func TestCacheMigrateDropsWhatNoVersionReads(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	dead := filepath.Join(dir, "video.heaac.m4a")
	abandoned := filepath.Join(dir, "half.mp4.part")
	writing := filepath.Join(dir, "now.mp4.part")
	current := filepath.Join(dir, "song.mp4")
	writeCacheTestFile(t, dead, 6, -48*time.Hour)
	writeCacheTestFile(t, abandoned, 6, -48*time.Hour)
	writeCacheTestFile(t, writing, 6, 0)
	writeCacheTestFile(t, current, 6, -48*time.Hour)

	freed := c.Migrate()

	if freed != 12 {
		t.Fatalf("Migrate() freed %d bytes, want 12", freed)
	}
	for _, path := range []string{dead, abandoned} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was kept", filepath.Base(path))
		}
	}
	for _, path := range []string{writing, current} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(path), err)
		}
	}
}

// writeCacheTestFile writes a file of size bytes, last modified ago.
func writeCacheTestFile(t *testing.T, path string, size int, ago time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mod := time.Now().Add(ago)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("age %s: %v", path, err)
	}
}

func TestCacheClearExceptKeepsCurrentTrack(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<20)
	for _, name := range []string{"keep.mp4", "keep.alt.m4a", "drop.mp4", "drop.aac", "drop.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c.ClearExcept("keep")
	for _, name := range []string{"keep.mp4", "keep.alt.m4a"} {
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
