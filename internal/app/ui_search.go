package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) searchFilters(c *ui.Context, r route) {
	p := a.pal
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		categories := []struct {
			label string
			kind  ytmusic.SearchType
		}{
			{"全部", ytmusic.SearchAll},
			{"歌曲", ytmusic.SearchSongs},
			{"视频", ytmusic.SearchVideos},
			{"专辑", ytmusic.SearchAlbums},
			{"音乐人", ytmusic.SearchArtists},
			{"歌单", ytmusic.SearchPlaylists},
		}

		for _, cat := range categories {
			active := r.kind == cat.kind
			bg := p.card
			txtCol := p.muted
			if active {
				bg = p.text
				txtCol = p.sidebar
			}
			pill := ui.Row(c.Key("pill-"+cat.label)).Height(32).Radius(16).Background(bg).Padding(0, 16).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if pill.Clicked() {
				a.searchFilter = cat.kind
				a.open(route{
					page:  pageSearch,
					query: r.query,
					kind:  cat.kind,
				})
			}
			pill.Children(func() {
				t := ui.Text(c, cat.label).FontSize(13).TextColor(txtCol)
				if active {
					t.Bold()
				}
			})
		}
	})
}

func (a *app) searchTrackList(c *ui.Context, r route, res *ytmusic.SearchResult) {
	key := fmt.Sprintf("search:%s:%d", r.query, r.kind)
	cur := a.pl.Current()
	count := 1 + len(res.Tracks)

	ui.List(c, a.trackListState(key), count).Fill().Gap(2).Rows(func(row ui.ListRow) {
		if row.Index == 0 {
			title := "歌曲"
			if r.kind == ytmusic.SearchVideos {
				title = "视频"
			}
			ui.Column(row.Context).FillWidth().Gap(20).Padding(0, 0, 12, 0).Children(func() {
				a.searchFilters(row.Context, r)
				ui.Text(row.Context, title).FontSize(18).Bold().TextColor(a.pal.text)
			})
			return
		}
		a.renderTrackRow(row.Context, res.Tracks, row.Index-1, cur)
	})
}

func (a *app) viewSearch(c *ui.Context) {
	p := a.pal
	r := a.route
	res := a.searches[r]

	if a.pageUsesTrackList() && res != nil && len(res.Tracks) > 0 &&
		len(res.Artists) == 0 && len(res.Albums) == 0 && len(res.Playlists) == 0 {
		a.searchTrackList(c, r, res)
		return
	}

	ui.Column(c).FillWidth().Gap(20).Children(func() {
		a.searchFilters(c, r)

		// Loading indicator
		if res == nil {
			ui.Column(c).FillWidth().Height(200).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, fmt.Sprintf("正在搜索「%s」...", r.query)).FontSize(14).TextColor(p.muted)
			})
			return
		}

		// Empty check
		hasResults := len(res.Tracks) > 0 || len(res.Artists) > 0 || len(res.Albums) > 0 || len(res.Playlists) > 0
		if !hasResults {
			ui.Column(c).FillWidth().Height(200).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "未找到相关结果").FontSize(16).Bold().TextColor(p.muted)
				ui.Text(c, "请尝试使用其他关键词搜索").FontSize(13).TextColor(p.faint)
			})
			return
		}

		// Tracks
		if len(res.Tracks) > 0 {
			ui.Text(c, "歌曲").FontSize(18).Bold().TextColor(p.text)
			a.renderTrackTable(c, res.Tracks)
		}

		searchKey := fmt.Sprintf("search-%s-%d", r.query, r.kind)

		// Artists
		if len(res.Artists) > 0 {
			items := make([]ytmusic.CardItem, len(res.Artists))
			for i, art := range res.Artists {
				items[i] = ytmusic.CardItem{
					ID:        art.ID,
					Title:     art.Name,
					Thumbnail: art.Thumbnail,
					Type:      "artist",
				}
			}
			a.renderSection(c, searchKey+"-artists", ytmusic.Section{Title: "音乐人", Items: items})
		}

		// Albums
		if len(res.Albums) > 0 {
			items := make([]ytmusic.CardItem, len(res.Albums))
			for i, al := range res.Albums {
				items[i] = ytmusic.CardItem{
					ID:        al.ID,
					Title:     al.Title,
					Subtitle:  al.Artist,
					Thumbnail: al.Thumbnail,
					Type:      "album",
				}
			}
			a.renderSection(c, searchKey+"-albums", ytmusic.Section{Title: "专辑", Items: items})
		}

		// Playlists
		if len(res.Playlists) > 0 {
			items := make([]ytmusic.CardItem, len(res.Playlists))
			for i, pl := range res.Playlists {
				items[i] = ytmusic.CardItem{
					ID:        pl.ID,
					Title:     pl.Title,
					Subtitle:  pl.Author,
					Thumbnail: pl.Thumbnail,
					Type:      "playlist",
				}
			}
			a.renderSection(c, searchKey+"-playlists", ytmusic.Section{Title: "歌单", Items: items})
		}
	})
}
