package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) viewHome(c *ui.Context) {
	p := a.pal

	if a.homeLoading && len(a.homeSections) == 0 {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载音乐推荐...").FontSize(14).TextColor(p.muted)
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
			ui.Text(c, "正在探索新音乐...").FontSize(14).TextColor(p.muted)
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
			ui.Text(c, "正在加载分类...").FontSize(14).TextColor(p.muted)
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
			ui.Text(c, "正在加载热门排行榜...").FontSize(14).TextColor(p.muted)
		})
		return
	}

	ui.Column(c).FillWidth().Gap(28).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Text(c, "排行榜").FontSize(28).Bold().TextColor(p.text)
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
	selectedName := "选择国家/地区"
	for _, country := range a.chartCountries {
		if country.Code == a.chartCountry {
			selectedName = country.Name
			break
		}
	}

	sel := ui.SelectBase(c.Key("chart-country"), &a.chartCountry)
	sel.Trigger.Label("选择国家/地区").Height(34).Radius(17).Background(p.card).Padding(0, 14).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer)
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
		ui.ScrollHorizontal(c).TrackScroll(state).FillWidth().Children(func() {
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
				ui.Text(c, "正在同步媒体库...").FontSize(13).TextColor(p.muted)
			})
			return
		}

		switch a.libraryFilter {
		case "songs":
			a.librarySongs(c)
		case "all", "playlists", "albums", "artists", "profile", "podcasts":
			a.libraryGrid(c)
		default:
			a.libraryEmpty(c, "暂无内容")
		}
	})
}

func (a *app) libraryFilters(c *ui.Context) {
	p := a.pal
	filters := []struct {
		key   string
		label string
	}{
		{"all", "全部"},
		{"playlists", "播放列表"},
		{"songs", "歌曲"},
		{"albums", "专辑"},
		{"artists", "音乐人"},
		{"profile", "个人资料"},
		{"podcasts", "播客"},
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
			ui.Text(c, "近期活动").FontSize(12.5).TextColor(p.text)
			ui.Icon(c, iconChevron).Size(12, 12).TextColor(p.muted)
		})
	})
}

func (a *app) libraryGrid(c *ui.Context) {
	items := a.libraryFilterItems()
	if len(items) == 0 {
		labels := map[string]string{
			"playlists": "暂无播放列表",
			"albums":    "暂无专辑",
			"artists":   "暂无音乐人",
			"profile":   "暂无个人资料",
			"podcasts":  "暂无播客",
		}
		text := labels[a.libraryFilter]
		if text == "" {
			text = "暂无内容"
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
			Title:    "赞过的音乐",
			Subtitle: "自动播放列表",
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
		subtitle := "播放列表"
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
		a.libraryEmpty(c, "暂无歌曲")
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
	case pagePlaylist, pageAlbum, pageLiked, pageArtist:
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
		moreBtn.Label("更多")
		if moreBtn.Hovered() {
			moreBtn.Background(p.hover)
		}
		if moreBtn.Clicked() {
			more()
		}
		moreBtn.Children(func() {
			ui.Text(c, "更多").FontSize(12).TextColor(p.text)
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
		arrow("上一页", "prev", iconBack, prevOn, prev)
		arrow("下一页", "next", iconForward, nextOn, next)
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
			ui.Text(c, "内容不可用").FontSize(14).TextColor(p.muted)
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
					if m.Item("复制名称").Chosen() {
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
			a.settingsModalOpen = true
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
	if isLikedPlaylistID(a.route.id) && a.profile == nil {
		a.viewLiked(c)
		return
	}
	p := a.pal
	pl := a.playlists[a.route.id]
	if pl == nil {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载歌单...").FontSize(14).TextColor(p.muted)
		})
		return
	}

	tracks := pl.Tracks
	if isLikedPlaylistID(a.route.id) || isLikedPlaylistID(pl.ID) {
		tracks = a.likedTracks()
	}
	a.maybeLoadMorePlaylist(c)
	a.playlistTrackList(c, pl, tracks)
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
	ui.Column(c).FillWidth().Padding(0, 0, 24, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.End).Children(func() {
			a.playlistCover(c, pl, 160, 12)
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, "歌单").FontSize(12).Bold().TextColor(p.accent)
				ui.Text(c, pl.Title).FontSize(28).Bold().TextColor(p.text)
				if pl.Author != "" {
					ui.Text(c, "创建者："+pl.Author).FontSize(13).TextColor(p.muted)
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
					ui.Text(c, fmt.Sprintf("播放全部 (%d 首)", total)).FontSize(13).Bold().TextColor(p.onAccent)
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

func (a *app) playlistMoreFooter(c *ui.Context, key string) {
	p := a.pal
	ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		if a.loadingMore[key] {
			ui.Spinner(c)
			ui.Text(c, "正在加载更多...").FontSize(12).TextColor(p.muted)
		} else {
			ui.Text(c, "继续向下滚动加载更多").FontSize(12).TextColor(p.faint)
		}
	})
}

func (a *app) viewAlbum(c *ui.Context) {
	al := a.albums[a.route.id]
	if al == nil {
		p := a.pal
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载专辑...").FontSize(14).TextColor(p.muted)
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
				ui.Text(c, "专辑").FontSize(12).Bold().TextColor(p.accent)
				ui.Text(c, al.Title).FontSize(28).Bold().TextColor(p.text)
				if len(al.Artists) > 0 {
					ui.Text(c, al.Artists[0].Name).FontSize(14).TextColor(p.text)
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
					ui.Text(c, fmt.Sprintf("播放专辑 (%d 首)", len(al.Tracks))).FontSize(13).Bold().TextColor(p.onAccent)
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
			ui.Text(c, "正在加载音乐人资料...").FontSize(14).TextColor(p.muted)
		})
		return
	}

	hasAlbums := len(art.Albums) > 0
	hasSingles := len(art.Singles) > 0
	count := 1 + len(art.TopTracks)
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
			a.artistSinglesSection(r.Context, art)
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
					ui.Text(c, art.Subscribers+" 订阅者").FontSize(13).TextColor(p.muted)
				}
				allSongsBtn := ui.Row(c.Key("art-all-songs")).AlignSelf(ui.Start).Height(32).Radius(16).Background(p.card).Padding(0, 16).AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer)
				if allSongsBtn.Clicked() {
					a.open(route{page: pageSearch, query: art.Name, kind: ytmusic.SearchSongs})
				}
				allSongsBtn.Children(func() {
					ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.text)
					ui.Text(c, "查看所有歌曲").FontSize(12).Bold().TextColor(p.text)
				})
			})
		})
		if len(art.TopTracks) > 0 {
			ui.Text(c, "热门歌曲").FontSize(18).Bold().TextColor(p.text)
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
	a.renderSection(c, "artist-albums-"+art.ID, ytmusic.Section{Title: "专辑", Items: items})
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
	a.renderSection(c, "artist-singles-"+art.ID, ytmusic.Section{Title: "单曲与迷你专辑", Items: items})
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
				ui.Text(c, "我喜欢的音乐").FontSize(28).Bold().TextColor(p.text)
				ui.Text(c, fmt.Sprintf("共 %d 首歌曲", total)).FontSize(13).TextColor(p.muted)
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
						ui.Text(c, "播放全部").FontSize(12).Bold().TextColor(p.onAccent)
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
			ui.Text(c, "正在从 YouTube 同步我喜欢的音乐...").FontSize(14).TextColor(p.muted)
		})
		return
	}
	ui.Column(c).FillWidth().Height(200).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "暂无喜欢的音乐").FontSize(15).Bold().TextColor(p.muted)
		ui.Text(c, "在播放歌曲时点击红心，或登录后同步 YouTube 媒体库").FontSize(13).TextColor(p.faint)
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
		likeLabel := "喜欢 " + t.Title
		if liked {
			likeLabel = "取消喜欢 " + t.Title
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
	return ui.Row(c).Wrap().AlignItems(ui.Center).Children(func() {
		if len(t.Artists) == 0 {
			ui.Text(c, "未知音乐人").FontSize(12).TextColor(color)
			return
		}
		n := 0
		for _, ar := range t.Artists {
			artist := ar
			artist.Name = ytmusic.CleanArtistName(artist.Name)
			if artist.Name == "" {
				continue
			}
			if n > 0 {
				ui.Text(c, ", ").FontSize(12).TextColor(color)
			}
			r := route{page: pageSearch, query: artist.Name, kind: ytmusic.SearchArtists}
			if artist.ID != "" && strings.HasPrefix(artist.ID, "UC") {
				r = route{page: pageArtist, id: artist.ID}
			}
			a.link(c.Key(fmt.Sprintf("art-link-%s-%d", t.ID, n)), artist.Name, color, hover, r)
			n++
		}
	})
}

func (a *app) link(c *ui.Context, text string, color, hover ui.Color, r route) {
	row := ui.Row(c).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	var label ui.Element
	row.Children(func() {
		label = ui.Text(c, text).FontSize(12).TextColor(color)
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
	case "artist", "playlist", "track":
		return typ
	default:
		return "album"
	}
}
