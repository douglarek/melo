package app

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// useUpdatePlugin adds the update window of github.com/egoist/mygo/plugins/updater,
// drawn in native UI, so it needs no webview. Signed updates that `mygo
// build` publishes are what it installs: it downloads a new version, checks
// its signature against the key in mygo.json, replaces the app with it and
// restarts into it.
//
// The window opens only when the user asks for it: from the sidebar, the
// tray, the menu or the settings. Background checks are Melo's own
// (updateLoop below), because the plugin's open its window as soon as they
// find a version, and Melo points at it in the sidebar and the tray
// instead.
func useUpdatePlugin() {
	mygo.Use(native.New(updater.Options{
		// The interface is Chinese, so the window follows it rather than
		// the system's language.
		Language: "zh-Hans",
		// The window checks when the user asks to, and never in the
		// background: see updateLoop.
		DisableAutomaticChecks: true,
	}))
}

// The waits of updateLoop, replaced by tests.
var (
	// updateFirstCheck is when the first background check of a launch runs:
	// soon enough that a version that came out since the app last ran shows
	// in the sidebar when it opens again.
	updateFirstCheck = 10 * time.Second
	// updateInterval is the time between background checks.
	updateInterval = 24 * time.Hour
	// updateRetryWait is when the next check runs after one that failed,
	// rather than a whole day: the network may be back sooner.
	updateRetryWait = time.Hour
)

// updateStatus is what the settings page says about this build's updates.
type updateStatus struct {
	// supported reports whether this build can replace itself: a packaged
	// build where it may write, not a development build, and not an app a
	// package manager installed.
	supported bool
	// version is the version that runs.
	version string
}

// Replaced by tests.
var (
	updatesEnabled  = mygo.Updater.Enabled
	updatesCheck    = mygo.Updater.Check
	updatesWindow   = updater.CheckForUpdates
	updatesOnChange = updater.OnChange
	updatesFound    = func() (available string, lastCheck time.Time) {
		return updater.AvailableVersion(), updater.LastCheck()
	}
	appVersion = mygo.App.Version
)

// setupUpdates reads this build's update support for the settings page and
// follows the update window: it found a version, or the user skipped the
// one the sidebar offered.
func (a *app) setupUpdates() {
	a.refreshUpdates()
	a.updateOn.Store(a.settings.CheckUpdates)
	// The plugin's own checks are off for good: they open its window the
	// moment they find a version, and Melo points at one in the sidebar and
	// the tray instead. Set here as well as in the options, so a state file
	// of an earlier build cannot bring them back.
	updater.SetAutomaticChecks(false)
	updatesOnChange(func() {
		// OnChange runs on the main thread; posting redraws the app, whose
		// sidebar and tray show the badge.
		a.post(a.adoptWindowState)
	})
}

// updateLoop checks for new versions in the background while the user asked
// for that, and records what it finds for the sidebar, the tray and the
// settings page. A check that fails is retried sooner than the interval.
func (a *app) updateLoop(ctx context.Context) {
	wait := updateFirstCheck
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.updateWake:
			// The switch moved: check soon, rather than a day from now.
			timer.Stop()
			wait = updateFirstCheck
			continue
		case <-timer.C:
		}
		wait = updateInterval
		if !a.updateOn.Load() || !updatesEnabled() {
			continue
		}
		if a.checkInBackground(ctx) != nil {
			wait = updateRetryWait
		}
	}
}

// checkInBackground asks the update feed for a newer version and hands what
// it found to recordCheck on the main thread.
func (a *app) checkInBackground(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	up, err := updatesCheck(ctx)
	if err != nil {
		return err
	}
	found := ""
	if up != nil {
		found = up.Version
	}
	a.post(func() { a.recordCheck(found) })
	return nil
}

// recordCheck keeps what a check found: the version the sidebar and the
// tray point at, and when it was found. It runs on the main thread.
func (a *app) recordCheck(found string) {
	a.settings.UpdateLastCheck = time.Now()
	switch {
	case found == "" || found == a.settings.UpdateSkipped:
		a.settings.UpdateAvailable = ""
	default:
		a.settings.UpdateAvailable = found
	}
	a.settings.save()
	a.refreshTray()
}

// adoptWindowState follows the update window: the version its check found,
// or none because the app is up to date or the user skipped the version the
// sidebar pointed at. It runs on the main thread.
func (a *app) adoptWindowState() {
	available, last := updatesFound()
	if !last.IsZero() {
		a.settings.UpdateLastCheck = last
	}
	switch {
	case available != "":
		a.settings.UpdateAvailable = available
	case a.settings.UpdateAvailable != "":
		// Skipped, or gone from the feed: do not point at it again.
		a.settings.UpdateSkipped = a.settings.UpdateAvailable
		a.settings.UpdateAvailable = ""
	}
	a.settings.save()
	a.refreshTray()
}

// refreshUpdates reads what this build can do about updates for the settings
// page, and drops a version that the previous launch found and that this one
// runs: it is not news.
func (a *app) refreshUpdates() {
	a.update = updateStatus{supported: updatesEnabled(), version: appVersion()}
	if v := a.settings.UpdateAvailable; v != "" && v == a.update.version {
		a.settings.UpdateAvailable = ""
		a.settings.save()
	}
}

// setUpdateChecks applies the settings switch: the background loop checks
// from now on, soon after the switch moves, or stops.
func (a *app) setUpdateChecks(on bool) {
	a.settings.CheckUpdates = on
	a.settings.save()
	a.updateOn.Store(on)
	// Buffered, and never waited on: a poke the loop has not taken yet is
	// as good as this one.
	select {
	case a.updateWake <- struct{}{}:
	default:
	}
}

// checkUpdates opens the update window as the user asked, from the sidebar,
// the tray, the menu or the settings. It checks at once and says when Melo
// is up to date; a new version offers to download, install and restart.
func (a *app) checkUpdates() {
	updatesWindow()
}

// versionLabel names the running version for the settings page.
func (a *app) versionLabel() string {
	if a.update.version == "" {
		return "开发版本"
	}
	return "版本 " + a.update.version
}

// updateStatusLine summarizes the last check for the settings page.
func (a *app) updateStatusLine() string {
	switch {
	case a.settings.UpdateAvailable != "":
		return "新版本 " + a.settings.UpdateAvailable + " 可用"
	case a.settings.UpdateLastCheck.IsZero():
		return "尚未检查更新"
	default:
		return "上次检查：" + a.settings.UpdateLastCheck.Local().Format("2006-01-02 15:04")
	}
}
