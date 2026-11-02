package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
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
	color    ui.Color
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
	artSmallSide     = 96
	artMediumSide    = 192
	artLargeSide     = 384
	artNotification  = 96
	artMosaicSide    = 192
	maxArtEntries    = 1024
	maxArtBytes      = 96 << 20
	maxMosaicEntries = 64
	maxMosaicBytes   = 32 << 20
	artLoadWorkers   = 4
	artDiskLimit     = 256 << 20
	artDiskPruneAge  = 10 * time.Minute
)

var artworkSizePattern = regexp.MustCompile(`=w\d+-h\d+`)

type artCache struct {
	mu      sync.Mutex
	art     map[string]*artwork
	busy    map[string]bool
	mosaics map[string]*mosaicEntry
	pinned  map[string]bool
	colors  map[string]ui.Color
	post    func(func())
	dir     string

	artBytes    int64
	mosaicBytes int64
	mosaicLimit int64
	maxArt      int
	maxMosaic   int
	maxDisk     int64
	lastPrune   time.Time
	diskMu      sync.Mutex
	sem         chan struct{}
}

func newArtCache(post func(func()), dir string) *artCache {
	return &artCache{
		art:         map[string]*artwork{},
		busy:        map[string]bool{},
		mosaics:     map[string]*mosaicEntry{},
		pinned:      map[string]bool{},
		colors:      map[string]ui.Color{},
		post:        post,
		dir:         dir,
		maxArt:      maxArtEntries,
		maxMosaic:   maxMosaicEntries,
		mosaicLimit: maxMosaicBytes,
		maxDisk:     artDiskLimit,
		sem:         make(chan struct{}, artLoadWorkers),
	}
}

func artworkSideFor(size float32) int {
	pixels := int(size * 2)
	switch {
	case pixels <= artSmallSide:
		return artSmallSide
	case pixels <= artMediumSide:
		return artMediumSide
	default:
		return artLargeSide
	}
}

func artworkCacheKey(u string, side int) string {
	return u + "|" + strconv.Itoa(side)
}

func sizedArtworkURL(u string, side int) string {
	return artworkSizePattern.ReplaceAllString(u, fmt.Sprintf("=w%d-h%d", side, side))
}

func (c *artCache) get(u string, side int) *artwork {
	if u == "" {
		return nil
	}
	if side <= 0 {
		side = artMediumSide
	}
	key := artworkCacheKey(u, side)

	c.mu.Lock()
	if a, ok := c.art[key]; ok {
		a.lastUsed = time.Now()
		c.mu.Unlock()
		return a
	}
	if c.busy[key] {
		c.mu.Unlock()
		return nil
	}
	c.busy[key] = true
	c.mu.Unlock()

	go func(targetURL string, targetSide int, cacheKey string) {
		c.sem <- struct{}{}
		bmp, data, size, avg, err := loadArtwork(targetURL, targetSide, c.dir)
		<-c.sem
		c.mu.Lock()
		delete(c.busy, cacheKey)
		if err != nil {
			c.art[cacheKey] = &artwork{failed: true, lastUsed: time.Now()}
		} else {
			c.art[cacheKey] = &artwork{bmp: bmp, data: data, color: avg, size: size, lastUsed: time.Now()}
			c.colors[cacheKey] = avg
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
	}(u, side, key)

	return nil
}

func (c *artCache) setPinned(pins map[string]bool) {
	c.mu.Lock()
	c.pinned = pins
	c.mu.Unlock()
}

func (c *artCache) evictArtLocked() {
	for len(c.art) > c.maxArt || (maxArtBytes > 0 && c.artBytes > maxArtBytes) {
		var oldestKey string
		var oldest time.Time
		for key, art := range c.art {
			if c.pinned[key] {
				continue
			}
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

func loadArtwork(u string, side int, dir string) (*ui.Bitmap, []byte, int64, ui.Color, error) {
	cacheKey := artworkCacheKey(u, side)
	if dir != "" {
		if data, err := os.ReadFile(artworkDiskPath(dir, cacheKey)); err == nil {
			if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				img = fitArtwork(img, side)
				bmp := ui.NewBitmap(img)
				bounds := img.Bounds()
				size := int64(bounds.Dx()*bounds.Dy()*4) + int64(len(data))
				return bmp, data, size, averageColor(img), nil
			}
		}
	}

	fetchURL := sizedArtworkURL(u, side)
	bmp, data, size, avg, err := downloadArtwork(fetchURL, side)
	if err != nil && fetchURL != u {
		bmp, data, size, avg, err = downloadArtwork(u, side)
	}
	if err != nil {
		return nil, nil, 0, ui.Color{}, err
	}
	if dir != "" {
		_ = writeArtworkDisk(dir, cacheKey, data)
	}
	return bmp, data, size, avg, nil
}

func downloadArtwork(u string, side int) (*ui.Bitmap, []byte, int64, ui.Color, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, 0, ui.Color{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, 0, ui.Color{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, 0, ui.Color{}, fmt.Errorf("artwork HTTP %d", resp.StatusCode)
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, nil, 0, ui.Color{}, err
	}
	img = fitArtwork(img, side)
	bounds := img.Bounds()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, nil, 0, ui.Color{}, err
	}
	data := buf.Bytes()
	size := int64(bounds.Dx()*bounds.Dy()*4) + int64(len(data))
	return ui.NewBitmap(img), data, size, averageColor(img), nil
}

func averageColor(img image.Image) ui.Color {
	bounds := img.Bounds()
	step := max(1, max(bounds.Dx(), bounds.Dy())/8)
	var rs, gs, bs, count uint64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			rs += uint64(r)
			gs += uint64(g)
			bs += uint64(b)
			count++
		}
	}
	if count == 0 {
		return ui.Color{}
	}
	return ui.RGBA(uint8((rs/count)>>8), uint8((gs/count)>>8), uint8((bs/count)>>8), 1)
}

func (c *artCache) colorFor(key string) (ui.Color, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	color, ok := c.colors[key]
	return color, ok
}

func artworkDiskPath(dir, key string) string {
	if dir == "" {
		return ""
	}
	sum := sha1.Sum([]byte(key))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".jpg")
}

func writeArtworkDisk(dir, key string, data []byte) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".artwork-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, artworkDiskPath(dir, key))
}

func (c *artCache) pruneDisk(force bool) {
	if c.dir == "" || c.maxDisk <= 0 {
		return
	}
	c.diskMu.Lock()
	defer c.diskMu.Unlock()

	now := time.Now()
	if !force && !c.lastPrune.IsZero() && now.Sub(c.lastPrune) < artDiskPruneAge {
		return
	}
	c.lastPrune = now

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type diskFile struct {
		path string
		size int64
		mod  time.Time
	}
	var files []diskFile
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, diskFile{
			path: filepath.Join(c.dir, entry.Name()),
			size: info.Size(),
			mod:  info.ModTime(),
		})
		total += info.Size()
	}
	if total <= c.maxDisk {
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.Before(files[j].mod)
	})
	target := c.maxDisk * 9 / 10
	for _, file := range files {
		if total <= target {
			return
		}
		if err := os.Remove(file.path); err == nil {
			total -= file.size
		}
	}
}

func (c *artCache) Clear() {
	c.mu.Lock()
	c.art = map[string]*artwork{}
	c.busy = map[string]bool{}
	c.mosaics = map[string]*mosaicEntry{}
	c.pinned = map[string]bool{}
	c.colors = map[string]ui.Color{}
	c.artBytes = 0
	c.mosaicBytes = 0
	c.mu.Unlock()

	if c.dir == "" {
		return
	}
	c.diskMu.Lock()
	defer c.diskMu.Unlock()
	_ = os.RemoveAll(c.dir)
	c.lastPrune = time.Time{}
}

// fitArtwork downscales large covers to the requested cache side.
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
		art := c.get(u, artMosaicSide)
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

	side := artworkSideFor(size)
	if color, ok := a.art.colorFor(artworkCacheKey(u, side)); ok {
		box.Background(color)
	}
	if a.pl != nil {
		if cur := a.pl.Current(); cur != nil && cur.Thumbnail == u {
			if a.artPins == nil {
				a.artPins = map[string]bool{}
			}
			a.artPins[artworkCacheKey(u, side)] = true
		}
	}

	art := a.art.get(u, side)
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
