package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"
)

func (a *app) settingsModalView(c *ui.Context) {
	p := a.pal

	backdrop := ui.Box(c.Key("settings-backdrop")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.65)).AlignItems(ui.Center).Justify(ui.Center)
	if backdrop.Clicked() {
		a.settingsModalOpen = false
	}

	backdrop.Children(func() {
		dialog := ui.Column(c.Key("settings-dialog")).Width(460).MaxHeightPercent(94).Radius(16).Background(p.sidebar).Border(1, p.hairline).Padding(20).Gap(18).Shadow(0, 16, 40, -8, ui.RGBA(0, 0, 0, 0.5))
		if dialog.Clicked() {
			// Prevent event propagation to backdrop
		}

		dialog.Children(func() {
			// Header
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.Settings()).FontSize(18).Bold().TextColor(p.text)
				ui.Spacer(c)
				closeBtn := ui.Box(c.Key("set-close")).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if closeBtn.Hovered() {
					closeBtn.Background(p.hover)
				}
				if closeBtn.Clicked() {
					a.settingsModalOpen = false
				}
				closeBtn.Children(func() {
					ui.Icon(c, iconClose).Size(16, 16).TextColor(p.muted)
				})
			})

			// The sections scroll when they do not fit the window, as at
			// the smallest window size.
			ui.Scroll(c.Key("settings-scroll")).Vertical().FillWidth().Grow(1).TrackScroll(&a.settingsScroll).Children(func() {
				// The right inset keeps the scroll bar clear of the sections.
				ui.Column(c).FillWidth().Gap(14).Padding(0, 10, 0, 0).Children(func() {

					// 0. Account Info Section
					if a.profile != nil {
						ui.Column(c).FillWidth().Background(p.card).Radius(12).Padding(16).Gap(10).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
								if a.profile.Avatar != "" {
									a.coverImage(c, a.profile.Avatar, 44, 22)
								} else {
									ui.Box(c).Size(44, 44).Radius(22).Background(p.sidebar).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
										ui.Icon(c, iconUser).Size(22, 22).TextColor(p.accent)
									})
								}
								ui.Column(c).Grow(1).Gap(3).Children(func() {
									ui.Text(c, a.profile.Name).FontSize(16).Bold().TextColor(p.text)
									if a.profile.Handle != "" {
										ui.Text(c, a.profile.Handle).FontSize(12).TextColor(p.muted)
									}
								})
								signOutBtn := ui.Row(c.Key("set-sign-out")).Height(30).Radius(15).Background(p.sidebar).Padding(0, 14).AlignItems(ui.Center).Cursor(ui.CursorPointer)
								if signOutBtn.Hovered() {
									signOutBtn.Background(p.press)
								}
								if signOutBtn.Clicked() {
									a.signOut()
								}
								signOutBtn.Children(func() {
									ui.Text(c, locales.SignOut()).FontSize(12).TextColor(p.accent)
								})
							})
						})
					} else {
						ui.Column(c).FillWidth().Background(p.card).Radius(12).Padding(16).Gap(10).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Column(c).Grow(1).Gap(3).Children(func() {
									ui.Text(c, locales.GoogleAccount()).FontSize(15).Bold().TextColor(p.text)
									ui.Text(c, locales.SignInHint()).FontSize(12).TextColor(p.muted)
								})
								loginBtn := ui.Row(c.Key("set-login-btn")).Height(32).Radius(16).Background(p.accent).Padding(0, 16).AlignItems(ui.Center).Cursor(ui.CursorPointer)
								if loginBtn.Clicked() {
									a.settingsModalOpen = false
									a.openLogin()
								}
								loginBtn.Children(func() {
									ui.Text(c, locales.SignInButton()).FontSize(12.5).Bold().TextColor(p.onAccent)
								})
							})
						})
					}

					// 1. Appearance / Theme
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, locales.Appearance()).FontSize(14).Bold().TextColor(p.text)

						themes := []struct {
							key   string
							label string
						}{
							{"system", locales.LanguageSystem()},
							{"dark", locales.Dark()},
							{"light", locales.Light()},
						}

						ui.Row(c).FillWidth().Gap(8).Children(func() {
							for _, item := range themes {
								t := item
								active := a.settings.Appearance == t.key
								bg := p.card
								txtCol := p.muted
								if active {
									bg = p.accent
									txtCol = p.onAccent
								}

								btn := ui.Box(c.Key("theme-" + t.key)).Grow(1).Height(36).Radius(8).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
								if btn.Clicked() {
									a.settings.Appearance = t.key
									a.settings.applyAppearance()
									a.settings.save()
								}
								btn.Children(func() {
									txt := ui.Text(c, t.label).FontSize(13).TextColor(txtCol)
									if active {
										txt.Bold()
									}
								})
							}
						})
					})

					// 2. Language
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, locales.Language()).FontSize(14).Bold().TextColor(p.text)

						// Each language is named in itself, as a language menu does.
						languages := []struct {
							tag   string
							label string
						}{
							{"", locales.LanguageSystem()},
							{"zh-Hans", "简体中文"},
							{"en", "English"},
						}

						ui.Row(c).FillWidth().Gap(8).Children(func() {
							for _, language := range languages {
								language := language
								active := a.settings.Language == language.tag
								bg := p.card
								txtCol := p.muted
								if active {
									bg = p.accent
									txtCol = p.onAccent
								}

								btn := ui.Box(c.Key("language-" + language.tag)).Grow(1).Height(36).Radius(8).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
								if btn.Clicked() {
									a.setLanguage(language.tag)
								}
								btn.Children(func() {
									txt := ui.Text(c, language.label).FontSize(13).TextColor(txtCol)
									if active {
										txt.Bold()
									}
								})
							}
						})
					})

					// 3. Audio quality
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, locales.AudioQuality()).FontSize(14).Bold().TextColor(p.text)
						ui.Text(c, locales.AudioQualityHint()).FontSize(12).TextColor(p.muted)
					})

					// 4. Cache
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							ui.Text(c, locales.Cache()).FontSize(14).Bold().TextColor(p.text)
							ui.Spacer(c)
							ui.Text(c, locales.CacheInUsePrefix()+formatBytes(a.pl.Cache.Size())).FontSize(11).TextColor(p.muted)
						})
						ui.Text(c, locales.CacheHint()).FontSize(12).TextColor(p.muted)

						cacheOptions := []struct {
							mb    int64
							label string
						}{
							{0, locales.Off()},
							{1024, "1G"},
							{2048, "2G"},
							{4096, "4G"},
							{-1, locales.Unlimited()},
						}
						ui.Row(c).FillWidth().Gap(6).Children(func() {
							for _, opt := range cacheOptions {
								opt := opt
								active := a.settings.CacheMB == opt.mb
								bg := p.card
								txtCol := p.muted
								if active {
									bg = p.accent
									txtCol = p.onAccent
								}
								btn := ui.Box(c.Key("cache-" + opt.label)).Grow(1).Height(32).Radius(8).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
								if btn.Clicked() {
									a.settings.CacheMB = opt.mb
									a.settings.save()
									a.applyCacheLimit()
								}
								btn.Children(func() {
									txt := ui.Text(c, opt.label).FontSize(12).TextColor(txtCol)
									if active {
										txt.Bold()
									}
								})
							}
						})

						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							ui.Text(c, locales.CacheLimitHint()).FontSize(11).TextColor(p.muted)
							ui.Spacer(c)
							clearBtn := ui.Row(c.Key("clear-cache-btn")).Height(30).Radius(15).Background(p.card).Padding(0, 12).AlignItems(ui.Center).Cursor(ui.CursorPointer)
							if clearBtn.Clicked() {
								if a.pl != nil && a.pl.Cache != nil {
									a.pl.Cache.Clear()
								}
								if a.art != nil {
									a.art.Clear()
								}
								a.releaseMemoryNow()
							}
							clearBtn.Children(func() {
								ui.Text(c, locales.ClearCache()).FontSize(12).TextColor(p.text)
							})
						})
					})

					// 5. Close behavior
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, locales.WhenTheWindowCloses()).FontSize(14).Bold().TextColor(p.text)
						ui.Text(c, locales.CloseToTrayHint()).FontSize(12).TextColor(p.muted)

						options := []struct {
							key   string
							label string
							tray  bool
						}{
							{"exit", locales.QuitTheApp(), false},
							{"tray", locales.KeepInTheMenuBar(), true},
						}
						ui.Row(c).FillWidth().Gap(8).Children(func() {
							for _, opt := range options {
								opt := opt
								active := a.settings.CloseToTray == opt.tray
								bg := p.card
								txtCol := p.muted
								if active {
									bg = p.accent
									txtCol = p.onAccent
								}
								btn := ui.Box(c.Key("close-" + opt.key)).Grow(1).Height(36).Radius(8).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
								if btn.Clicked() {
									a.settings.CloseToTray = opt.tray
									a.settings.save()
								}
								btn.Children(func() {
									txt := ui.Text(c, opt.label).FontSize(13).TextColor(txtCol)
									if active {
										txt.Bold()
									}
								})
							}
						})
						if a.settings.CloseToTray && a.trayErr != nil {
							ui.Text(c, locales.TrayUnavailableHint()).FontSize(11).TextColor(p.muted)
						}
					})

					// 6. Track notifications
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						ui.Text(c, locales.SongNotifications()).FontSize(14).Bold().TextColor(p.text)
						cb := ui.Checkbox(c, &a.settings.TrackNotifications, locales.NotificationsToggle())
						if cb.Changed() {
							a.settings.save()
						}
						ui.Text(c, locales.NotificationsHint()).FontSize(11).TextColor(p.muted)
					})

					// 7. Software updates: none for a development build,
					// which cannot install one, and whose reader builds it.
					if !a.update.dev {
						ui.Column(c).FillWidth().Gap(8).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Text(c, locales.SoftwareUpdate()).FontSize(14).Bold().TextColor(p.text)
								ui.Spacer(c)
								ui.Text(c, a.versionLabel()).FontSize(11).TextColor(p.muted)
							})

							if a.update.supported {
								cb := ui.Checkbox(c, &a.settings.CheckUpdates, locales.AutoChecksToggle())
								if cb.Changed() {
									a.setUpdateChecks(a.settings.CheckUpdates)
								}
								ui.Text(c, locales.AutoChecksHint()).FontSize(11).TextColor(p.muted)
							} else {
								// 由 deb / rpm 等软件包管理器安装的版本，
								// 不能替换自身。
								ui.Text(c, locales.UpdatesUnavailableHint()).FontSize(11).TextColor(p.muted)
							}

							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Text(c, a.updateStatusLine()).FontSize(11).TextColor(p.muted)
								ui.Spacer(c)
								checkBtn := ui.Row(c.Key("check-updates")).Height(30).Radius(15).Background(p.card).Padding(0, 14).AlignItems(ui.Center).Cursor(ui.CursorPointer)
								if checkBtn.Hovered() {
									checkBtn.Background(p.hover)
								}
								if checkBtn.Clicked() {
									a.checkUpdates()
								}
								checkBtn.Children(func() {
									ui.Text(c, locales.CheckForUpdates()).FontSize(12).TextColor(p.accent)
								})
							})
						})
					}

					// 8. About
					ui.Column(c).FillWidth().Background(p.card).Radius(10).Padding(12).Gap(4).Children(func() {
						ui.Text(c, "Melo").FontSize(13).Bold().TextColor(p.text)
						ui.Text(c, a.versionLabel()+locales.ThirdPartyClientSuffix()).FontSize(11).TextColor(p.muted)
					})
				})
			})
		})
	})
}
