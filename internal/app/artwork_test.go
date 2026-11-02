package app

import (
	"image"
	"testing"
	"time"
)

func TestFitArtworkDownscalesLargeCovers(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	got := fitArtwork(img, 256)
	if got.Bounds().Dx() != 256 || got.Bounds().Dy() != 128 {
		t.Fatalf("bounds = %v, want 256x128", got.Bounds())
	}
}

func TestArtCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newArtCache(nil)
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
	c := newArtCache(nil)
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
