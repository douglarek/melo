package app

import (
	_ "embed"
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

	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示 / 隐藏 Melo", Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleWindow() }},
		mygo.Separator(),
		{Label: "播放 / 暂停", Click: func(*mygo.MenuItem, *mygo.Window) { a.togglePlayback() }},
		{Label: "上一首", Click: func(*mygo.MenuItem, *mygo.Window) { a.playerPrev() }},
		{Label: "下一首", Click: func(*mygo.MenuItem, *mygo.Window) { a.playerNext() }},
		mygo.Separator(),
		{Label: "退出 Melo", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})

	if runtime.GOOS == "linux" {
		// AppIndicator only exposes menus.
		tray.SetMenu(menu)
	} else {
		tray.OnClick(a.toggleWindow)
		tray.OnRightClick(func() {
			tray.SetMenu(menu)
			defer tray.SetMenu(nil)
			tray.PopUpMenu()
		})
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
