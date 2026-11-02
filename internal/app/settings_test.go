package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCloseToTraySetting(t *testing.T) {
	a := newTestApp()
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
	a := newTestApp()
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
	a := newTestApp()
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
