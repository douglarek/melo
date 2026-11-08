package app

import (
	"github.com/douglarek/melo/locales"
	"runtime"

	"github.com/egoist/mygo"
)

// setMenu configures the macOS application menu bar.
func (a *app) setMenu() {
	mygo.App.SetMenu(mygo.NewMenu(a.menuItems("Melo")))
}

// menuItems is the application menu: the app's own items, then the Edit,
// Controls and Window submenus.
func (a *app) menuItems(name string) []*mygo.MenuItem {
	run := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			a.post(fn)
		}
	}

	appMenu := []*mygo.MenuItem{
		{Role: mygo.RoleAbout, Label: locales.AboutPrefix() + name},
		mygo.Separator(),
	}
	// A build that cannot install an update offers no check for one, as its
	// settings page hides the update section.
	if a.update.supported {
		appMenu = append(appMenu, &mygo.MenuItem{Label: locales.CheckForUpdatesMenu(), Click: func(*mygo.MenuItem, *mygo.Window) { a.checkUpdates() }})
	}
	appMenu = append(appMenu,
		&mygo.MenuItem{Label: locales.SettingsMenu(), Accelerator: "CmdOrCtrl+,", Click: run(a.openSettings)},
		mygo.Separator(),
	)

	if runtime.GOOS == "darwin" {
		appMenu = append(appMenu,
			&mygo.MenuItem{Role: mygo.RoleServices, Label: locales.Services()},
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleHide, Label: locales.HidePrefix() + name},
			&mygo.MenuItem{Role: mygo.RoleHideOthers, Label: locales.HideOthers()},
			&mygo.MenuItem{Role: mygo.RoleUnhide, Label: locales.ShowAll()},
			mygo.Separator(),
		)
	}
	appMenu = append(appMenu, &mygo.MenuItem{Role: mygo.RoleQuit, Label: locales.QuitPrefix() + name})

	return []*mygo.MenuItem{
		{Label: name, Submenu: appMenu},
		{Label: locales.Edit(), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo, Label: locales.Undo()},
			{Role: mygo.RoleRedo, Label: locales.Redo()},
			mygo.Separator(),
			{Role: mygo.RoleCut, Label: locales.Cut()},
			{Role: mygo.RoleCopy, Label: locales.Copy()},
			{Role: mygo.RolePaste, Label: locales.Paste()},
			{Role: mygo.RoleSelectAll, Label: locales.SelectAll()},
		}},
		{Label: locales.Controls(), Submenu: []*mygo.MenuItem{
			{Label: locales.PlayPause(), Click: run(func() { a.pl.TogglePlay() })},
			{Label: locales.Next(), Accelerator: "CmdOrCtrl+Right", Click: run(func() { a.pl.Next() })},
			{Label: locales.Previous(), Accelerator: "CmdOrCtrl+Left", Click: run(func() { a.pl.Prev() })},
			mygo.Separator(),
			{Label: locales.Shuffle(), Click: run(func() { a.pl.SetShuffle(!a.pl.Shuffle()) })},
			{Label: locales.CycleRepeatMode(), Click: run(func() { a.pl.ToggleRepeat() })},
			mygo.Separator(),
			{Label: locales.NowPlaying(), Accelerator: "CmdOrCtrl+L", Click: run(func() {
				a.nowPlayingOpen = !a.nowPlayingOpen && a.pl.Current() != nil
			})},
		}},
		{Label: locales.Window(), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: locales.Minimize()},
			{Role: mygo.RoleZoom, Label: locales.Zoom()},
			{Role: mygo.RoleClose, Label: locales.CloseWindow()},
		}},
	}
}
