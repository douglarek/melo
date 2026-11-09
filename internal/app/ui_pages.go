package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) viewHome(c *ui.Context) {
	p := a.pal

	if a.homeLoading && len(a.homeSections) == 0 {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingRecommendations()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(32).Children(func() {
		for i, sec := range a.homeSections {
			a.renderSection(c, fmt.Sprintf("home-sec-%d", i), sec)
		}
	})
}

func (a *app) viewExplore(c *ui.Context) {
	p := a.pal

	if a.exploreLoading && len(a.exploreSections) == 0 {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.ExploringNewMusic()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(32).Children(func() {
		for i, sec := range a.exploreSections {
			a.renderSection(c, fmt.Sprintf("exp-sec-%d", i), sec)
		}
	})
}

func (a *app) viewCategory(c *ui.Context) {
	p := a.pal
	cat := a.categories[categoryKey(a.route.id, a.route.params)]
	if cat == nil {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingCategories()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(28).Children(func() {
		ui.Text(c, cat.Title).FontSize(28).Bold().TextColor(p.text)
		// Only near-viewport sections fetch artwork. A podcast page can hold
		// sixteen shelves, and loading every cover at once overwhelms the
		// art cache and redraws the window for each image.
		const sectionStride = 300
		first := max(0, int(a.contentScroll.Y/sectionStride)-1)
		for i, sec := range cat.Sections {
			a.renderSectionWithArt(c, fmt.Sprintf("cat-%s-sec-%d", categoryKey(a.route.id, a.route.params), i), sec, i >= first && i <= first+4)
		}
	})
}

func (a *app) viewCharts(c *ui.Context) {
	p := a.pal

	if a.chartsLoading && len(a.chartsSections) == 0 {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingTheCharts()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(28).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Text(c, locales.Charts()).FontSize(28).Bold().TextColor(p.text)
			ui.Spacer(c)
			if len(a.chartCountries) > 0 {
				a.chartCountrySelector(c)
			}
		})
		for i, sec := range a.chartsSections {
			a.renderSection(c, fmt.Sprintf("chart-sec-%d", i), sec)
		}
	})
}

func (a *app) chartCountrySelector(c *ui.Context) {
	p := a.pal
	selectedName := locales.ChooseACountry()
	for _, country := range a.chartCountries {
		if country.Code == a.chartCountry {
			selectedName = country.Name
			break
		}
	}

	sel := ui.SelectBase(c.Key("chart-country"), &a.chartCountry)
	sel.Trigger.Label(locales.ChooseACountry()).Height(34).Radius(17).Background(p.card).Padding(0, 14).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
	sel.Trigger.Children(func() {
		ui.Text(c, selectedName).FontSize(13).TextColor(p.text)
		ui.Icon(c, iconChevron).Size(12, 12).TextColor(p.muted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Width(260).Padding(6).Background(p.card).Radius(10).BorderWidth(1, 1, 1, 1).BorderColor(p.hairline)
		ui.Scroll(c).Vertical().MaxHeight(400).Children(func() {
			for _, country := range a.chartCountries {
				item := sel.Item(country.Code).Height(32).Radius(6).Padding(0, 10).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
				if item.Highlighted() {
					item.Background(p.hover)
				}
				item.Children(func() {
					ui.Box(c).Size(16, 16).Children(func() {
						if country.Code == a.chartCountry {
							ui.Icon(c, iconCheck).Size(14, 14).TextColor(p.accent)
						}
					})
					ui.Text(c, country.Name).FontSize(13).TextColor(p.text)
				})
			}
		})
	})
	if sel.Trigger.Changed() {
		a.loadChartsCountry(c)
	}
}

func (a *app) loadChartsCountry(c *ui.Context) {
	if a.chartCountry == "" {
		return
	}
	code := a.chartCountry
	a.chartsGen++
	gen := a.chartsGen
	a.chartsLoading = true
	c.Invalidate()
	go func() {
		page, _ := ytmusic.DefaultClient.BrowseChartsPage(context.Background(), code)
		a.post(func() {
			if gen != a.chartsGen {
				return
			}
			a.chartsLoading = false
			if page == nil {
				return
			}
			a.chartsSections = page.Sections
			a.chartCountries = page.Countries
			a.chartCountry = page.Selected
		})
	}()
}

func (a *app) renderSection(c *ui.Context, key string, sec ytmusic.Section) {
	a.renderSectionWithArt(c, key, sec, true)
}

func (a *app) renderSectionWithArt(c *ui.Context, key string, sec ytmusic.Section, loadArt bool) {
	ui.Column(c.Key(key)).FillWidth().Gap(14).Children(func() {
		if sec.Layout == "moods" {
			a.renderMoodSection(c, key, sec)
			return
		}
		if sec.Layout == "shortcuts" {
			a.renderShortcuts(c, key, sec)
			return
		}

		state := a.sectionScroll(key)
		step := float32(6 * (150 + 16))
		prevOn := state.X > 0.5
		nextOn := state.X < state.MaxX-0.5 || (state.MaxX == 0 && len(sec.Items) > 5)
		if sec.Title != "" {
			a.sectionHeader(c, key, sec.Title, prevOn, nextOn,
				func() {
					state.ScrollTo(max(0, state.X-step), 0, ui.Smooth)
				},
				func() {
					target := state.X + step
					if state.MaxX > 0 && target > state.MaxX {
						target = state.MaxX
					}
					state.ScrollTo(target, 0, ui.Smooth)
				},
				func() {
					a.openMore(key, sec)
				},
			)
		}

		items := sec.Items
		if len(items) > 40 {
			items = items[:40]
		}
		shelf := ui.ScrollHorizontal(c).TrackScroll(state).FillWidth()
		shelf.Children(func() {
			ui.Row(c).Gap(16).Padding(0, 0, 10, 0).AlignItems(ui.Start).Children(func() {
				// Load covers for the visible cards and a small buffer. Loading
				// every cover in a long shelf floods the art cache and forces a
				// full redraw for each image.
				first := max(0, int(state.X/(150+16))-1)
				last := first + 8
				for j, item := range items {
					a.renderCardWithArt(c.Key(fmt.Sprintf("card-%d", j)), item, loadArt && j >= first && j <= last)
				}
			})
		})
		a.shelfRects = append(a.shelfRects, shelf.Bounds())
	})
}

func (a *app) renderShortcuts(c *ui.Context, key string, sec ytmusic.Section) {
	p := a.pal
	ui.Row(c).FillWidth().Gap(16).Children(func() {
		for i, item := range sec.Items {
			item := item
			card := ui.Column(c.Key(fmt.Sprintf("%s-%d", key, i))).Grow(1).Basis(0).Height(96).Radius(10).Padding(16).Background(p.card).AlignItems(ui.Start).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer)
			if card.Hovered() {
				card.Background(p.hover)
			}
			if card.Clicked() {
				a.openShortcut(item)
			}
			card.Children(func() {
				ui.Icon(c, shortcutIcon(item.Icon)).Size(20, 20).TextColor(p.text)
				ui.Text(c, item.Title).FontSize(15).Bold().TextColor(p.text).SingleLine()
			})
		}
	})
}

func (a *app) openShortcut(item ytmusic.CardItem) {
	if item.ID == "" {
		return
	}
	if item.ID == "FEmusic_charts" {
		a.open(route{page: pageCharts})
		return
	}
	a.open(route{page: pageCategory, id: item.ID, params: item.Params})
}

func (a *app) viewLibrary(c *ui.Context) {
	p := a.pal
	if a.libraryFilter == "" {
		a.libraryFilter = "all"
	}

	ui.Column(c).FillWidth().Gap(24).Children(func() {
		a.libraryFilters(c)

		if a.libraryLoading && len(a.libraryPlaylists) == 0 {
			ui.Column(c).FillWidth().Height(220).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.SyncingYourLibrary()).FontSize(13).TextColor(p.muted)
			})
			return
		}

		switch a.libraryFilter {
		case "songs":
			a.librarySongs(c)
		case "all", "playlists", "albums", "artists", "profile", "podcasts":
			a.libraryGrid(c)
		default:
			a.libraryEmpty(c, locales.NothingHereYet())
		}
	})
}

func (a *app) libraryFilters(c *ui.Context) {
	p := a.pal
	filters := []struct {
		key   string
		label string
	}{
		{"all", locales.All()},
		{"playlists", locales.Playlist()},
		{"songs", locales.Songs()},
		{"albums", locales.Albums()},
		{"artists", locales.Artists()},
		{"profile", locales.Profile()},
		{"podcasts", locales.Podcasts()},
	}

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		for _, f := range filters {
			active := a.libraryFilter == f.key
			bg, col := p.card, p.text
			if active {
				bg, col = p.text, p.sidebar
			}
			pill := ui.Row(c.Key("lib-filter-"+f.key)).Height(30).Radius(15).Background(bg).Padding(0, 14).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if pill.Clicked() {
				a.libraryFilter = f.key
				c.Invalidate()
			}
			pill.Children(func() {
				ui.Text(c, f.label).FontSize(12.5).TextColor(col)
			})
		}
		ui.Spacer(c)
		sort := ui.Row(c.Key("lib-sort")).Height(30).Radius(15).Background(p.card).Padding(0, 12).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
		sort.Children(func() {
			ui.Text(c, locales.RecentActivity()).FontSize(12.5).TextColor(p.text)
			ui.Icon(c, iconChevron).Size(12, 12).TextColor(p.muted)
		})
	})
}

func (a *app) libraryGrid(c *ui.Context) {
	items := a.libraryFilterItems()
	if len(items) == 0 {
		labels := map[string]string{
			"playlists": locales.NoPlaylistsYet(),
			"albums":    locales.NoAlbumsYet(),
			"artists":   locales.NoArtistsYet(),
			"profile":   locales.NoProfileYet(),
			"podcasts":  locales.NoPodcastsYet(),
		}
		text := labels[a.libraryFilter]
		if text == "" {
			text = locales.NothingHereYet()
		}
		a.libraryEmpty(c, text)
		return
	}

	ui.Row(c).FillWidth().Wrap().Gap(24).AlignItems(ui.Start).Children(func() {
		for i, item := range items {
			a.renderCard(c.Key(fmt.Sprintf("lib-card-%d", i)), item)
		}
	})
}

func (a *app) libraryFilterItems() []ytmusic.CardItem {
	if a.libraryFilter == "profile" {
		if a.profile == nil {
			return nil
		}
		return []ytmusic.CardItem{{
			ID:        "profile",
			Title:     a.profile.Name,
			Subtitle:  a.profile.Handle,
			Thumbnail: a.profile.Avatar,
			Type:      "profile",
		}}
	}

	var items []ytmusic.CardItem
	if a.libraryFilter == "all" || a.libraryFilter == "playlists" {
		items = append(items, ytmusic.CardItem{
			ID:       "LM",
			Title:    locales.LikedMusicCached(),
			Subtitle: locales.AutoPlaylist(),
			Type:     "playlist",
		})
	}

	if len(a.libraryItems) > 0 {
		for _, item := range a.libraryItems {
			if isLikedPlaylistID(item.ID) {
				continue
			}
			switch a.libraryFilter {
			case "all":
				items = append(items, item)
			case "playlists":
				if item.Type == "playlist" {
					items = append(items, item)
				}
			case "albums":
				if item.Type == "album" {
					items = append(items, item)
				}
			case "artists":
				if item.Type == "artist" {
					items = append(items, item)
				}
			case "podcasts":
				if item.Type == "podcast" {
					items = append(items, item)
				}
			}
		}
		return items
	}

	if a.libraryFilter != "all" && a.libraryFilter != "playlists" {
		return items
	}
	for _, pl := range a.libraryPlaylists {
		subtitle := locales.Playlist()
		if pl.Author != "" {
			subtitle += " • " + pl.Author
		}
		if pl.TrackCount != "" {
			subtitle += " • " + pl.TrackCount
		}
		items = append(items, ytmusic.CardItem{
			ID:        pl.ID,
			Title:     pl.Title,
			Subtitle:  subtitle,
			Thumbnail: pl.Thumbnail,
			Type:      "playlist",
		})
	}
	return items
}

func (a *app) librarySongs(c *ui.Context) {
	tracks := a.likedTracks()
	if len(tracks) == 0 {
		a.libraryEmpty(c, locales.NoSongsYet())
		return
	}

	cur := a.pl.Current()
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for i := range tracks {
			a.renderTrackRow(c.Key(fmt.Sprintf("lib-track-%d", i)), tracks, i, cur)
		}
	})
}

func (a *app) libraryEmpty(c *ui.Context, text string) {
	p := a.pal
	ui.Column(c).FillWidth().Height(220).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, iconLibrary).Size(30, 30).TextColor(p.faint)
		ui.Text(c, text).FontSize(13).TextColor(p.muted)
	})
}

func (a *app) openMore(key string, sec ytmusic.Section) {
	a.more[key] = sec
	a.open(route{page: pageSection, id: key})
}

func (a *app) trackListState(key string) *ui.ListState {
	if a.trackLists == nil {
		a.trackLists = map[string]*ui.ListState{}
	}
	s := a.trackLists[key]
	if s == nil {
		s = &ui.ListState{}
		a.trackLists[key] = s
	}
	return s
}

func (a *app) pageUsesTrackList() bool {
	switch a.route.page {
	case pagePlaylist, pageAlbum, pageLiked, pageArtist, pageArtistSongs:
		return true
	case pageSearch:
		if a.route.kind != ytmusic.SearchSongs && a.route.kind != ytmusic.SearchVideos && a.route.kind != ytmusic.SearchPodcastEpisodes {
			return false
		}
		res := a.searches[a.route]
		return res != nil && len(res.Tracks) > 0 &&
			len(res.Artists) == 0 && len(res.Albums) == 0 && len(res.Playlists) == 0 && len(res.Podcasts) == 0
	default:
		return false
	}
}

func (a *app) currentTrackListState() *ui.ListState {
	switch a.route.page {
	case pagePlaylist:
		return a.trackListState("playlist:" + playlistKey(a.route.id))
	case pageAlbum:
		return a.trackListState("album:" + a.route.id)
	case pageLiked:
		return a.trackListState("liked")
	case pageArtistSongs:
		return a.trackListState(artistSongsKey(a.route.id))
	default:
		return nil
	}
}

func (a *app) sectionScroll(key string) *ui.ScrollState {
	s := a.scrolled[key]
	if s == nil {
		s = &ui.ScrollState{}
		a.scrolled[key] = s
	}
	return s
}

func (a *app) sectionHeader(c *ui.Context, key, title string, prevOn, nextOn bool, prev, next, more func()) {
	p := a.pal

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(20).Bold().TextColor(p.text)
		ui.Spacer(c)

		moreBtn := ui.Row(c.Key("sec-more-"+key)).Height(30).Padding(0, 14).Radius(15).Background(p.card).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		moreBtn.Label(locales.More())
		if moreBtn.Hovered() {
			moreBtn.Background(p.hover)
		}
		if moreBtn.Clicked() {
			more()
		}
		moreBtn.Children(func() {
			ui.Text(c, locales.More()).FontSize(12).TextColor(p.text)
		})

		arrow := func(label string, suffix string, icon *ui.SVG, on bool, fn func()) {
			b := ui.Box(c.Key("sec-"+suffix+"-"+key)).Size(30, 30).Radius(15).BorderWidth(1, 1, 1, 1).BorderColor(p.hairline).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
			b.Label(label)
			if !on {
				b.Opacity(0.3)
			} else {
				if b.Hovered() {
					b.Background(p.hover)
				}
				if b.Clicked() {
					fn()
				}
			}
			b.Children(func() {
				ui.Icon(c, icon).Size(14, 14).TextColor(p.text)
			})
		}
		arrow(locales.PreviousPage(), "prev", iconBack, prevOn, prev)
		arrow(locales.NextPage(), "next", iconForward, nextOn, next)
	})
}

func (a *app) renderMoodSection(c *ui.Context, key string, sec ytmusic.Section) {
	const perPage = 24
	total := (len(sec.Items) + perPage - 1) / perPage
	page := a.moodPages[key]
	if total == 0 {
		page = 0
	} else if page >= total {
		page = total - 1
		a.moodPages[key] = page
	}

	prevOn := page > 0
	nextOn := page+1 < total
	a.sectionHeader(c, key, sec.Title, prevOn, nextOn,
		func() {
			if page > 0 {
				a.moodPages[key] = page - 1
				c.Invalidate()
			}
		},
		func() {
			if page+1 < total {
				a.moodPages[key] = page + 1
				c.Invalidate()
			}
		},
		func() {
			a.openMore(key, sec)
		},
	)

	start := page * perPage
	end := min(start+perPage, len(sec.Items))
	if start < end {
		a.renderMoodGrid(c, sec.Items[start:end], 0)
	}
}

func (a *app) viewSection(c *ui.Context) {
	p := a.pal
	sec, ok := a.more[a.route.id]
	if !ok {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, locales.NotAvailable()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(20).Children(func() {
		ui.Text(c, sec.Title).FontSize(28).Bold().TextColor(p.text)
		if sec.Layout == "moods" {
			a.renderMoodGrid(c, sec.Items, 0)
			return
		}
		ui.Row(c).FillWidth().Wrap().Gap(16).AlignItems(ui.Start).Children(func() {
			for j, item := range sec.Items {
				a.renderCard(c.Key(fmt.Sprintf("more-card-%d", j)), item)
			}
		})
	})
}

func (a *app) renderMoodGrid(c *ui.Context, items []ytmusic.CardItem, limit int) {
	p := a.pal
	const cols = 6
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	for start := 0; start < len(items); start += cols {
		end := min(start+cols, len(items))
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			for i := start; i < end; i++ {
				item := items[i]
				// Every mood shares one browse id, so the unique params must
				// be part of the key or all buttons share one state.
				moodKey := "mood-" + item.Params
				if item.Params == "" {
					moodKey = fmt.Sprintf("mood-%s-%d", item.ID, i)
				}
				pill := ui.ButtonBase(c).Key(moodKey).Label(item.Title).Grow(1).Height(34).Radius(4).Background(p.card).AlignItems(ui.Center).Justify(ui.Start).Gap(12).Padding(0, 12, 0, 0).Cursor(ui.CursorPointer)
				if pill.Hovered() {
					pill.Background(p.hover)
				}
				if pill.Clicked() {
					a.open(route{page: pageCategory, id: item.ID, params: item.Params})
				}
				pill.ContextMenu(func(m *ui.Menu) {
					if m.Item(locales.CopyName()).Chosen() {
						c.WriteClipboard(item.Title)
					}
				})
				pill.Children(func() {
					col := p.accent
					if item.Color != 0 {
						col = ui.RGB(uint8(item.Color>>16), uint8(item.Color>>8), uint8(item.Color))
					}
					ui.Box(c).Width(4).Height(20).Radius(2).Background(col)
					// Keep the pill free of tooltips: their popups can take the
					// click that should open the category.
					ui.Text(c, item.Title).FontSize(13).TextColor(p.text).SingleLine()
				})
			}
		})
	}
}

func (a *app) renderCard(c *ui.Context, item ytmusic.CardItem) {
	a.renderCardWithArt(c, item, true)
}

func (a *app) renderCardWithArt(c *ui.Context, item ytmusic.CardItem, loadArt bool) {
	p := a.pal

	card := ui.Column(c).Width(150).Gap(8).Cursor(ui.CursorPointer).ContextMenu(a.cardCopyMenu(c, item))
	if card.Clicked() {
		switch item.Type {
		case "track":
			if item.Track != nil {
				a.pl.PlayTrack(*item.Track)
			} else {
				a.pl.PlayTrack(ytmusic.Track{ID: item.ID, Title: item.Title})
			}
		case "playlist":
			if isLikedPlaylistID(item.ID) {
				a.open(route{page: pageLiked})
			} else {
				a.open(route{page: pagePlaylist, id: item.ID})
			}
		case "album":
			a.open(route{page: pageAlbum, id: item.ID})
		case "artist":
			a.open(route{page: pageArtist, id: item.ID})
		case "podcast":
			a.open(route{page: pagePlaylist, id: item.ID})
		case "profile":
			a.openSettings()
		default:
			if item.Track != nil {
				a.pl.PlayTrack(*item.Track)
			}
		}
	}

	card.Children(func() {
		// Cover image
		if isLikedPlaylistID(item.ID) {
			liked := ui.Box(c).Size(150, 150).Radius(8).LinearGradient(ui.LinearGradient{
				From:  ui.RGB(236, 64, 122),
				To:    ui.RGB(156, 39, 176),
				Angle: 135,
			}).AlignItems(ui.Center).Justify(ui.Center)
			liked.Children(func() {
				ui.Icon(c, iconHeartFill).Size(48, 48).TextColor(ui.RGBA(1, 1, 1, 0.92))
			})
		} else if loadArt {
			a.coverImage(c, item.Thumbnail, 150, 8)
		} else {
			a.coverImageCached(c, item.Thumbnail, 150, 8)
		}

		// Titles
		ui.Text(c, item.Title).FontSize(13).Bold().TextColor(p.text).MaxLines(2).Tooltip(item.Title)
		if item.Subtitle != "" {
			ui.Text(c, item.Subtitle).FontSize(12).TextColor(p.muted).SingleLine().Tooltip(item.Subtitle)
		}
	})
}

func (a *app) viewPlaylist(c *ui.Context) {
	liked := isLikedPlaylistID(a.route.id)
	if liked && a.profile == nil {
		a.viewLiked(c)
		return
	}
	if liked {
		a.loadLiked()
	} else if a.playlistRefreshing[a.route.id] {
		a.playlistLoading(c)
		return
	}
	pl := a.playlists[a.route.id]
	if liked {
		if pl == nil {
			pl = a.playlists["LM"]
		}
		if pl == nil {
			pl = &ytmusic.Playlist{ID: a.route.id, Title: locales.LikedMusic()}
		}
	}
	if pl == nil {
		a.playlistLoading(c)
		return
	}
	tracks := pl.Tracks
	if liked || isLikedPlaylistID(pl.ID) {
		tracks = a.likedTracks()
		if len(tracks) == 0 && a.likedLoading {
			a.playlistLoading(c)
			return
		}
	}
	a.maybeLoadMorePlaylist(c)
	a.playlistTrackList(c, pl, tracks)
}

// playlistLoading fills the page while its playlist is on its way.
func (a *app) playlistLoading(c *ui.Context) {
	p := a.pal
	loading := locales.LoadingThePlaylist()
	if ytmusic.IsPodcastShowID(a.route.id) {
		loading = locales.LoadingThePodcast()
	}
	ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
		ui.Spinner(c)
		ui.Text(c, loading).FontSize(14).TextColor(p.muted)
	})
}

func (a *app) playlistTrackList(c *ui.Context, pl *ytmusic.Playlist, tracks []ytmusic.Track) {
	key := playlistKey(pl.ID)
	count := 1 + len(tracks)
	if a.loadingMore[key] {
		count++
	}
	cur := a.pl.Current()

	ui.List(c, a.trackListState("playlist:"+key), count).Fill().Gap(2).Rows(func(r ui.ListRow) {
		switch {
		case r.Index == 0:
			a.playlistHeader(r.Context, pl, tracks)
		case r.Index <= len(tracks):
			a.renderTrackRow(r.Context, tracks, r.Index-1, cur)
		default:
			a.playlistMoreFooter(r.Context, key)
		}
	})
}

func (a *app) playlistHeader(c *ui.Context, pl *ytmusic.Playlist, tracks []ytmusic.Track) {
	p := a.pal
	// A show's page lists episodes, not songs: name it and count them so.
	kind, byline, playAll := locales.Playlists(), locales.CreatedByPrefix(), locales.PlayAllCount()
	if pl.Podcast {
		kind, byline, playAll = locales.Podcasts(), locales.HostedByPrefix(), locales.PlayAllEpisodes()
	}
	ui.Column(c).FillWidth().Padding(0, 0, 24, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.End).Children(func() {
			a.playlistCover(c, pl, 160, 12)
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, kind).FontSize(12).Bold().TextColor(p.accent)
				ui.Text(c, pl.Title).FontSize(28).Bold().TextColor(p.text)
				if pl.Author != "" {
					name := byline + pl.Author
					if pl.Podcast && pl.AuthorID != "" {
						// The host's channel: the page the show's episodes
						// come from.
						a.link(c.Key("pl-author"), name, 13, p.muted, p.text, route{page: pageArtist, id: pl.AuthorID})
					} else {
						ui.Text(c, name).FontSize(13).TextColor(p.muted)
					}
				}
				if pl.Description != "" {
					ui.Text(c, pl.Description).FontSize(12).TextColor(p.faint).MaxLines(2).Tooltip(pl.Description)
				}
				playAllBtn := ui.Row(c.Key("pl-play-all")).AlignSelf(ui.Start).Height(36).Radius(18).Background(p.accent).Padding(0, 18).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
				if playAllBtn.Clicked() {
					if len(tracks) > 0 {
						a.playAllPlaylist(c, playlistKey(pl.ID), tracks)
					}
				}
				playAllBtn.Children(func() {
					ui.Icon(c, iconPlay).Size(16, 16).TextColor(p.onAccent)
					total := pl.TrackCount
					if total < len(tracks) {
						total = len(tracks)
					}
					ui.Text(c, fmt.Sprintf(playAll, total)).FontSize(13).Bold().TextColor(p.onAccent)
				})
			})
		})
	})
}

func playlistKey(id string) string {
	if isLikedPlaylistID(id) {
		return "LM"
	}
	return id
}

// artistSongsKey is the cache key of an artist's complete song list. The list
// is playlist-shaped — it pages by continuation and plays as a queue — so it
// lives in the playlist cache, under a key no playlist route can use.
func artistSongsKey(artistID string) string {
	return "artist-songs:" + artistID
}

func (a *app) playlistMoreFooter(c *ui.Context, key string) {
	p := a.pal
	ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		if a.loadingMore[key] {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingMore()).FontSize(12).TextColor(p.muted)
		} else {
			ui.Text(c, locales.LoadMoreHint()).FontSize(12).TextColor(p.faint)
		}
	})
}

func (a *app) viewAlbum(c *ui.Context) {
	al := a.albums[a.route.id]
	if al == nil {
		p := a.pal
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingTheAlbum()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	count := 1 + len(al.Tracks)
	cur := a.pl.Current()
	ui.List(c, a.trackListState("album:"+al.ID), count).Fill().Gap(2).Rows(func(r ui.ListRow) {
		if r.Index == 0 {
			a.albumHeader(r.Context, al)
			return
		}
		a.renderTrackRow(r.Context, al.Tracks, r.Index-1, cur)
	})
}

func (a *app) albumHeader(c *ui.Context, al *ytmusic.Album) {
	p := a.pal
	ui.Column(c).FillWidth().Padding(0, 0, 24, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.End).Children(func() {
			a.coverImage(c, al.Thumbnail, 160, 12)
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, locales.Albums()).FontSize(12).Bold().TextColor(p.accent)
				ui.Text(c, al.Title).FontSize(28).Bold().TextColor(p.text)
				if len(al.Artists) > 0 {
					// The byline links like the artist line of every track
					// below it, opening the artist rather than dead-ending.
					a.artistByline(c, "album-"+al.ID, al.Artists, 14, ytmusic.SearchArtists, p.text, p.text)
				}
				if al.Year != "" {
					ui.Text(c, al.Year).FontSize(12).TextColor(p.muted)
				}
				playAllBtn := ui.Row(c.Key("al-play-all")).AlignSelf(ui.Start).Height(36).Radius(18).Background(p.accent).Padding(0, 18).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
				if playAllBtn.Clicked() {
					if len(al.Tracks) > 0 {
						a.pl.SetQueue(al.Tracks, 0)
					}
				}
				playAllBtn.Children(func() {
					ui.Icon(c, iconPlay).Size(16, 16).TextColor(p.onAccent)
					ui.Text(c, fmt.Sprintf(locales.PlayAlbumCount(), len(al.Tracks))).FontSize(13).Bold().TextColor(p.onAccent)
				})
			})
		})
	})
}

func (a *app) viewArtist(c *ui.Context) {
	art := a.artists[a.route.id]
	if art == nil {
		p := a.pal
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingTheArtist()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	hasAlbums := len(art.Albums) > 0
	hasSingles := len(art.Singles) > 0
	count := 1 + len(art.TopTracks) + len(art.Collections)
	if hasAlbums {
		count++
	}
	if hasSingles {
		count++
	}

	cur := a.pl.Current()
	ui.List(c, a.trackListState("artist:"+art.ID), count).Fill().Gap(2).Rows(func(r ui.ListRow) {
		idx := r.Index
		if idx == 0 {
			a.artistHeader(r.Context, art)
			return
		}
		idx--
		if idx < len(art.TopTracks) {
			a.renderTrackRow(r.Context, art.TopTracks, idx, cur)
			return
		}
		idx -= len(art.TopTracks)
		if hasAlbums {
			if idx == 0 {
				a.artistAlbumsSection(r.Context, art)
				return
			}
			idx--
		}
		if hasSingles {
			if idx == 0 {
				a.artistSinglesSection(r.Context, art)
				return
			}
			idx--
		}
		if idx >= 0 && idx < len(art.Collections) {
			a.artistCollectionSection(r.Context, art, idx)
		}
	})
}

func (a *app) artistHeader(c *ui.Context, art *ytmusic.Artist) {
	p := a.pal
	ui.Column(c).FillWidth().Gap(24).Padding(0, 0, 20, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Center).Children(func() {
			a.coverImage(c, art.Thumbnail, 120, 60)
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, art.Name).FontSize(32).Bold().TextColor(p.text)
				if art.Subscribers != "" {
					ui.Text(c, art.Subscribers+locales.SubscribersSuffix()).FontSize(13).TextColor(p.muted)
				}
				// The shelf's more button is the only way to the artist's
				// complete song list; without it there is nothing to show.
				if art.SongsID != "" {
					allSongsBtn := ui.Row(c.Key("art-all-songs")).AlignSelf(ui.Start).Height(32).Radius(16).Background(p.card).Padding(0, 16).AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer)
					if allSongsBtn.Clicked() {
						a.open(route{page: pageArtistSongs, id: art.ID})
					}
					allSongsBtn.Children(func() {
						ui.Icon(c, iconMusic).Size(14, 14).TextColor(p.text)
						ui.Text(c, locales.SeeAllSongs()).FontSize(12).Bold().TextColor(p.text)
					})
				}
			})
		})
		if len(art.TopTracks) > 0 {
			ui.Text(c, locales.PopularSongs()).FontSize(18).Bold().TextColor(p.text)
		}
	})
}

func (a *app) artistAlbumsSection(c *ui.Context, art *ytmusic.Artist) {
	items := make([]ytmusic.CardItem, len(art.Albums))
	for i, al := range art.Albums {
		items[i] = ytmusic.CardItem{
			ID:        al.ID,
			Title:     al.Title,
			Subtitle:  al.Year,
			Thumbnail: al.Thumbnail,
			Type:      albumCardType(al.Type),
			Track:     al.Track,
		}
	}
	a.renderSection(c, "artist-albums-"+art.ID, ytmusic.Section{Title: locales.Albums(), Items: items})
}

// artistCollectionSection shows one of the page's other carousels under the
// title it came with: a podcast host's shows, an artist's videos.
func (a *app) artistCollectionSection(c *ui.Context, art *ytmusic.Artist, i int) {
	col := art.Collections[i]
	a.renderSection(c, fmt.Sprintf("artist-col-%s-%d", art.ID, i), ytmusic.Section{Title: col.Title, Items: col.Items})
}

func (a *app) artistSinglesSection(c *ui.Context, art *ytmusic.Artist) {
	items := make([]ytmusic.CardItem, len(art.Singles))
	for i, sg := range art.Singles {
		items[i] = ytmusic.CardItem{
			ID:        sg.ID,
			Title:     sg.Title,
			Subtitle:  sg.Year,
			Thumbnail: sg.Thumbnail,
			Type:      albumCardType(sg.Type),
			Track:     sg.Track,
		}
	}
	a.renderSection(c, "artist-singles-"+art.ID, ytmusic.Section{Title: locales.SinglesAndEPs(), Items: items})
}

// viewArtistSongs shows the song list behind an artist page. The shelf on the
// artist page previews only a handful of songs, and searching the artist's
// name would hand back other artists' hits under that name.
func (a *app) viewArtistSongs(c *ui.Context) {
	p := a.pal
	id := a.route.id
	key := artistSongsKey(id)
	pl := a.playlists[key]
	if pl == nil {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingSongs()).FontSize(14).TextColor(p.muted)
		})
		return
	}

	tracks := pl.Tracks
	if len(tracks) == 0 && !a.loadingMore[key] {
		ui.Column(c).FillWidth().Gap(20).Children(func() {
			a.artistSongsHeader(c, id, key, pl, tracks)
			a.libraryEmpty(c, locales.NoSongsYet())
		})
		return
	}

	a.maybeLoadMorePlaylist(c)

	count := 1 + len(tracks)
	if a.loadingMore[key] {
		count++
	}
	cur := a.pl.Current()
	ui.List(c, a.trackListState(key), count).Fill().Gap(2).Rows(func(r ui.ListRow) {
		switch {
		case r.Index == 0:
			a.artistSongsHeader(r.Context, id, key, pl, tracks)
		case r.Index <= len(tracks):
			a.renderTrackRow(r.Context, tracks, r.Index-1, cur)
		default:
			a.playlistMoreFooter(r.Context, key)
		}
	})
}

func (a *app) artistSongsHeader(c *ui.Context, artistID, key string, pl *ytmusic.Playlist, tracks []ytmusic.Track) {
	p := a.pal
	name, cover := pl.Title, ""
	if art := a.artists[artistID]; art != nil {
		name, cover = art.Name, art.Thumbnail
	}
	if name == "" {
		name = locales.AllSongs()
	}

	ui.Column(c).FillWidth().Padding(0, 0, 24, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.End).Children(func() {
			a.coverImage(c, cover, 160, 12)
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, locales.Artists()).FontSize(12).Bold().TextColor(p.accent)
				ui.Text(c, name).FontSize(28).Bold().TextColor(p.text)
				ui.Text(c, artistSongsCount(len(tracks), pl.Continuation != "")).FontSize(13).TextColor(p.muted)
				playAllBtn := ui.Row(c.Key("artist-songs-play-all")).AlignSelf(ui.Start).Height(36).Radius(18).Background(p.accent).Padding(0, 18).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
				if playAllBtn.Clicked() {
					if len(tracks) > 0 {
						a.playAllPlaylist(c, key, tracks)
					}
				}
				playAllBtn.Children(func() {
					ui.Icon(c, iconPlay).Size(16, 16).TextColor(p.onAccent)
					ui.Text(c, fmt.Sprintf(locales.PlayAllCount(), len(tracks))).FontSize(13).Bold().TextColor(p.onAccent)
				})
			})
		})
	})
}

// artistSongsCount reports how much of a paged song list has arrived: the
// endpoint answers a page at a time until its continuation runs dry.
func artistSongsCount(loaded int, more bool) string {
	if more {
		return fmt.Sprintf(locales.SongsLoaded(), loaded)
	}
	return fmt.Sprintf(locales.SongCount(), loaded)
}

func (a *app) viewLiked(c *ui.Context) {
	likedPlaylist := a.playlists["LM"]
	tracks := a.likedTracks()
	total := len(tracks)
	if likedPlaylist != nil && likedPlaylist.TrackCount > total {
		total = likedPlaylist.TrackCount
	}
	a.maybeLoadMorePlaylist(c)

	if len(tracks) == 0 {
		if a.likedLoading {
			a.playlistLoading(c)
			return
		}
		ui.Column(c).FillWidth().Gap(24).Children(func() {
			a.likedHeader(c, likedPlaylist, tracks, total)
			a.likedEmpty(c)
		})
		return
	}

	count := 1 + len(tracks)
	if a.loadingMore["LM"] {
		count++
	}
	cur := a.pl.Current()
	ui.List(c, a.trackListState("liked"), count).Fill().Gap(2).Rows(func(r ui.ListRow) {
		switch {
		case r.Index == 0:
			a.likedHeader(r.Context, likedPlaylist, tracks, total)
		case r.Index <= len(tracks):
			a.renderTrackRow(r.Context, tracks, r.Index-1, cur)
		default:
			a.playlistMoreFooter(r.Context, "LM")
		}
	})
}

func (a *app) likedHeader(c *ui.Context, likedPlaylist *ytmusic.Playlist, tracks []ytmusic.Track, total int) {
	p := a.pal
	ui.Column(c).FillWidth().Padding(0, 0, 24, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(120, 120).Radius(12).Background(p.accent).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(c, iconHeartFill).Size(48, 48).TextColor(p.onAccent)
			})
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, locales.LikedMusic()).FontSize(28).Bold().TextColor(p.text)
				ui.Text(c, fmt.Sprintf(locales.SongCount(), total)).FontSize(13).TextColor(p.muted)
				if len(tracks) > 0 {
					playAllBtn := ui.Row(c.Key("liked-play-all")).AlignSelf(ui.Start).Height(34).Radius(17).Background(p.accent).Padding(0, 16).AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer)
					if playAllBtn.Clicked() {
						if likedPlaylist != nil {
							a.playAllPlaylist(c, "LM", tracks)
						} else {
							a.pl.SetQueue(tracks, 0)
						}
					}
					playAllBtn.Children(func() {
						ui.Icon(c, iconPlay).Size(14, 14).TextColor(p.onAccent)
						ui.Text(c, locales.PlayAll()).FontSize(12).Bold().TextColor(p.onAccent)
					})
				}
			})
		})
	})
}

func (a *app) likedEmpty(c *ui.Context) {
	p := a.pal
	if a.profile != nil && a.libraryLoading {
		ui.Column(c).FillWidth().Height(200).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.SyncingLikedMusicFromYouTube()).FontSize(14).TextColor(p.muted)
		})
		return
	}
	ui.Column(c).FillWidth().Height(200).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Text(c, locales.NoLikedMusicYet()).FontSize(15).Bold().TextColor(p.muted)
		ui.Text(c, locales.LikedEmptyHint()).FontSize(13).TextColor(p.faint)
	})
}

func (a *app) renderTrackTable(c *ui.Context, tracks []ytmusic.Track) {
	cur := a.pl.Current()
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for i := range tracks {
			a.renderTrackRow(c, tracks, i, cur)
		}
	})
}

func (a *app) renderTrackRow(c *ui.Context, tracks []ytmusic.Track, idx int, cur *ytmusic.Track) {
	p := a.pal
	t := tracks[idx]
	isPlaying := cur != nil && cur.ID == t.ID

	row := ui.Row(c.Key(fmt.Sprintf("track-%s-%d", t.ID, idx))).FillWidth().Height(52).Radius(8).Padding(0, 12).AlignItems(ui.Center).Gap(14).Cursor(ui.CursorPointer).ContextMenu(a.trackCopyMenu(c, t))
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.pl.SetQueue(tracks, idx)
	}

	row.Children(func() {
		// Index or play indicator
		ui.Box(c).Width(24).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if isPlaying {
				ui.Icon(c, iconPlay).Size(14, 14).TextColor(p.accent)
			} else {
				ui.Text(c, fmt.Sprintf("%d", idx+1)).FontSize(13).TextColor(p.muted)
			}
		})

		// Thumbnail
		a.coverImage(c, t.Thumbnail, 36, 6)

		// Title & artist
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			tText := ui.Text(c, t.Title).FontSize(14)
			if isPlaying {
				tText.Bold().TextColor(p.accent)
			} else {
				tText.TextColor(p.text)
			}
			a.artistLinks(c, t, p.muted, p.text)
		})

		// Like button
		liked := a.isLiked(t.ID)
		likeLabel := locales.LikePrefix() + t.Title
		if liked {
			likeLabel = locales.UnlikePrefix() + t.Title
		}
		likeBtn := ui.Box(c.Key(fmt.Sprintf("like-row-%d", idx))).Label(likeLabel).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
		if likeBtn.Clicked() {
			a.toggleLike(t)
			c.Invalidate()
		}
		likeBtn.Children(func() {
			if liked {
				ui.Icon(c, iconHeartFill).Size(16, 16).TextColor(p.accent)
			} else {
				ui.Icon(c, iconHeart).Size(16, 16).TextColor(p.faint)
			}
		})

		// Duration
		durStr := t.DurationString()
		if t.DurationMs <= 0 {
			durStr = ""
		}
		ui.Text(c, durStr).FontSize(12).TextColor(p.muted).Width(48)
	})
}

func (a *app) artistLinks(c *ui.Context, t ytmusic.Track, color, hover ui.Color) ui.Element {
	// A show a podcast episode plays under is not a musician: searching its
	// name among the artists finds nothing.
	fallback := ytmusic.SearchArtists
	if t.Podcast {
		fallback = ytmusic.SearchPodcasts
	}
	return a.artistByline(c, t.ID, t.Artists, 12, fallback, color, hover)
}

// artistByline renders artist names that open the artist's page, the show's
// page for a podcast, and a search of the artists or the podcasts when the
// name carries no page. Page headers and track rows share it so a name links
// wherever it appears; key scopes the links within the caller.
func (a *app) artistByline(c *ui.Context, key string, artists []ytmusic.ArtistRef, size float32, fallback ytmusic.SearchType, color, hover ui.Color) ui.Element {
	return ui.Row(c).Wrap().AlignItems(ui.Center).Children(func() {
		if len(artists) == 0 {
			ui.Text(c, locales.UnknownArtist()).FontSize(size).TextColor(color)
			return
		}
		n := 0
		for _, ar := range artists {
			artist := ar
			artist.Name = ytmusic.CleanArtistName(artist.Name)
			if artist.Name == "" {
				continue
			}
			if n > 0 {
				ui.Text(c, ", ").FontSize(size).TextColor(color)
			}
			r := route{page: pageSearch, query: artist.Name, kind: fallback}
			switch {
			case strings.HasPrefix(artist.ID, "UC"):
				r = route{page: pageArtist, id: artist.ID}
			case ytmusic.IsPodcastShowID(artist.ID):
				r = route{page: pagePlaylist, id: artist.ID}
			}
			a.link(c.Key(fmt.Sprintf("art-link-%s-%d", key, n)), artist.Name, size, color, hover, r)
			n++
		}
	})
}

func (a *app) link(c *ui.Context, text string, size float32, color, hover ui.Color, r route) {
	row := ui.Row(c).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	var label ui.Element
	row.Children(func() {
		label = ui.Text(c, text).FontSize(size).TextColor(color)
	})
	if row.Hovered() {
		label.TextColor(hover).Underline()
	}
	if row.Clicked() {
		a.nowPlayingOpen = false
		a.open(r)
	}
}

func albumCardType(typ string) string {
	switch typ {
	case "artist", "playlist", "track", "podcast":
		return typ
	default:
		return "album"
	}
}
