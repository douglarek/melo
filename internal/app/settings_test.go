package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
)

func TestCloseToTraySetting(t *testing.T) {
	a := newTestApp(t)
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("保持在菜单栏"); err != nil {
		t.Fatalf("select tray mode: %v", err)
	}
	if !a.settings.CloseToTray {
		t.Fatal("CloseToTray was not enabled")
	}
	if err := tt.Click("退出应用"); err != nil {
		t.Fatalf("select exit mode: %v", err)
	}
	if a.settings.CloseToTray {
		t.Fatal("CloseToTray was not disabled")
	}
}

func TestWriteFileAtomicReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := writeFileAtomic(path, []byte(`{"new":true}`)); err != nil {
		t.Fatalf("atomic write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"new":true}` {
		t.Fatalf("file = %q, want the new contents", data)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file left: %v", err)
	}
}

func TestDefaultSettings(t *testing.T) {
	s := defaultSettings()
	if !s.CloseToTray {
		t.Fatal("CloseToTray should default to true")
	}
	if s.CacheMB != 1024 {
		t.Fatalf("CacheMB = %d, want 1024", s.CacheMB)
	}
	if !s.TrackNotifications {
		t.Fatal("TrackNotifications should default to true")
	}
}

func TestCacheSizeSetting(t *testing.T) {
	a := newTestApp(t)
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("2G"); err != nil {
		t.Fatalf("select 2G: %v", err)
	}
	if a.settings.CacheMB != 2048 {
		t.Fatalf("CacheMB = %d, want 2048", a.settings.CacheMB)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("select off: %v", err)
	}
	if a.settings.CacheMB != 0 {
		t.Fatalf("CacheMB = %d, want 0", a.settings.CacheMB)
	}
}

func TestTrackNotificationsSetting(t *testing.T) {
	a := newTestApp(t)
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("切歌时显示桌面通知"); err != nil {
		t.Fatalf("toggle notifications: %v", err)
	}
	if a.settings.TrackNotifications {
		t.Fatal("notifications were not disabled")
	}
	if err := tt.Click("切歌时显示桌面通知"); err != nil {
		t.Fatalf("toggle notifications back: %v", err)
	}
	if !a.settings.TrackNotifications {
		t.Fatal("notifications were not enabled")
	}
}

// TestOpenSettingsCountsTheWholeCache checks that the size the settings window
// reports is what the disk holds: the audio of tracks, the covers and what a
// download left behind, not the audio alone.
func TestClearCacheKeepsPlayingTrack(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "cache")
	cache := player.NewCache(dir, 1<<30)
	a.pl.Cache = cache
	a.pl.Resolve = func(context.Context, string) (*ytmusic.StreamInfo, error) {
		return nil, errors.New("no stream in the test")
	}
	a.pl.SetQueue([]ytmusic.Track{{ID: "active", Title: "Active"}}, 0)

	active := []string{
		filepath.Join(dir, "active.mp4"),
		filepath.Join(cache.SabrDir(), "active.m4a"),
	}
	dropped := []string{
		filepath.Join(dir, "other.mp4"),
		filepath.Join(cache.SabrDir(), "other.m4a"),
	}
	for _, path := range append(append([]string{}, active...), dropped...) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if err := tt.Click("清理缓存"); err != nil {
		t.Fatalf("clear cache: %v", err)
	}

	for _, path := range active {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("kept file %s: %v", path, err)
		}
	}
	for _, path := range dropped {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("file %s was kept", path)
		}
	}
}

func TestOpenSettingsCountsTheWholeCache(t *testing.T) {
	a := newTestApp(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cache := player.NewCache(cacheDir, 1<<30)
	files := map[string]int{
		filepath.Join(cacheDir, "a.mp4"):           3,
		filepath.Join(cache.SabrDir(), "b.m4a"):    5,
		filepath.Join(cache.ArtworkDir(), "c.jpg"): 7,
		filepath.Join(cacheDir, "d.mp4.part"):      11,
	}
	for path, size := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	a.pl.Cache = cache

	a.openSettings()

	if !a.settingsModalOpen {
		t.Fatal("the settings window was not opened")
	}
	if got, want := cache.Size(), int64(26); got != want {
		t.Fatalf("the cache reports %d bytes, want %d", got, want)
	}
}
