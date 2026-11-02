package app

import (
	"context"
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
)

func newTestApp() *app {
	setThemeSource = func(src mygo.ThemeSource) {}

	a := &app{
		playlists:   map[string]*ytmusic.Playlist{},
		albums:      map[string]*ytmusic.Album{},
		artists:     map[string]*ytmusic.Artist{},
		searches:    map[route]*ytmusic.SearchResult{},
		categories:  map[string]*ytmusic.Category{},
		more:        map[string]ytmusic.Section{},
		scrolled:    map[string]*ui.ScrollState{},
		moodPages:   map[string]int{},
		loadingMore: map[string]bool{},
		route:       route{page: pageHome},
		settings:    defaultSettings(),
	}

	pl := player.New(func(fn func()) { fn() })
	pl.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		return nil, errors.New("test mock offline")
	}
	a.pl = pl
	a.art = newArtCache(func(fn func()) {})

	// Populate mock catalog data for testing
	t1 := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{ID: "art1", Name: "周杰伦"}}, DurationMs: 269000}
	t2 := ytmusic.Track{ID: "track2", Title: "七里香", Artists: []ytmusic.ArtistRef{{ID: "art1", Name: "周杰伦"}}, DurationMs: 299000}
	t3 := ytmusic.Track{ID: "track3", Title: "搁浅", Artists: []ytmusic.ArtistRef{{ID: "art1", Name: "周杰伦"}}, DurationMs: 240000}

	a.homeSections = []ytmusic.Section{
		{
			Title: "为您推荐",
			Items: []ytmusic.CardItem{
				{ID: "track1", Title: "晴天", Subtitle: "周杰伦", Type: "track", Track: &t1},
				{ID: "pl1", Title: "华语流行精选", Subtitle: "YouTube Music", Type: "playlist"},
				{ID: "al1", Title: "叶惠美", Subtitle: "周杰伦", Type: "album"},
				{ID: "art1", Title: "周杰伦", Subtitle: "音乐人", Type: "artist"},
			},
		},
	}

	a.exploreSections = []ytmusic.Section{
		{
			Title: "新歌首发",
			Items: []ytmusic.CardItem{
				{ID: "track2", Title: "七里香", Subtitle: "周杰伦", Type: "track", Track: &t2},
			},
		},
	}

	a.chartsSections = []ytmusic.Section{
		{
			Title: "热门单曲榜",
			Items: []ytmusic.CardItem{
				{ID: "track1", Title: "晴天", Subtitle: "周杰伦", Type: "track", Track: &t1},
			},
		},
	}

	a.playlists["pl1"] = &ytmusic.Playlist{
		ID:          "pl1",
		Title:       "华语流行精选",
		Author:      "YouTube Music",
		Description: "精选华语金曲",
		TrackCount:  3,
		Tracks:      []ytmusic.Track{t1, t2, t3},
	}

	a.albums["al1"] = &ytmusic.Album{
		ID:      "al1",
		Title:   "叶惠美",
		Artists: []ytmusic.ArtistRef{{ID: "art1", Name: "周杰伦"}},
		Year:    "2003",
		Tracks:  []ytmusic.Track{t1, t2},
	}

	a.artists["art1"] = &ytmusic.Artist{
		ID:          "art1",
		Name:        "周杰伦",
		Subscribers: "1320万",
		TopTracks:   []ytmusic.Track{t1, t2, t3},
		Albums: []ytmusic.AlbumItem{
			{ID: "al1", Title: "叶惠美", Year: "2003", Type: "Album"},
		},
	}

	a.searches[route{page: pageSearch, query: "周杰伦", kind: ytmusic.SearchAll}] = &ytmusic.SearchResult{
		Tracks:  []ytmusic.Track{t1, t2},
		Artists: []ytmusic.ArtistRef{{ID: "art1", Name: "周杰伦"}},
		Albums:  []ytmusic.AlbumItem{{ID: "al1", Title: "叶惠美", Artist: "周杰伦", Year: "2003"}},
	}
	a.searches[route{page: pageSearch, query: "周杰伦", kind: ytmusic.SearchSongs}] = &ytmusic.SearchResult{
		Tracks: []ytmusic.Track{t1, t2},
	}

	return a
}
