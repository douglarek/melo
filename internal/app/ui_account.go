package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

var (
	openWebLoginWindow = func(a *app) { a.openWebLogin() }
)

func (a *app) beginAuthAttempt() uint64 {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	a.authAttempt++
	return a.authAttempt
}

func (a *app) authAttemptIsCurrent(attempt uint64) bool {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return a.authAttempt == attempt
}

func (a *app) openLogin() {
	a.accountModalOpen = true
}

func (a *app) accountModalView(c *ui.Context) {
	p := a.pal

	// Backdrop modal
	backdrop := ui.Box(c.Key("account-backdrop")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.65)).AlignItems(ui.Center).Justify(ui.Center)
	if backdrop.Clicked() {
		a.accountModalOpen = false
	}

	backdrop.Children(func() {
		// Dialog Card
		dialog := ui.Column(c.Key("account-dialog")).Width(440).Radius(16).Background(p.sidebar).Border(1, p.hairline).Padding(24).Gap(20).Shadow(0, 16, 40, -8, ui.RGBA(0, 0, 0, 0.5))
		if dialog.Clicked() {
			// Stop backdrop click propagation
		}

		dialog.Children(func() {
			// Dialog Header
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				title := "登录 YouTube Music"
				if a.profile != nil {
					title = "我的账号"
				}
				ui.Text(c, title).FontSize(18).Bold().TextColor(p.text)
				ui.Spacer(c)
				closeBtn := ui.Box(c.Key("acc-close")).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if closeBtn.Hovered() {
					closeBtn.Background(p.hover)
				}
				if closeBtn.Clicked() {
					a.accountModalOpen = false
				}
				closeBtn.Children(func() {
					ui.Icon(c, iconClose).Size(16, 16).TextColor(p.muted)
				})
			})

			// Content based on whether user is logged in
			if a.profile != nil {
				// User Profile & Sign Out
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(14).Margin(10, 0).Children(func() {
					ui.Box(c).Size(72, 72).Radius(36).Background(p.card).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Icon(c, iconUser).Size(36, 36).TextColor(p.accent)
					})
					ui.Text(c, a.profile.Name).FontSize(18).Bold().TextColor(p.text)
					if a.profile.Handle != "" {
						ui.Text(c, a.profile.Handle).FontSize(13).TextColor(p.muted)
					}
					ui.Text(c, "已成功绑定 Google 账号").FontSize(12).TextColor(p.faint)

					signOutBtn := ui.Row(c.Key("sign-out-btn")).Height(38).Radius(19).Background(p.card).Padding(0, 24).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if signOutBtn.Hovered() {
						signOutBtn.Background(p.press)
					}
					if signOutBtn.Clicked() {
						a.signOut()
					}
					signOutBtn.Children(func() {
						ui.Text(c, "退出登录").FontSize(13).TextColor(p.accent)
					})
				})
			} else {
				a.renderWebLoginFlow(c)
			}
		})
	})
}

func (a *app) renderWebLoginFlow(c *ui.Context) {
	p := a.pal

	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(16).Padding(8, 0).Children(func() {
		ui.Text(c, "使用官方网页窗口直接登录 Google 账号").FontSize(13).Bold().TextColor(p.text).TextAlign(ui.Center)
		ui.Text(c, "点击下方按钮将打开 Google 官方登录窗口。\n登录完成后窗口会自动关闭，并同步您的专属推荐与歌单。").FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.4)

		if a.loginWin != nil {
			ui.Column(c).Height(120).AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "登录窗口已打开，请在窗口中登录 Google 账号...").FontSize(12.5).TextColor(p.text)
				})

				reopenBtn := ui.Row(c.Key("reopen-login-win")).Height(32).Radius(16).Background(p.card).Padding(0, 16).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if reopenBtn.Clicked() {
					a.loginWin.Focus()
				}
				reopenBtn.Children(func() {
					ui.Text(c, "置顶登录窗口").FontSize(12).TextColor(p.muted)
				})
			})
		} else {
			ui.Column(c).Height(120).AlignItems(ui.Center).Justify(ui.Center).Gap(14).Children(func() {
				loginBtn := ui.Row(c.Key("open-web-login-btn")).Height(44).Radius(22).Background(p.accent).Padding(0, 24).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
				if loginBtn.Hovered() {
					loginBtn.Opacity(0.9)
				}
				if loginBtn.Clicked() {
					openWebLoginWindow(a)
				}
				loginBtn.Children(func() {
					ui.Icon(c, iconExternal).Size(16, 16).TextColor(p.onAccent)
					ui.Text(c, "打开 Google 登录窗口").FontSize(14).Bold().TextColor(p.onAccent)
				})

				ui.Text(c, "无需寻找或复制任何复杂 Cookie，官方窗口登录全自动提取").FontSize(11).TextColor(p.faint)
			})
		}
	})
}

func (a *app) openWebLogin() {
	if a.loginWin != nil {
		a.loginWin.Focus()
		return
	}

	attempt := a.beginAuthAttempt()
	done := make(chan struct{})
	var doneOnce sync.Once
	stop := func() { doneOnce.Do(func() { close(done) }) }

	loginURL := "https://accounts.google.com/ServiceLogin?service=youtube&continue=https%3A%2F%2Fmusic.youtube.com%2F"
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"

	// Keep Google's login cookies in memory instead of the persistent
	// WebKit cookie database.
	loginSession := mygo.NewEphemeralSession()
	a.loginSession = loginSession

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "登录 YouTube Music",
		Width:     540,
		Height:    720,
		MinWidth:  400,
		MinHeight: 500,
		URL:       loginURL,
		Page:      mygo.PageOptions{Session: loginSession},
	})
	a.loginWin = win

	win.Page().SetUserAgent(ua)

	checkCookies := func() bool {
		select {
		case <-done:
			return true
		default:
		}
		if !a.authAttemptIsCurrent(attempt) {
			return true
		}
		cookies, err := loginSession.Cookies.List(mygo.CookieFilter{URL: "https://music.youtube.com"})
		if err != nil || len(cookies) == 0 {
			return false
		}
		hasSAPISID := false
		hasSession := false
		var parts []string
		for _, c := range cookies {
			parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
			if c.Name == "SAPISID" || c.Name == "__Secure-3PAPISID" {
				hasSAPISID = true
			}
			if c.Name == "SID" || c.Name == "LOGIN_INFO" || c.Name == "__Secure-3PSID" {
				hasSession = true
			}
		}

		if hasSAPISID && hasSession {
			cookieStr := strings.Join(parts, "; ")
			stop()
			a.post(func() {
				if !a.authAttemptIsCurrent(attempt) {
					return
				}
				if a.loginWin == win {
					a.loginWin.Close()
					a.loginWin = nil
				}
				if a.loginSession == loginSession {
					a.loginSession = nil
				}
				a.saveCookieSession(cookieStr, attempt)
			})
			return true
		}
		return false
	}

	win.Page().OnDidNavigate(func(navURL string) {
		if strings.Contains(navURL, "music.youtube.com") {
			time.Sleep(600 * time.Millisecond)
			checkCookies()
		}
	})

	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !a.authAttemptIsCurrent(attempt) {
					return
				}
				if checkCookies() {
					return
				}
			}
		}
	}()

	win.OnClose(func(e *mygo.CloseEvent) {
		stop()
		a.post(func() {
			if a.loginWin == win {
				a.loginWin = nil
			}
			if a.loginSession == loginSession {
				a.loginSession = nil
			}
		})
	})
}

func (a *app) saveCookieSession(cookie string, attempt uint64) {
	cookie = strings.TrimSpace(cookie)
	cookie = strings.TrimPrefix(cookie, "Cookie:")
	cookie = strings.TrimPrefix(cookie, "cookie:")
	cookie = strings.TrimSpace(cookie)

	a.authMu.Lock()
	if a.authAttempt != attempt {
		a.authMu.Unlock()
		return
	}
	ytmusic.DefaultClient.SetAuth(cookie)
	a.authMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prof, _ := ytmusic.DefaultClient.Account(ctx)
		if prof == nil {
			sapisid := ytmusic.ExtractSAPISID(cookie)
			if sapisid != "" {
				prof = &ytmusic.Profile{Name: "已登录 YouTube 用户"}
			} else {
				prof = &ytmusic.Profile{Name: "已登录用户"}
			}
		}

		sess := &ytmusic.SessionData{
			Cookie:  cookie,
			Profile: prof,
		}
		a.authMu.Lock()
		if a.authAttempt != attempt {
			a.authMu.Unlock()
			return
		}
		err := ytmusic.SaveSession(sess)
		a.authMu.Unlock()
		if err != nil {
			a.post(func() {
				if a.authAttemptIsCurrent(attempt) {
					a.authErr = "登录信息保存失败，请检查用户数据目录后重试"
				}
			})
			return
		}

		a.post(func() {
			if !a.authAttemptIsCurrent(attempt) {
				return
			}
			a.profile = prof
			a.accountModalOpen = false
			a.clearDefaultBrowserData()

			// Invalidate cached unauthenticated sections so Home, Explore, Charts and all pages reload with user account
			a.homeSections = nil
			a.homeLoading = false
			a.exploreSections = nil
			a.exploreLoading = false
			a.chartsSections = nil
			a.chartsLoading = false
			a.searches = map[route]*ytmusic.SearchResult{}

			a.loadCurrentRoute()
			a.loadLibrary()
		})
	}()
}

func (a *app) signOut() {
	a.authMu.Lock()
	a.authAttempt++
	err := ytmusic.ClearSession()
	a.authMu.Unlock()
	if err != nil {
		a.authErr = "退出登录未完全清除本地凭据，请重试"
	}

	a.clearDefaultBrowserData()
	if a.loginWin != nil {
		a.loginWin.Close()
		a.loginWin = nil
	}
	a.loginSession = nil
	a.profile = nil
	a.accountModalOpen = false
	a.settingsModalOpen = false

	// Clear authenticated library & personal data
	a.libraryPlaylists = nil
	a.liked = nil
	delete(a.playlists, "LM")
	delete(a.playlists, "VLLM")
	a.deleteLibraryCache()

	// Reset cached sections requested with user session
	a.homeSections = nil
	a.homeLoading = false
	a.exploreSections = nil
	a.exploreLoading = false
	a.chartsSections = nil
	a.chartsLoading = false
	a.searches = map[route]*ytmusic.SearchResult{}

	// Return to clean home view and reload public data
	a.route = route{page: pageHome}
	a.history = nil
	a.forwardHistory = nil
	a.loadCurrentRoute()
}
