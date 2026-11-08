package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// stubUpdates replaces the updater with functions of the test, so that the
// settings page and the sidebar can be tested without the plugin, which only
// an app that called mygo.Use has.
func stubUpdates(t *testing.T) {
	t.Helper()
	origEnabled, origCheck, origWindow, origOnChange, origFound, origVersion :=
		updatesEnabled, updatesCheck, updatesWindow, updatesOnChange, updatesFound, appVersion
	t.Cleanup(func() {
		updatesEnabled, updatesCheck, updatesWindow, updatesOnChange, updatesFound, appVersion =
			origEnabled, origCheck, origWindow, origOnChange, origFound, origVersion
	})
}

func TestCheckUpdatesDefaultsOff(t *testing.T) {
	s := defaultSettings()
	if s.CheckUpdates {
		t.Fatal("CheckUpdates should default to false: package installs update elsewhere")
	}
	if s.UpdateAvailable != "" || s.UpdateSkipped != "" {
		t.Fatalf("a fresh install knows no version: %+v", s)
	}
}

func TestCheckUpdatesSetting(t *testing.T) {
	stubUpdates(t)

	a := newTestApp(t)
	a.updateWake = make(chan struct{}, 1)
	a.update = updateStatus{supported: true, version: "0.2.0"}
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("自动检查更新"); err != nil {
		t.Fatalf("toggle automatic checks: %v", err)
	}
	if !a.settings.CheckUpdates {
		t.Fatal("CheckUpdates was not enabled")
	}
	if !a.updateOn.Load() {
		t.Fatal("the background loop was not told to check")
	}
	select {
	case <-a.updateWake:
	default:
		t.Fatal("the background loop was not woken up")
	}
	// The switch is what the app boots with, so it has to outlive the app.
	if !loadSettings().CheckUpdates {
		t.Fatal("CheckUpdates was not saved")
	}

	if err := tt.Click("自动检查更新"); err != nil {
		t.Fatalf("toggle automatic checks back: %v", err)
	}
	if a.settings.CheckUpdates || a.updateOn.Load() {
		t.Fatal("CheckUpdates was not disabled")
	}
}

func TestCheckUpdatesButton(t *testing.T) {
	stubUpdates(t)
	opened := 0
	updatesWindow = func() { opened++ }

	a := newTestApp(t)
	a.update = updateStatus{supported: true, version: "0.2.0"}
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("检查更新"); err != nil {
		t.Fatalf("click 检查更新: %v", err)
	}
	if opened != 1 {
		t.Fatalf("the update window opened %d times, want 1", opened)
	}
}

func TestUpdatesUnsupportedHint(t *testing.T) {
	stubUpdates(t)

	a := newTestApp(t)
	// A build that cannot replace itself, wherever it was installed.
	a.update = updateStatus{supported: false, version: "0.2.0"}
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if !tt.HasText("当前安装的版本无法更新。") {
		t.Fatalf("settings should say why updates are unavailable, texts: %v", tt.Texts())
	}
	if tt.HasText("自动检查更新") {
		t.Fatal("a build that cannot update itself should not offer automatic checks")
	}
	// No manual check either: it is updated elsewhere.
	if _, ok := tt.Find("检查更新"); ok {
		t.Fatal("a version that cannot update itself should offer no 检查更新 button")
	}
	if tt.HasText("尚未检查更新") {
		t.Fatalf("a version that cannot update itself should say nothing about checks, texts: %v", tt.Texts())
	}
}

func TestUpdateStatusLines(t *testing.T) {
	stubUpdates(t)

	a := newTestApp(t)
	a.update = updateStatus{version: "0.2.0"}
	if got := a.versionLabel(); got != "版本 0.2.0" {
		t.Fatalf("versionLabel = %q, want 版本 0.2.0", got)
	}
	if got := a.updateStatusLine(); got != "尚未检查更新" {
		t.Fatalf("updateStatusLine = %q, want 尚未检查更新", got)
	}

	a.settings.UpdateLastCheck = time.Date(2026, 11, 7, 21, 5, 0, 0, time.Local)
	if got := a.updateStatusLine(); got != "上次检查：2026-11-07 21:05" {
		t.Fatalf("updateStatusLine = %q, want 上次检查：2026-11-07 21:05", got)
	}

	a.settings.UpdateAvailable = "0.3.0"
	if got := a.updateStatusLine(); got != "新版本 0.3.0 可用" {
		t.Fatalf("updateStatusLine = %q, want 新版本 0.3.0 可用", got)
	}
	if got := a.updateMenuLabel(); got != "更新到 0.3.0…" {
		t.Fatalf("updateMenuLabel = %q, want 更新到 0.3.0…", got)
	}

	a.settings.UpdateAvailable = ""
	if got := a.updateMenuLabel(); got != "检查更新…" {
		t.Fatalf("updateMenuLabel = %q, want 检查更新…", got)
	}

	a.update = updateStatus{}
	if got := a.versionLabel(); got != "开发版本" {
		t.Fatalf("versionLabel without a version = %q, want 开发版本", got)
	}
}

// A check that finds a version points at it in the sidebar, and nowhere
// does a window open on its own.
func TestBackgroundCheckBadge(t *testing.T) {
	stubUpdates(t)
	opened := 0
	updatesWindow = func() { opened++ }

	a := newTestApp(t)
	a.settings.CheckUpdates = true
	a.recordCheck("0.3.0")
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if opened != 0 {
		t.Fatal("a background check must not open the update window")
	}
	if !tt.HasText("有更新 0.3.0") {
		t.Fatalf("the sidebar should point at the version, texts: %v", tt.Texts())
	}
	if a.settings.UpdateLastCheck.IsZero() {
		t.Fatal("the check was not recorded")
	}

	if err := tt.Click("有更新 0.3.0"); err != nil {
		t.Fatalf("click the badge: %v", err)
	}
	if opened != 1 {
		t.Fatalf("the badge opened the update window %d times, want 1", opened)
	}

	// Up to date: the badge goes away.
	a.recordCheck("")
	tt.Frame()
	if tt.HasText("有更新") {
		t.Fatalf("the badge should be gone, texts: %v", tt.Texts())
	}
}

func TestSkippedVersionIsNotOffered(t *testing.T) {
	stubUpdates(t)

	a := newTestApp(t)
	a.settings.CheckUpdates = true
	a.settings.UpdateSkipped = "0.3.0"
	a.recordCheck("0.3.0")
	if a.settings.UpdateAvailable != "" {
		t.Fatalf("a skipped version came back: %+v", a.settings)
	}

	// The version after it is news again.
	a.recordCheck("0.4.0")
	if a.settings.UpdateAvailable != "0.4.0" {
		t.Fatalf("UpdateAvailable = %q, want 0.4.0", a.settings.UpdateAvailable)
	}
}

// Skipping in the update window takes the badge away.
func TestWindowSkipClearsBadge(t *testing.T) {
	stubUpdates(t)
	updatesFound = func() (string, time.Time) { return "", time.Now() }

	a := newTestApp(t)
	a.settings.UpdateAvailable = "0.3.0"
	a.adoptWindowState()
	if a.settings.UpdateAvailable != "" {
		t.Fatalf("the badge survived a skip: %+v", a.settings)
	}
	if a.settings.UpdateSkipped != "0.3.0" {
		t.Fatalf("UpdateSkipped = %q, want 0.3.0", a.settings.UpdateSkipped)
	}
}

// A launch into the version the previous one found is not news.
func TestRefreshUpdatesDropsRunningVersion(t *testing.T) {
	stubUpdates(t)
	updatesEnabled = func() bool { return true }
	appVersion = func() string { return "0.3.0" }

	a := newTestApp(t)
	a.settings.UpdateAvailable = "0.3.0"
	a.refreshUpdates()
	if a.settings.UpdateAvailable != "" {
		t.Fatalf("the badge points at the running version: %+v", a.settings)
	}
	if !a.update.supported || a.update.version != "0.3.0" {
		t.Fatalf("update status = %+v", a.update)
	}
}

func TestSettingsScrollToUpdates(t *testing.T) {
	stubUpdates(t)
	opened := 0
	updatesWindow = func() { opened++ }

	a := newTestApp(t)
	a.update = updateStatus{supported: true, version: "0.2.0"}
	a.settings.UpdateAvailable = "0.3.0"
	a.settingsModalOpen = true
	// The smallest window: the sections below the fold need scrolling.
	tt := ui.NewTester(a.view, 920, 620)
	tt.Frame()

	// A wheel glides the list over a moment, which the test clock shows.
	tt.Wheel(600, 300, 0, 600)
	tt.Advance(time.Second)
	tt.Wheel(600, 300, 0, 600)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("检查更新"); err != nil {
		t.Fatalf("click 检查更新 after scrolling: %v", err)
	}
	if opened != 1 {
		t.Fatalf("the update window opened %d times, want 1", opened)
	}
}

// runUpdateLoop starts the loop of a test, which stops, and is waited for,
// when the test ends: before the stubs of the test are put back.
func runUpdateLoop(t *testing.T, a *app) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.updateLoop(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second): // a guard, not a deadline
			t.Fatal("the loop kept running after the app quit")
		}
	})
}

// fastUpdateWaits makes the loop check at once, rather than a day from now.
func fastUpdateWaits(t *testing.T) {
	t.Helper()
	first, interval, retry := updateFirstCheck, updateInterval, updateRetryWait
	updateFirstCheck, updateInterval, updateRetryWait = time.Millisecond, time.Hour, time.Hour
	t.Cleanup(func() { updateFirstCheck, updateInterval, updateRetryWait = first, interval, retry })
}

// The background loop checks on its own, and stops with the app.
func TestUpdateLoopChecksInTheBackground(t *testing.T) {
	stubUpdates(t)
	updatesEnabled = func() bool { return true }
	checked := make(chan struct{}, 4)
	updatesCheck = func(context.Context) (*mygo.Update, error) {
		checked <- struct{}{}
		return nil, nil
	}
	fastUpdateWaits(t)

	a := newTestApp(t)
	a.updateWake = make(chan struct{}, 1)
	a.updateOn.Store(true)
	runUpdateLoop(t, a)

	select {
	case <-checked:
	case <-time.After(10 * time.Second): // a guard, not a deadline
		t.Fatal("the loop did not check for updates in the background")
	}
}

// The switch off means no checks, however long the app runs.
func TestUpdateLoopStaysQuietWhenOff(t *testing.T) {
	stubUpdates(t)
	updatesEnabled = func() bool { return true }
	var checked atomic.Int32
	updatesCheck = func(context.Context) (*mygo.Update, error) {
		checked.Add(1)
		return nil, nil
	}
	fastUpdateWaits(t)

	a := newTestApp(t)
	a.updateWake = make(chan struct{}, 1)
	runUpdateLoop(t, a)

	// Several first checks are due by then, each of which the switch turns
	// into nothing.
	time.Sleep(20 * time.Millisecond)
	if n := checked.Load(); n != 0 {
		t.Fatalf("the loop checked %d times with checks off", n)
	}
}

// A development build says nothing about updates: it cannot install one,
// and whoever runs it builds it.
func TestDevelopmentBuildHidesUpdates(t *testing.T) {
	stubUpdates(t)

	a := newTestApp(t)
	a.update = updateStatus{supported: false, dev: true, version: "0.3.0"}
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if tt.HasText("软件更新") || tt.HasText("检查更新") {
		t.Fatalf("a development build should hide the update section, texts: %v", tt.Texts())
	}
	// The version is still there, under About.
	if !tt.HasText("版本 0.3.0") {
		t.Fatalf("the About section should still name the version, texts: %v", tt.Texts())
	}
}
