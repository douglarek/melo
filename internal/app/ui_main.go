package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"
)

const sidebarWidth = 220
const playerHeight = 72
const headerHeight = 52
const navRowHeight = 38

// sidebarTopInset keeps the first sidebar row level with the header
// controls. The room the window controls take wins where they sit over the
// sidebar, as the macOS traffic lights do.
func sidebarTopInset(bar ui.TitleBar) float32 {
	if bar.Left > 0 {
		return max(bar.Height, headerHeight)
	}
	return (headerHeight - navRowHeight) / 2
}

func (a *app) view(c *ui.Context) {
	a.syncNowPlayingWindowButtons()
	a.syncMediaArtwork()
	t := *c.Theme()
	a.pal = paletteFor(&t)
	p := a.pal
	a.artPins = map[string]bool{}
	if a.art != nil {
		defer a.art.setPinned(a.artPins)
	}

	t.Accent = p.accent
	t.AccentText = p.onAccent
	c.SetTheme(&t)

	// Global shortcut: Space toggles play if search box not focused
	if !c.Focused(a.searchBox) {
		c.OnShortcut(0, ui.KeySpace, a.pl.TogglePlay)
		c.OnShortcut(0, ui.KeyBack, a.back)
		c.OnShortcut(0, ui.KeyForward, a.forward)
	}

	root := ui.Box(c).Fill().Background(p.bg).Selectable()
	root.Children(func() {
		// Main 2-column layout (Sidebar + Content)
		ui.Row(c).Fill().Children(func() {
			// Sidebar (goes all the way to bottom with zero bottom padding)
			topInset := sidebarTopInset(c.TitleBar())
			side := ui.Column(c).Width(sidebarWidth).FillHeight().Background(p.sidebar).BorderWidth(0, 1, 0, 0).BorderColor(p.hairline).Padding(topInset, 0, 0, 0)
			side.Children(func() {
				a.sidebar(c)
			})

			// Content pane (Right)
			ui.Box(c.Key("main")).Grow(1).FillHeight().Background(p.bg).Children(func() {
				ui.Column(c).Fill().Children(func() {
					// Top header bar
					a.header(c)

					// Page body scroll area
					ui.Box(c).Grow(1).FillWidth().Children(func() {
						if a.pageUsesTrackList() {
							ui.Box(c).Grow(1).FillWidth().Padding(24, 32, playerHeight+24, 32).Children(func() {
								a.content(c)
							})
							return
						}
						ui.Scroll(c).Vertical().Fill().TrackScroll(&a.contentScroll).Children(func() {
							ui.Column(c).FillWidth().Padding(24, 32, playerHeight+24, 32).Children(func() {
								a.content(c)
							})
						})
					})
				})

				// Docked bottom player bar (ONLY along the foot of the main pane)
				ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(playerHeight).Background(p.sidebar).BorderWidth(1, 0, 0, 0).BorderColor(p.hairline).Children(func() {
					a.playerBar(c)
				})
			})
		})

		// Modal overlays
		a.nowPlayingLayer(c)
		if a.accountModalOpen {
			a.accountModalView(c)
		}
		if a.settingsModalOpen {
			a.settingsModalView(c)
		}
	})

	if a.authErr != "" {
		c.Toast(a.authErr)
		a.authErr = ""
	}
	if a.playbackErr != "" {
		c.Toast(a.playbackErr)
		a.playbackErr = ""
	}
}

func (a *app) sidebar(c *ui.Context) {
	p := a.pal

	// Draggable area above the navigation rows. It is a bare drag region:
	// Clicked, Pressed or DoubleClicked would make it take the pointer, and
	// mygo moves the window only for a press that a drag region alone takes.
	// A double click on it zooms the window, as mygo does for every drag
	// region.
	ui.Box(c.Key("sidebar-top-drag")).Absolute().Top(0).Left(0).Right(0).Height(sidebarTopInset(c.TitleBar())).DragWindow()

	// Middle scrollable area for navigation & playlists
	ui.Scroll(c.Key("sidebar-scroll")).Vertical().Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			navItem := func(label string, icon *ui.SVG, page pageKind) {
				active := a.route.page == page && a.route.query == ""
				bg := ui.Transparent
				col := p.muted
				if active {
					bg = p.hover
					col = p.text
				}
				b := ui.Row(c.Key(label)).FillWidth().Height(navRowHeight).Radius(8).Background(bg).AlignItems(ui.Center).Padding(0, 12).Gap(12).Cursor(ui.CursorPointer)
				if b.Hovered() && !active {
					b.Background(p.hover)
				}
				if b.Clicked() {
					a.open(route{page: page})
				}
				b.Children(func() {
					ui.Icon(c, icon).Size(18, 18).TextColor(col)
					t := ui.Text(c, label).FontSize(13.5).TextColor(col)
					if active {
						t.Bold().TextColor(p.text)
					}
				})
			}

			navItem(locales.Home(), iconHome, pageHome)
			navItem(locales.Explore(), iconExplore, pageExplore)
			navItem(locales.Library(), iconLibrary, pageLibrary)

			// User's playlists
			if len(a.libraryPlaylists) > 0 {
				ui.Box(c).Height(10)
				ui.Text(c, locales.MyPlaylists()).FontSize(11.5).Bold().TextColor(p.faint).Margin(0, 12, 6, 12)
				for _, pl := range a.libraryPlaylists {
					item := pl
					active := a.route.page == pagePlaylist && a.route.id == item.ID
					bg := ui.Transparent
					col := p.muted
					if active {
						bg = p.hover
						col = p.text
					}
					b := ui.Row(c.Key("side-pl-"+item.ID)).FillWidth().Height(34).Radius(6).Background(bg).AlignItems(ui.Center).Padding(0, 10).Gap(8).Cursor(ui.CursorPointer).ContextMenu(a.playlistCopyMenu(c, item.Title))
					if b.Hovered() && !active {
						b.Background(p.hover)
					}
					if b.Clicked() {
						a.open(route{page: pagePlaylist, id: item.ID})
					}
					b.Children(func() {
						ui.Icon(c, iconQueue).Size(15, 15).TextColor(col)
						t := ui.Text(c, item.Title).FontSize(12.5).TextColor(col).SingleLine().Tooltip(item.Title)
						if active {
							t.Bold().TextColor(p.text)
						}
					})
				}
			} else if a.profile != nil && a.libraryLoading {
				ui.Row(c).Padding(6, 12).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, locales.SyncingPlaylists()).FontSize(11).TextColor(p.muted)
				})
			}
		})
	})

	// Foot of sidebar: clean, continuous sidebar without dividing line
	ui.Column(c.Key("sidebar-foot")).FillWidth().Padding(0, 10, 6, 10).Gap(6).Justify(ui.End).Children(func() {
		if a.settings.UpdateAvailable != "" {
			a.updateBadge(c)
		}
		userRow := ui.Row(c.Key("sidebar-user-row")).FillWidth().Height(44).Radius(8).Padding(0, 8).AlignItems(ui.Center).Gap(10).Cursor(ui.CursorPointer)
		if userRow.Hovered() {
			userRow.Background(p.hover)
		}

		if a.profile == nil {
			userRow.Label(locales.SignIn()).Children(func() {
				ui.Box(c).Size(28, 28).Radius(14).Background(p.card).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, iconUser).Size(16, 16).TextColor(p.muted)
				})
				ui.Text(c, locales.SignInWithGoogle()).FontSize(13).TextColor(p.muted)
			})
			if userRow.Clicked() {
				a.openLogin()
			}
		} else {
			userRow.Label(a.profile.Name).Children(func() {
				if a.profile.Avatar != "" {
					a.coverImage(c, a.profile.Avatar, 28, 14)
				} else {
					ui.Box(c).Size(28, 28).Radius(14).Background(p.card).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Icon(c, iconUser).Size(16, 16).TextColor(p.accent)
					})
				}
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, a.profile.Name).FontSize(13).Bold().TextColor(p.text).SingleLine().Tooltip(a.profile.Name)
					if a.profile.Handle != "" {
						ui.Text(c, a.profile.Handle).FontSize(11).TextColor(p.muted).SingleLine().Tooltip(a.profile.Handle)
					}
				})
			})
			if userRow.Clicked() {
				a.openSettings()
			}
		}
	})
}

// updateBadge points at the version a background check found. Clicking it
// opens the update window, which checks again and offers to install it.
func (a *app) updateBadge(c *ui.Context) {
	p := a.pal
	badge := ui.Row(c.Key("sidebar-update")).FillWidth().Height(28).Radius(8).Background(p.card).Padding(0, 10).AlignItems(ui.Center).Gap(7).Cursor(ui.CursorPointer).Tooltip(locales.UpdateBadgeTooltip())
	if badge.Hovered() {
		badge.Background(p.press)
	}
	if badge.Clicked() {
		a.checkUpdates()
	}
	badge.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(p.accent)
		ui.Text(c, locales.UpdateBadgePrefix()+a.settings.UpdateAvailable).FontSize(12).TextColor(p.accent).SingleLine()
	})
}

func (a *app) header(c *ui.Context) {
	p := a.pal

	// A bare drag region: a click handler here would take the pointer and
	// the window would never move (see the sidebar's drag region).
	headerBar := ui.Row(c.Key("header-bar")).FillWidth().Height(headerHeight).Padding(0, 24).AlignItems(ui.Center).Gap(12).DragWindow()
	headerBar.Children(func() {
		// History Back & Forward
		backBtn := ui.Box(c.Key("nav-back")).Size(32, 32).Radius(16).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
		if len(a.history) == 0 {
			backBtn.Opacity(0.3)
		} else {
			if backBtn.Hovered() {
				backBtn.Background(p.hover)
			}
			if backBtn.Clicked() {
				a.back()
			}
		}
		backBtn.Children(func() {
			ui.Icon(c, iconBack).Size(18, 18).TextColor(p.text)
		})

		fwdBtn := ui.Box(c.Key("nav-forward")).Size(32, 32).Radius(16).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
		if len(a.forwardHistory) == 0 {
			fwdBtn.Opacity(0.3)
		} else {
			if fwdBtn.Hovered() {
				fwdBtn.Background(p.hover)
			}
			if fwdBtn.Clicked() {
				a.forward()
			}
		}
		fwdBtn.Children(func() {
			ui.Icon(c, iconForward).Size(18, 18).TextColor(p.text)
		})

		// Search bar input
		searchWrap := ui.Row(c.Key("search-bar")).Width(360).Height(36).Radius(18).Background(p.card).Padding(0, 14).AlignItems(ui.Center).Gap(8)
		searchWrap.Children(func() {
			ui.Icon(c, iconSearch).Size(16, 16).TextColor(p.muted)
			input := ui.TextInput(c, &a.searchQuery).Bind(&a.searchBox).Grow(1).Placeholder(locales.SearchPlaceholder()).Background(ui.Transparent)
			input.BorderWidth(0).TextColor(p.text).FontSize(13)
			if input.Submitted() {
				a.search(a.searchQuery)
			}
		})

		ui.Spacer(c)
	})
}

func (a *app) content(c *ui.Context) {
	switch a.route.page {
	case pageHome:
		a.viewHome(c)
	case pageExplore:
		a.viewExplore(c)
	case pageLibrary:
		a.viewLibrary(c)
	case pageCharts:
		a.viewCharts(c)
	case pageSearch:
		a.viewSearch(c)
	case pagePlaylist:
		a.viewPlaylist(c)
	case pageAlbum:
		a.viewAlbum(c)
	case pageArtist:
		a.viewArtist(c)
	case pageArtistSongs:
		a.viewArtistSongs(c)
	case pageLiked:
		a.viewLiked(c)
	case pageCategory:
		a.viewCategory(c)
	case pageSection:
		a.viewSection(c)
	default:
		a.viewHome(c)
	}
}
