package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestArtCacheCoalescesPosts(t *testing.T) {
	var posts atomic.Int32
	c := newArtCache(func(func()) { posts.Add(1) }, "")

	for i := 0; i < 5; i++ {
		c.schedulePost()
	}

	deadline := time.Now().Add(time.Second)
	for posts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("posts = %d, want 1", got)
	}

	time.Sleep(200 * time.Millisecond)
	if got := posts.Load(); got != 1 {
		t.Fatalf("posts after delay = %d, want 1", got)
	}
}

func TestFitArtworkDownscalesLargeCovers(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	got := fitArtwork(img, 256)
	if got.Bounds().Dx() != 256 || got.Bounds().Dy() != 128 {
		t.Fatalf("bounds = %v, want 256x128", got.Bounds())
	}
}

func TestArtCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newArtCache(nil, "")
	c.maxArt = 2
	now := time.Now()
	c.art["old"] = &artwork{size: 1, lastUsed: now.Add(-3 * time.Second)}
	c.art["mid"] = &artwork{size: 1, lastUsed: now.Add(-2 * time.Second)}
	c.art["new"] = &artwork{size: 1, lastUsed: now.Add(-time.Second)}
	c.artBytes = 3
	c.evictArtLocked()

	if len(c.art) != 2 {
		t.Fatalf("cache entries = %d, want 2", len(c.art))
	}
	if _, ok := c.art["old"]; ok {
		t.Fatal("oldest artwork was not evicted")
	}
}

func TestMosaicCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newArtCache(nil, "")
	c.maxMosaic = 1
	now := time.Now()
	c.mosaics["old"] = &mosaicEntry{size: 1, lastUsed: now.Add(-2 * time.Second)}
	c.mosaics["new"] = &mosaicEntry{size: 1, lastUsed: now.Add(-time.Second)}
	c.mosaicBytes = 2
	c.evictMosaicLocked()

	if len(c.mosaics) != 1 {
		t.Fatalf("mosaic entries = %d, want 1", len(c.mosaics))
	}
	if _, ok := c.mosaics["old"]; ok {
		t.Fatal("oldest mosaic was not evicted")
	}
}

func TestArtCacheKeepsPinnedEntry(t *testing.T) {
	c := newArtCache(nil, "")
	c.maxArt = 1
	now := time.Now()
	c.art["pinned"] = &artwork{size: 1, lastUsed: now.Add(-time.Hour)}
	c.art["other"] = &artwork{size: 1, lastUsed: now}
	c.artBytes = 2
	c.pinned = map[string]bool{"pinned": true}

	c.evictArtLocked()
	if _, ok := c.art["pinned"]; !ok {
		t.Fatal("pinned artwork was evicted")
	}
	if _, ok := c.art["other"]; ok {
		t.Fatal("unpinned artwork was not evicted")
	}
}

func TestArtworkSideFor(t *testing.T) {
	cases := []struct {
		size float32
		want int
	}{
		{36, artSmallSide},
		{80, artMediumSide},
		{150, artLargeSide},
		{300, artLargeSide},
	}
	for _, tc := range cases {
		if got := artworkSideFor(tc.size); got != tc.want {
			t.Errorf("artworkSideFor(%v) = %d, want %d", tc.size, got, tc.want)
		}
	}
}

func TestSizedArtworkURL(t *testing.T) {
	got := sizedArtworkURL("https://lh3.googleusercontent.com/a=w544-h544-l90-rj", artSmallSide)
	want := "https://lh3.googleusercontent.com/a=w96-h96-l90-rj"
	if got != want {
		t.Fatalf("sizedArtworkURL = %q, want %q", got, want)
	}
}

func TestLoadArtworkFromDisk(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := artworkDiskPath(dir, artworkCacheKey("https://example.invalid/cover", artSmallSide))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write disk artwork: %v", err)
	}

	bmp, data, size, color, err := loadArtwork("https://example.invalid/cover", artSmallSide, dir)
	if err != nil {
		t.Fatalf("load artwork: %v", err)
	}
	if bmp == nil || len(data) == 0 || size <= 0 || color.A == 0 {
		t.Fatalf("bitmap=%v data=%d size=%d color=%+v", bmp, len(data), size, color)
	}
}

func TestArtCacheClearRemovesDisk(t *testing.T) {
	dir := t.TempDir()
	c := newArtCache(nil, dir)
	path := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(path, []byte("cover"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	c.Clear()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("artwork disk directory was not removed")
	}
}
