package player

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/douglarek/melo/internal/ytmusic"
)

// Cache manages local disk storage of streamed music tracks.
type Cache struct {
	dir   string
	mu    sync.Mutex
	limit int64
}

// NewCache returns a cache located at dir with a size limit in bytes.
func NewCache(dir string, limit int64) *Cache {
	return &Cache{
		dir:   dir,
		limit: limit,
	}
}

// Path returns the absolute file path for a cached track.
func (c *Cache) Path(videoID, ext string) string {
	videoID = filepath.Base(strings.TrimSpace(videoID))
	if videoID == "." || videoID == ".." || videoID == "" {
		videoID = "invalid"
	}
	ext = filepath.Base(strings.TrimPrefix(ext, "."))
	if c == nil {
		return filepath.Join("/tmp", videoID+"."+ext)
	}
	_ = os.MkdirAll(c.dir, 0o700)
	_ = os.Chmod(c.dir, 0o700)
	return filepath.Join(c.dir, videoID+"."+ext)
}

func (c *Cache) SetLimit(limit int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	if limit > 0 {
		c.pruneLocked()
	}
}

// Limit returns the configured byte limit; 0 means caching is disabled and
// negative means unlimited.
func (c *Cache) Limit() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

// Size returns the bytes used by cached media files.
func (c *Cache) Size() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, _ := os.ReadDir(c.dir)
	var total int64
	for _, e := range entries {
		if !isCacheMedia(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Prune evicts the least recently used media files until the limit is met.
func (c *Cache) Prune() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
}

// ClearExcept removes all cached media except one track, used when caching is
// disabled and only the currently playing track needs its temporary file.
func (c *Cache) ClearExcept(videoID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, _ := os.ReadDir(c.dir)
	for _, e := range entries {
		name := e.Name()
		if !isCacheMedia(name) && filepath.Ext(name) != ".part" {
			continue
		}
		if videoID != "" && strings.HasPrefix(name, videoID+".") {
			continue
		}
		p := filepath.Join(c.dir, name)
		_ = os.Remove(p)
		_ = os.Remove(strings.TrimSuffix(p, filepath.Ext(p)) + ".json")
	}
}

func isCacheMedia(name string) bool {
	switch filepath.Ext(name) {
	case ".aac", ".mp4", ".m4a":
		return true
	}
	return false
}

// Lookup checks if a track's audio is fully cached on disk.
func (c *Cache) Lookup(videoID string) ([]byte, *ytmusic.StreamInfo, bool) {
	if c == nil {
		return nil, nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	audioPath := c.Path(videoID, "aac")
	metaPath := c.Path(videoID, "json")

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, nil, false
	}
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil, false
	}

	var info ytmusic.StreamInfo
	if json.Unmarshal(metaBytes, &info) != nil {
		return nil, nil, false
	}

	now := time.Now()
	_ = os.Chtimes(audioPath, now, now)
	return data, &info, true
}

// Save writes a complete audio file and its metadata to the cache and returns
// any write error.
func (c *Cache) Save(videoID string, info *ytmusic.StreamInfo, audioData []byte) error {
	if c == nil || len(audioData) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}

	audioPath := c.Path(videoID, "aac")
	metaPath := c.Path(videoID, "json")

	metaBytes, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := writeCacheFile(audioPath, audioData); err != nil {
		return err
	}
	if err := writeCacheFile(metaPath, metaBytes); err != nil {
		return err
	}

	c.pruneLocked()
	return nil
}

func writeCacheFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
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
	return os.Rename(tmpName, path)
}

func (c *Cache) pruneLocked() {
	if c.limit <= 0 {
		return
	}
	entries, _ := os.ReadDir(c.dir)
	type fileEntry struct {
		path string
		size int64
		mod  time.Time
	}
	var files []fileEntry
	var total int64
	for _, e := range entries {
		if !isCacheMedia(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		files = append(files, fileEntry{path: p, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}

	if total <= c.limit {
		return
	}

	slices.SortFunc(files, func(a, b fileEntry) int {
		return a.mod.Compare(b.mod)
	})

	for _, f := range files {
		if total <= c.limit {
			break
		}
		_ = os.Remove(f.path)
		_ = os.Remove(strings.TrimSuffix(f.path, filepath.Ext(f.path)) + ".json")
		total -= f.size
	}
}

// Clear removes all files from cache.
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = os.RemoveAll(c.dir)
	_ = os.MkdirAll(c.dir, 0o700)
}
