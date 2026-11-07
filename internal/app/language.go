package app

import (
	"github.com/egoist/mygo"
)

// Melo is written in Chinese and English (see package locales): mygo
// matches the user's preferred languages against the catalogs, and the
// settings choose one of them instead (SetLocale).
//
// Replaced by tests.
var setAppLocale = mygo.App.SetLocale

// applyLanguage shows the language of the settings, "" being the system's,
// and says the menus and the tray again when it changes, since their labels
// are built once.
func (a *app) applyLanguage() {
	setAppLocale(a.settings.Language)
	mygo.App.OnLocaleChanged(func(string) {
		a.setMenu()
		a.refreshTray()
	})
}

// setLanguage makes Melo show the language of tag, "" to follow the user's
// languages, and keeps the choice for the next launches.
func (a *app) setLanguage(tag string) {
	a.settings.Language = tag
	a.settings.save()
	setAppLocale(tag)
}
