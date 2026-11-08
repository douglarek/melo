package app

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

func cacheLimitBytes(mb int64) int64 {
	if mb < 0 {
		return -1
	}
	return mb * 1024 * 1024
}

// maintenanceLoop persists playback position regularly and keeps the disk
// cache within its configured limit even though downloads happen outside
// Cache.Save.
func (a *app) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.savePlayerSession()
			a.maintainCache()
		}
	}
}

func (a *app) maintainCache() {
	if a.pl == nil || a.pl.Cache == nil {
		return
	}
	cache := a.pl.Cache
	keep := ""
	if cur := a.pl.Current(); cur != nil {
		keep = cur.ID
	}
	// The cache counts the audio of tracks with their covers, and prunes one
	// directory, so the track playing has to be known before it runs.
	cache.SetKeep(keep)
	if cache.Limit() == 0 {
		cache.ClearExcept(keep)
		return
	}
	cache.Prune()
}

func (a *app) applyCacheLimit() {
	if a.pl == nil || a.pl.Cache == nil {
		return
	}
	limit := cacheLimitBytes(a.settings.CacheMB)
	a.pl.Cache.SetLimit(limit)
	if limit == 0 {
		a.maintainCache()
	}
}

func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// releaseMemorySoon returns freed heap pages to the OS after navigation or
// hiding to the tray. The delay keeps foreground frames from paying for GC.
func (a *app) releaseMemorySoon() {
	if a.memoryTimer != nil {
		a.memoryTimer.Stop()
	}
	a.memoryTimer = time.AfterFunc(30*time.Second, func() {
		debug.FreeOSMemory()
	})
}

// releaseMemoryNow is for explicit user actions such as clearing the cache.
func (a *app) releaseMemoryNow() {
	go debug.FreeOSMemory()
}
