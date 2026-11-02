//go:build darwin

package app

import "github.com/ebitengine/purego/objc"

func (a *app) toggleMaximize() {
	if a.win == nil {
		return
	}
	handle := a.win.NativeHandle()
	if handle != 0 {
		sel := objc.RegisterName("performZoom:")
		objc.ID(handle).Send(sel, 0)
		return
	}
	a.win.ToggleMaximize()
}

func (a *app) minimize() {
	if a.win == nil {
		return
	}
	handle := a.win.NativeHandle()
	if handle != 0 {
		sel := objc.RegisterName("performMiniaturize:")
		objc.ID(handle).Send(sel, 0)
		return
	}
	a.win.Minimize()
}
