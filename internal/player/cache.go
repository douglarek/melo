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

// Sizes is what the disk cache holds, by kind.
type Sizes struct {
	// Songs is the audio of tracks, in the cache's directory and in SabrDir:
	// the sidecar files of a track, its metadata and the marker of a complete
	// download, count with it.
	Songs int64
	// Artwork is the covers the app downloaded.
	Artwork int64
	// Temp is what downloads wrote and did not finish.
	Temp int64
}

// Total returns the bytes the cache holds: what its limit counts and what the
// settings window reports.
func (s Sizes) Total() int64 { return s.Songs + s.Artwork + s.Temp }

// The cache keeps everything under one directory and one limit: the audio of
// tracks in it and in sabrDirName, the covers in artworkDirName, and the
// temporary files of downloads.
const (
	sabrDirName    = "sabr"
	artworkDirName = "artwork"

	// tempGrace is how long after its last write pruning leaves a file
	// alone: a download still writing one touches it, and the track just
	// played is not the one used longest ago.
	tempGrace = 10 * time.Minute
	// staleTempAge is when a file a download left behind is certainly
	// abandoned: Migrate drops those.
	staleTempAge = 24 * time.Hour
	// pruneHeadroom is the share of the limit pruning leaves free, so that
	// the downloads between two prunes have room.
	pruneHeadroom = 10
	// artworkBudget is the most covers may take of a cache over its limit,
	// short of the limit's own quarter: a page of covers is worth less than
	// the tracks they stand beside, and comes back from the network in a
	// moment.
	artworkBudget = 256 << 20
)

// Cache manages local disk storage of streamed music tracks, their covers and
// the temporary files of downloads in progress.
type Cache struct {
	dir   string
	mu    sync.Mutex
	limit int64
	// keep is the track playing: pruning leaves its files alone.
	keep string
	// sizes is what the last scan found, for the settings window, which asks
	// every frame.
	sizes Sizes
}

// NewCache returns a cache located at dir with a size limit in bytes.
func NewCache(dir string, limit int64) *Cache {
	return &Cache{
		dir:   dir,
		limit: limit,
	}
}

// SabrDir returns where the SABR path drains tracks, inside the cache so that
// its limit counts them.
func (c *Cache) SabrDir() string {
	if c == nil {
		return ""
	}
	return filepath.Join(c.dir, sabrDirName)
}

// ArtworkDir returns where covers are cached, inside the cache so that its
// limit counts them.
func (c *Cache) ArtworkDir() string {
	if c == nil {
		return ""
	}
	return filepath.Join(c.dir, artworkDirName)
}

// Path returns the absolute file path for a cached track.
func (c *Cache) Path(videoID, ext string) string {
	videoID = sanitizeID(videoID)
	ext = filepath.Base(strings.TrimPrefix(ext, "."))
	if c == nil {
		return filepath.Join("/tmp", videoID+"."+ext)
	}
	_ = os.MkdirAll(c.dir, 0o700)
	_ = os.Chmod(c.dir, 0o700)
	return filepath.Join(c.dir, videoID+"."+ext)
}

// sanitizeID returns a video id that names a file of the cache's directory.
func sanitizeID(videoID string) string {
	videoID = filepath.Base(strings.TrimSpace(videoID))
	if videoID == "." || videoID == ".." || videoID == "" {
		return "invalid"
	}
	return videoID
}

// SetLimit sets the bytes the whole cache may take, 0 for no cache and a
// negative limit for as much as the disk holds, pruning what is over it.
func (c *Cache) SetLimit(limit int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	c.pruneLocked()
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

// SetKeep names the track playing, whose files pruning and clearing leave
// alone.
func (c *Cache) SetKeep(videoID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(videoID) == "" {
		c.keep = ""
		return
	}
	c.keep = sanitizeID(videoID)
}

// Refresh counts what the cache holds, for what the app shows of it.
func (c *Cache) Refresh() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sizes = scanSizes(c.filesLocked())
}

// Sizes returns what the cache holds, as the last Refresh, Prune or write
// found it.
func (c *Cache) Sizes() Sizes {
	if c == nil {
		return Sizes{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sizes
}

// Size returns the bytes the cache holds, as the last Refresh, Prune or write
// found them.
func (c *Cache) Size() int64 {
	return c.Sizes().Total()
}

// Prune evicts what the cache holds until it is under its limit: abandoned
// downloads first, then covers beyond their share, then whatever was used
// longest ago.
func (c *Cache) Prune() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
}

// ClearExcept removes all cached files except one track's, used when caching
// is disabled and only the currently playing track needs its temporary file.
func (c *Cache) ClearExcept(videoID string) {
	if c == nil {
		return
	}
	keep := ""
	if strings.TrimSpace(videoID) != "" {
		keep = sanitizeID(videoID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clearExcept(c.SabrDir(), keep)
	clearExcept(c.dir, keep)
	_ = os.RemoveAll(c.ArtworkDir())
	c.sizes = scanSizes(c.filesLocked())
}

// clearExcept removes the files of dir that are not one track's, whose name
// starts with its id.
func clearExcept(dir, keep string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if keep != "" && strings.HasPrefix(name, keep+".") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

func isCacheMedia(name string) bool {
	switch filepath.Ext(name) {
	case ".aac", ".mp4", ".m4a":
		return true
	}
	return false
}

func isTemp(name string) bool {
	switch filepath.Ext(name) {
	case ".part", ".tmp":
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

// Clear removes all files from cache.
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = os.RemoveAll(c.dir)
	_ = os.MkdirAll(c.dir, 0o700)
	c.sizes = Sizes{}
}

// cacheFile is one file the cache holds, as filesLocked found it.
type cacheFile struct {
	path string
	size int64
	mod  time.Time
	kind fileKind
}

type fileKind int

const (
	// fileSong is the audio of a track.
	fileSong fileKind = iota
	// fileArtwork is a cached cover.
	fileArtwork
	// fileTemp is what a download wrote and did not finish.
	fileTemp
	// fileMeta is a track's sidecar: its metadata, or the marker of a
	// complete download. It counts and goes with its track.
	fileMeta
)

// filesLocked lists every file the cache holds: those of its directory, of
// SabrDir and of ArtworkDir.
func (c *Cache) filesLocked() []cacheFile {
	var files []cacheFile
	scan := func(dir string, classify func(string) fileKind) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, cacheFile{
				path: filepath.Join(dir, e.Name()),
				size: info.Size(),
				mod:  info.ModTime(),
				kind: classify(e.Name()),
			})
		}
	}
	classifyTrack := func(name string) fileKind {
		switch {
		case isTemp(name):
			return fileTemp
		case isCacheMedia(name):
			return fileSong
		default:
			return fileMeta
		}
	}
	scan(c.dir, classifyTrack)
	scan(c.SabrDir(), classifyTrack)
	scan(c.ArtworkDir(), func(name string) fileKind {
		if isTemp(name) {
			return fileTemp
		}
		return fileArtwork
	})
	return files
}

// scanSizes sums the files of each kind.
func scanSizes(files []cacheFile) Sizes {
	var sizes Sizes
	for _, f := range files {
		switch f.kind {
		case fileArtwork:
			sizes.Artwork += f.size
		case fileTemp:
			sizes.Temp += f.size
		default:
			sizes.Songs += f.size
		}
	}
	return sizes
}

// pruneLocked evicts what the cache holds until it is under its limit:
// abandoned downloads first, then covers beyond their share, then whatever
// was used longest ago, leaving the track playing and what a download is
// still writing.
func (c *Cache) pruneLocked() {
	files := c.filesLocked()
	c.sizes = scanSizes(files)
	if c.limit <= 0 {
		return
	}
	var total, artworkTotal int64
	for _, f := range files {
		total += f.size
		if f.kind == fileArtwork {
			artworkTotal += f.size
		}
	}
	if total <= c.limit {
		return
	}

	var temps, arts, songs, meta []cacheFile
	for _, f := range files {
		switch f.kind {
		case fileTemp:
			temps = append(temps, f)
		case fileArtwork:
			arts = append(arts, f)
		case fileSong:
			songs = append(songs, f)
		case fileMeta:
			meta = append(meta, f)
		}
	}

	oldestFirst := func(a, b cacheFile) int { return a.mod.Compare(b.mod) }
	drop := func(f cacheFile) int64 {
		if err := os.Remove(f.path); err != nil {
			return 0
		}
		freed := f.size
		if f.kind == fileSong {
			for _, side := range sidecars(f.path) {
				if info, err := os.Stat(side); err == nil && os.Remove(side) == nil {
					freed += info.Size()
				}
			}
		}
		return freed
	}
	now := time.Now()
	target := c.limit - c.limit/pruneHeadroom

	// What a download left unfinished, and nothing plays again.
	slices.SortFunc(temps, oldestFirst)
	for _, f := range temps {
		if total <= target {
			break
		}
		if now.Sub(f.mod) < tempGrace {
			continue
		}
		total -= drop(f)
	}

	// Covers take no more than their share of a cache over its limit, so that
	// browsing does not push tracks out of it.
	budget := min(c.limit/4, int64(artworkBudget))
	slices.SortFunc(arts, oldestFirst)
	for len(arts) > 0 && artworkTotal > budget {
		freed := drop(arts[0])
		artworkTotal -= freed
		total -= freed
		arts = arts[1:]
	}

	// Then the files used longest ago, tracks and covers alike.
	rest := slices.Concat(arts, songs)
	slices.SortFunc(rest, oldestFirst)
	for _, f := range rest {
		if total <= target {
			break
		}
		if f.kind == fileSong {
			if now.Sub(f.mod) < tempGrace {
				continue
			}
			if c.keep != "" && strings.HasPrefix(filepath.Base(f.path), c.keep+".") {
				continue
			}
		}
		total -= drop(f)
	}

	// Sidecars whose track is gone: of a download that failed, or of a track
	// an older version removed without them.
	for _, f := range meta {
		if total <= target {
			break
		}
		if orphanedSidecar(f.path) {
			total -= drop(f)
		}
	}
	c.sizes = scanSizes(c.filesLocked())
}

// sidecars returns the files that go with an audio file: its metadata, and
// the marker of a complete SABR download.
func sidecars(path string) []string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return []string{base + ".json", path + ".ok"}
}

// orphanedSidecar reports whether the track a sidecar belongs to is gone.
func orphanedSidecar(path string) bool {
	var candidates []string
	if base, ok := strings.CutSuffix(path, ".ok"); ok {
		candidates = append(candidates, base)
	} else if base, ok := strings.CutSuffix(path, ".json"); ok {
		for _, ext := range []string{".aac", ".mp4", ".m4a"} {
			candidates = append(candidates, base+ext)
		}
	} else {
		return false
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return false
		}
	}
	return true
}

// Migrate drops what no version reads any more: audio that older versions
// cached under another name, and downloads left unfinished. It returns the
// bytes it freed.
func (c *Cache) Migrate() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var freed int64
	now := time.Now()
	entries, _ := os.ReadDir(c.dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		stale := strings.Contains(name, ".heaac.")
		if !stale && isTemp(name) && now.Sub(info.ModTime()) > staleTempAge {
			stale = true
		}
		if !stale {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, name)); err == nil {
			freed += info.Size()
		}
	}
	c.sizes = scanSizes(c.filesLocked())
	return freed
}
