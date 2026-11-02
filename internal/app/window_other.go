//go:build !darwin

package app

func (a *app) toggleMaximize() {
	if a.win == nil {
		return
	}
	a.win.ToggleMaximize()
}

func (a *app) minimize() {
	if a.win == nil {
		return
	}
	a.win.Minimize()
}
