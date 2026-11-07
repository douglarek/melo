package app

import (
	"github.com/douglarek/melo/locales"
	"runtime"

	"github.com/egoist/mygo"
)

// setMenu configures the macOS application menu bar.
func (a *app) setMenu() {
	run := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			a.post(fn)
		}
	}
	name := "Melo"

	appMenu := []*mygo.MenuItem{
		{Role: mygo.RoleAbout, Label: locales.AboutPrefix() + name},
		mygo.Separator(),
		{Label: locales.CheckForUpdatesMenu(), Click: func(*mygo.MenuItem, *mygo.Window) { a.checkUpdates() }},
		{Label: locales.SettingsMenu(), Accelerator: "CmdOrCtrl+,", Click: run(func() { a.settingsModalOpen = true })},
		mygo.Separator(),
	}

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

	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
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
	}))
}
