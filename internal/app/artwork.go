package app

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/douglarek/melo/internal/ytmusic"
)

type artwork struct {
	bmp      *ui.Bitmap
	data     []byte
	failed   bool
	size     int64
	lastUsed time.Time
}

type mosaicEntry struct {
	bmp      *ui.Bitmap
	size     int64
	lastUsed time.Time
}

const (
	artworkMaxSide   = 256
	maxArtEntries    = 256
	maxArtBytes      = 128 << 20
	maxMosaicEntries = 64
	maxMosaicBytes   = 32 << 20
	artLoadWorkers   = 4
)

type artCache struct {
	mu      sync.Mutex
	art     map[string]*artwork
	busy    map[string]bool
	mosaics map[string]*mosaicEntry
	post    func(func())

	artBytes    int64
	mosaicBytes int64
	mosaicLimit int64
	maxArt      int
	maxMosaic   int
	sem         chan struct{}
}

func newArtCache(post func(func())) *artCache {
	return &artCache{
		art:         map[string]*artwork{},
		busy:        map[string]bool{},
		mosaics:     map[string]*mosaicEntry{},
		post:        post,
		maxArt:      maxArtEntries,
		maxMosaic:   maxMosaicEntries,
		mosaicLimit: maxMosaicBytes,
		sem:         make(chan struct{}, artLoadWorkers),
	}
}

func (c *artCache) get(u string) *artwork {
	if u == "" {
		return nil
	}
	c.mu.Lock()
	if a, ok := c.art[u]; ok {
		a.lastUsed = time.Now()
		c.mu.Unlock()
		return a
	}
	if c.busy[u] {
		c.mu.Unlock()
		return nil
	}
	c.busy[u] = true
	c.mu.Unlock()

	go func(targetURL string) {
		c.sem <- struct{}{}
		bmp, data, size, err := loadArtwork(targetURL)
		<-c.sem
		c.mu.Lock()
		delete(c.busy, targetURL)
		if err != nil {
			c.art[targetURL] = &artwork{failed: true, lastUsed: time.Now()}
		} else {
			c.art[targetURL] = &artwork{bmp: bmp, data: data, size: size, lastUsed: time.Now()}
			c.artBytes += size
		}
		before := c.artBytes
		c.evictArtLocked()
		evicted := before - c.artBytes
		c.mu.Unlock()
		if evicted > 8<<20 {
			go debug.FreeOSMemory()
		}

		c.post(func() {
			// Window redraw is automatically scheduled by post
		})
	}(u)

	return nil
}

func (c *artCache) evictArtLocked() {
	for len(c.art) > c.maxArt || (maxArtBytes > 0 && c.artBytes > maxArtBytes) {
		var oldestKey string
		var oldest time.Time
		for key, art := range c.art {
			if oldestKey == "" || art.lastUsed.Before(oldest) {
				oldestKey = key
				oldest = art.lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		c.artBytes -= c.art[oldestKey].size
		delete(c.art, oldestKey)
	}
	if c.artBytes < 0 {
		c.artBytes = 0
	}
}

func (c *artCache) evictMosaicLocked() {
	for len(c.mosaics) > c.maxMosaic || (c.mosaicLimit > 0 && c.mosaicBytes > c.mosaicLimit) {
		var oldestKey string
		var oldest time.Time
		for key, entry := range c.mosaics {
			if oldestKey == "" || entry.lastUsed.Before(oldest) {
				oldestKey = key
				oldest = entry.lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		c.mosaicBytes -= c.mosaics[oldestKey].size
		delete(c.mosaics, oldestKey)
	}
	if c.mosaicBytes < 0 {
		c.mosaicBytes = 0
	}
}

func loadArtwork(u string) (*ui.Bitmap, []byte, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, nil, 0, err
	}
	img = fitArtwork(img, artworkMaxSide)
	bounds := img.Bounds()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, nil, 0, err
	}
	data := buf.Bytes()
	size := int64(bounds.Dx()*bounds.Dy()*4) + int64(len(data))
	return ui.NewBitmap(img), data, size, nil
}

// fitArtwork downscales large covers; the UI never shows them above 256 DIPs.
func fitArtwork(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxSide && b.Dy() <= maxSide {
		return img
	}
	scale := float64(maxSide) / float64(max(b.Dx(), b.Dy()))
	w := max(1, int(float64(b.Dx())*scale))
	h := max(1, int(float64(b.Dy())*scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// playlistCover renders a playlist cover, using a 2x2 mosaic of the first
// tracks when YouTube Music does not provide a dedicated cover.
func (a *app) playlistCover(c *ui.Context, pl *ytmusic.Playlist, size, radius float32) {
	if pl.Thumbnail != "" || len(pl.Tracks) == 0 {
		a.coverImage(c, pl.Thumbnail, size, radius)
		return
	}

	urls := make([]string, 0, 4)
	key := pl.ID
	hasThumb := false
	for i := 0; i < 4 && i < len(pl.Tracks); i++ {
		u := pl.Tracks[i].Thumbnail
		urls = append(urls, u)
		key += "|" + u
		if u != "" {
			hasThumb = true
		}
	}
	if !hasThumb {
		a.coverImage(c, "", size, radius)
		return
	}
	if bmp := a.art.mosaic(key, urls); bmp != nil {
		a.coverBitmap(c, bmp, size, radius)
		return
	}

	// Images are still loading; show the individual cells meanwhile.
	p := a.pal
	cellSize := size / 2
	grid := ui.Box(c).Size(size, size).Radius(radius).Background(p.card).Clip()
	grid.Children(func() {
		ui.Column(c).Fill().Children(func() {
			for row := 0; row < 2; row++ {
				ui.Row(c).FillWidth().Height(cellSize).Children(func() {
					for col := 0; col < 2; col++ {
						idx := row*2 + col
						if idx < len(pl.Tracks) && pl.Tracks[idx].Thumbnail != "" {
							a.coverImage(c, pl.Tracks[idx].Thumbnail, cellSize, 0)
							continue
						}
						ui.Box(c).Size(cellSize, cellSize).Background(p.card).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
							ui.Icon(c, iconMusic).Size(size*0.14, size*0.14).TextColor(p.muted)
						})
					}
				})
			}
		})
	})
}

// mosaic composes up to four square-cropped artworks into one rounded image.
func (c *artCache) mosaic(key string, urls []string) *ui.Bitmap {
	c.mu.Lock()
	if entry, ok := c.mosaics[key]; ok {
		entry.lastUsed = time.Now()
		c.mu.Unlock()
		return entry.bmp
	}
	c.mu.Unlock()

	arts := make([]*artwork, len(urls))
	for i, u := range urls {
		if u == "" {
			continue
		}
		art := c.get(u)
		if art == nil {
			return nil
		}
		arts[i] = art
	}

	const mosaicPixels = 320
	cell := mosaicPixels / 2
	dst := image.NewRGBA(image.Rect(0, 0, mosaicPixels, mosaicPixels))
	for i, art := range arts {
		if art == nil || len(art.data) == 0 {
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(art.data))
		if err != nil {
			continue
		}
		rect := image.Rect((i%2)*cell, (i/2)*cell, (i%2)*cell+cell, (i/2)*cell+cell)
		xdraw.ApproxBiLinear.Scale(dst, rect, img, squareCrop(img), xdraw.Src, nil)
	}
	bmp := ui.NewBitmap(dst)

	c.mu.Lock()
	entry := &mosaicEntry{bmp: bmp, size: int64(mosaicPixels * mosaicPixels * 4), lastUsed: time.Now()}
	c.mosaics[key] = entry
	c.mosaicBytes += entry.size
	before := c.mosaicBytes
	c.evictMosaicLocked()
	evicted := before - c.mosaicBytes
	c.mu.Unlock()
	if evicted > 8<<20 {
		go debug.FreeOSMemory()
	}
	return bmp
}

func squareCrop(img image.Image) image.Rectangle {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := min(w, h)
	x := b.Min.X + (w-side)/2
	y := b.Min.Y + (h-side)/2
	return image.Rect(x, y, x+side, y+side)
}

func (a *app) coverBitmap(c *ui.Context, bmp *ui.Bitmap, size, radius float32) {
	p := a.pal
	box := ui.Box(c).Size(size, size).Radius(radius).Background(p.card)
	box.Children(func() {
		ui.Image(c, bmp).Fill().Fit(ui.Cover).Radius(radius)
	})
}

// coverImage renders an artwork image or a graceful placeholder if still loading.
func (a *app) coverImage(c *ui.Context, u string, size float32, radius float32) {
	p := a.pal

	box := ui.Box(c).Size(size, size).Radius(radius).Background(p.card).AlignItems(ui.Center).Justify(ui.Center)
	if u == "" {
		box.Children(func() {
			ui.Icon(c, iconMusic).Size(size*0.4, size*0.4).TextColor(p.muted)
		})
		return
	}

	art := a.art.get(u)
	if art != nil && art.bmp != nil {
		box.Children(func() {
			ui.Image(c, art.bmp).Fill().Fit(ui.Cover).Radius(radius)
		})
	} else {
		box.Children(func() {
			ui.Icon(c, iconMusic).Size(size*0.4, size*0.4).TextColor(p.muted)
		})
	}
}
