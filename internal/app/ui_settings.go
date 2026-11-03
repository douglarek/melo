package app

import (
	"github.com/egoist/mygo/ui"
)

func (a *app) settingsModalView(c *ui.Context) {
	p := a.pal

	backdrop := ui.Box(c.Key("settings-backdrop")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.65)).AlignItems(ui.Center).Justify(ui.Center)
	if backdrop.Clicked() {
		a.settingsModalOpen = false
	}

	backdrop.Children(func() {
		dialog := ui.Column(c.Key("settings-dialog")).Width(460).Radius(16).Background(p.sidebar).Border(1, p.hairline).Padding(24).Gap(24).Shadow(0, 16, 40, -8, ui.RGBA(0, 0, 0, 0.5))
		if dialog.Clicked() {
			// Prevent event propagation to backdrop
		}

		dialog.Children(func() {
			// Header
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "设置").FontSize(18).Bold().TextColor(p.text)
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
							ui.Text(c, "退出登录").FontSize(12).TextColor(p.accent)
						})
					})
				})
			} else {
				ui.Column(c).FillWidth().Background(p.card).Radius(12).Padding(16).Gap(10).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).Gap(3).Children(func() {
							ui.Text(c, "Google 账号").FontSize(15).Bold().TextColor(p.text)
							ui.Text(c, "登录以同步您的云端播放列表与喜爱的音乐").FontSize(12).TextColor(p.muted)
						})
						loginBtn := ui.Row(c.Key("set-login-btn")).Height(32).Radius(16).Background(p.accent).Padding(0, 16).AlignItems(ui.Center).Cursor(ui.CursorPointer)
						if loginBtn.Clicked() {
							a.settingsModalOpen = false
							a.openLogin()
						}
						loginBtn.Children(func() {
							ui.Text(c, "登录账号").FontSize(12.5).Bold().TextColor(p.onAccent)
						})
					})
				})
			}

			// 1. Appearance / Theme
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				ui.Text(c, "外观主题").FontSize(14).Bold().TextColor(p.text)

				themes := []struct {
					key   string
					label string
				}{
					{"system", "跟随系统"},
					{"dark", "深色外观"},
					{"light", "浅色外观"},
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

			// 2. Audio quality
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, "音质设置").FontSize(14).Bold().TextColor(p.text)
				ui.Text(c, "AAC 128 kbps 自适应流（边下边播），不可用时回退 96 kbps").FontSize(12).TextColor(p.muted)
			})

			// 3. Cache
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "本地缓存").FontSize(14).Bold().TextColor(p.text)
					ui.Spacer(c)
					ui.Text(c, "当前 "+formatBytes(a.pl.Cache.Size())).FontSize(11).TextColor(p.muted)
				})
				ui.Text(c, "已缓存歌曲将在再次播放时免网络秒开").FontSize(12).TextColor(p.muted)

				cacheOptions := []struct {
					mb    int64
					label string
				}{
					{0, "关闭"},
					{1024, "1G"},
					{2048, "2G"},
					{4096, "4G"},
					{-1, "无限"},
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
					ui.Text(c, "超出上限时自动清理最久未播放的缓存").FontSize(11).TextColor(p.muted)
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
						ui.Text(c, "清理缓存").FontSize(12).TextColor(p.text)
					})
				})
			})

			// 4. Close behavior
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				ui.Text(c, "关闭窗口时").FontSize(14).Bold().TextColor(p.text)
				ui.Text(c, "保持在菜单栏时，关闭窗口后音乐继续播放，可从托盘图标重新打开。").FontSize(12).TextColor(p.muted)

				options := []struct {
					key   string
					label string
					tray  bool
				}{
					{"exit", "退出应用", false},
					{"tray", "保持在菜单栏", true},
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
					ui.Text(c, "当前系统托盘不可用，关闭窗口仍会退出应用。").FontSize(11).TextColor(p.muted)
				}
			})

			// 5. Track notifications
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, "切歌通知").FontSize(14).Bold().TextColor(p.text)
				cb := ui.Checkbox(c, &a.settings.TrackNotifications, "切歌时显示桌面通知")
				if cb.Changed() {
					a.settings.save()
				}
				ui.Text(c, "仅当窗口失去焦点时通知；应用在前台时不打扰。").FontSize(11).TextColor(p.muted)
			})

			// 6. About
			ui.Column(c).FillWidth().Background(p.card).Radius(10).Padding(12).Gap(4).Children(func() {
				ui.Text(c, "Melo").FontSize(13).Bold().TextColor(p.text)
				ui.Text(c, "版本 0.1.0 · 第三方 YouTube Music 客户端").FontSize(11).TextColor(p.muted)
			})
		})
	})
}
