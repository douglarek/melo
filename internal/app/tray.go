package app

import (
	_ "embed"
	"github.com/douglarek/melo/locales"
	"runtime"

	"github.com/egoist/mygo"
)

// macOS tints the black-and-transparent template image to match the menu
// bar; Linux and Windows use the color image.
//
// Edit tray/tray-template.svg and tray/tray-color.svg, then regenerate the
// PNGs with the command in tray/README.md.
//
//go:embed tray/tray-template.png
var trayTemplate []byte

//go:embed tray/tray-color.png
var trayColor []byte

func (a *app) setupTray() {
	icon := trayColor
	template := false
	if runtime.GOOS == "darwin" {
		icon = trayTemplate
		template = true
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "melo",
		Icon:           icon,
		IconIsTemplate: template,
		ToolTip:        "Melo",
	})
	if err != nil {
		a.trayErr = err
		return
	}
	a.tray = tray

	if runtime.GOOS == "linux" {
		// AppIndicator only exposes menus.
		tray.SetMenu(a.trayMenu())
	} else {
		tray.OnClick(a.toggleWindow)
		tray.OnRightClick(func() {
			tray.SetMenu(a.trayMenu())
			defer tray.SetMenu(nil)
			tray.PopUpMenu()
		})
	}
}

// trayMenu is the menu of the tray icon, built each time it shows: its
// update item names the version a background check found, when there is one.
func (a *app) trayMenu() *mygo.Menu {
	items := []*mygo.MenuItem{
		{Label: locales.ShowHideApp(), Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleWindow() }},
		mygo.Separator(),
		{Label: locales.PlayPauseMenu(), Click: func(*mygo.MenuItem, *mygo.Window) { a.togglePlayback() }},
		{Label: locales.Previous(), Click: func(*mygo.MenuItem, *mygo.Window) { a.playerPrev() }},
		{Label: locales.Next(), Click: func(*mygo.MenuItem, *mygo.Window) { a.playerNext() }},
		mygo.Separator(),
	}
	// A build that cannot install an update offers no check for one, as its
	// settings page hides the update section.
	if a.update.supported {
		items = append(items,
			&mygo.MenuItem{Label: a.updateMenuLabel(), Click: func(*mygo.MenuItem, *mygo.Window) { a.checkUpdates() }},
			mygo.Separator(),
		)
	}
	items = append(items, &mygo.MenuItem{Label: locales.QuitApp(), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }})
	return mygo.NewMenu(items)
}

// updateMenuLabel names the update item after what a background check found.
func (a *app) updateMenuLabel() string {
	if v := a.offeredUpdate(); v != "" {
		return locales.UpdateToPrefix() + v + "…"
	}
	return locales.CheckForUpdatesMenu()
}

// offeredUpdate is the version the app points at: none for a build that
// cannot install one. Such a build says nothing about updates anywhere — the
// sidebar, the menus and the tray included.
func (a *app) offeredUpdate() string {
	if !a.update.supported {
		return ""
	}
	return a.settings.UpdateAvailable
}

// refreshTray points the tooltip, and on Linux the menu, at a version a
// background check found. It runs on the main thread.
func (a *app) refreshTray() {
	if a.tray == nil {
		return
	}
	tip := "Melo"
	if v := a.offeredUpdate(); v != "" {
		tip = "Melo " + v + locales.IsAvailableSuffix()
	}
	a.tray.SetToolTip(tip)
	if runtime.GOOS == "linux" {
		// The AppIndicator shows the menu set last.
		a.tray.SetMenu(a.trayMenu())
	}
}

func (a *app) toggleWindow() {
	if a.win == nil {
		return
	}
	if a.win.IsVisible() {
		a.win.Hide()
		return
	}
	a.win.Show()
	a.win.Focus()
}

func (a *app) togglePlayback() {
	if a.pl != nil {
		a.pl.TogglePlay()
	}
}

func (a *app) playerNext() {
	if a.pl != nil {
		a.pl.Next()
	}
}

func (a *app) playerPrev() {
	if a.pl != nil {
		a.pl.Prev()
	}
}
