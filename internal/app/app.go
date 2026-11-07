package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
)

type pageKind int

const (
	pageHome pageKind = iota
	pageExplore
	pageLibrary
	pageCharts
	pagePlaylist
	pageAlbum
	pageArtist
	pageArtistSongs
	pageSearch
	pageLiked
	pageCategory
	pageSection
)

// paneKind is a tab of the Now Playing view: the pane it shows beside the
// cover.
type paneKind int

const (
	paneLyrics paneKind = iota
	paneQueue
	paneRelated
)

// scrubState is the progress bar's own position: what it drags, and whether
// the pointer holds it, which outranks where playback is until it is let go.
type scrubState struct {
	v    float64
	held bool
	drag bool
}

type route struct {
	page   pageKind
	id     string
	params string
	query  string
	kind   ytmusic.SearchType
}

type app struct {
	mu       sync.Mutex
	win      *mygo.Window
	pl       *player.Player
	art      *artCache
	artPins  map[string]bool
	pal      palette
	settings settings
	update   updateStatus
	// updateOn mirrors settings.CheckUpdates for updateLoop, which reads it
	// from its own goroutine; updateWake pokes that loop when the switch
	// moves.
	updateOn       atomic.Bool
	updateWake     chan struct{}
	route          route
	history        []route
	forwardHistory []route

	// Profile & Auth
	profile          *ytmusic.Profile
	accountModalOpen bool
	loginWin         *mygo.Window
	loginSession     *mygo.Session
	authMu           sync.Mutex
	authAttempt      uint64
	authErr          string
	playbackErr      string

	// Persistence
	saveMu    sync.Mutex
	saveTimer *time.Timer

	// Application lifetime
	ctx    context.Context
	cancel context.CancelFunc
	pot    *potProvider

	// Settings modal
	settingsModalOpen bool
	settingsScroll    ui.ScrollState

	// Content data
	homeSections    []ytmusic.Section
	homeLoading     bool
	exploreSections []ytmusic.Section
	exploreLoading  bool
	chartsSections  []ytmusic.Section
	chartsLoading   bool
	chartCountries  []ytmusic.ChartCountry
	chartCountry    string
	chartsGen       int

	playlists     map[string]*ytmusic.Playlist
	albums        map[string]*ytmusic.Album
	artists       map[string]*ytmusic.Artist
	searches      map[route]*ytmusic.SearchResult
	categories    map[string]*ytmusic.Category
	more          map[string]ytmusic.Section
	scrolled      map[string]*ui.ScrollState
	trackLists    map[string]*ui.ListState
	moodPages     map[string]int
	loadingMore   map[string]bool
	contentScroll ui.ScrollState
	liked         []ytmusic.Track
	tray          *mygo.Tray
	trayErr       error
	quitting      bool

	libraryPlaylists []ytmusic.PlaylistItem
	libraryItems     []ytmusic.CardItem
	libraryLoading   bool
	libraryFilter    string

	// Search UI
	searchBox       ui.Handle
	searchQuery     string
	searchFilter    ytmusic.SearchType
	suggestions     []string
	showSuggestions bool

	// Now Playing UI
	nowPlayingOpen    bool
	lyrics            *ytmusic.Lyrics
	lyricsLoading     bool
	lyricsLastVideoID string
	npLyricScroll     ui.ScrollState
	npQueueScroll     ui.ScrollState
	npRelatedScroll   ui.ScrollState
	npLyricsManual    bool
	npPane            paneKind
	npOpen            bool
	npRest            bool
	npChangedAt       time.Time
	npQueueJump       bool
	npScrub           scrubState
	relatedVideoID    string
	relatedSections   []ytmusic.Section
	relatedLoading    bool
	relatedLoaded     bool
	npTargetY         float32
	npCurrentY        float32
	npButtonTheme     mygo.ThemeSource
	mediaTrackID      string
	mediaArtwork      string
	mediaDuration     time.Duration
	lastPlayedTrackID string
	memoryTimer       *time.Timer
	notifyTrackID     string
	notifySeq         int
	notifyMu          sync.Mutex
	notifyID          uint32
}

func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(fn)
}

// Run boots the application and launches the main window.
func Run() error {
	useUpdatePlugin()
	mygo.Bind(&Pot{client: &http.Client{Timeout: 30 * time.Second}})
	ctx, cancel := context.WithCancel(context.Background())
	a := &app{
		ctx:         ctx,
		cancel:      cancel,
		updateWake:  make(chan struct{}, 1),
		playlists:   map[string]*ytmusic.Playlist{},
		albums:      map[string]*ytmusic.Album{},
		artists:     map[string]*ytmusic.Artist{},
		searches:    map[route]*ytmusic.SearchResult{},
		categories:  map[string]*ytmusic.Category{},
		more:        map[string]ytmusic.Section{},
		scrolled:    map[string]*ui.ScrollState{},
		trackLists:  map[string]*ui.ListState{},
		moodPages:   map[string]int{},
		loadingMore: map[string]bool{},
		route:       route{page: pageHome},
	}

	mygo.App.WhenReady(func() {
		secureUserDataDir()
		// Drop cookies that older builds left in the persistent WebView
		// session; the app's own session lives in session.json.
		go func() {
			_ = mygo.DefaultSession().ClearData(mygo.SiteDataAll)
			secureUserDataDir()
		}()
		a.settings = loadSettings()
		a.applyLanguage()
		a.settings.applyAppearance()

		// Cache setup
		cacheDir, _ := mygo.App.Path(mygo.PathCache)
		if cacheDir == "" {
			cacheDir = "/tmp/melo-cache"
		} else {
			cacheDir = filepath.Join(cacheDir, "melo")
		}
		limit := cacheLimitBytes(a.settings.CacheMB)
		songCache := player.NewCache(cacheDir, limit)
		if limit == 0 {
			songCache.Clear()
		} else {
			songCache.Prune()
		}

		a.art = newArtCache(a.post, filepath.Join(cacheDir, "artwork"))
		a.pot = newPOTProvider()
		a.pot.start()
		ytmusic.DefaultClient.SetSabr(a.pot, filepath.Join(cacheDir, "sabr"))
		a.pl = player.New(a.post)
		a.pl.SetContext(a.ctx)
		a.pl.Cache = songCache
		a.pl.Changed = func() {
			a.post(func() {
				cur := a.pl.Current()
				a.maybeNotifyTrack(cur)
				a.syncNowPlayingLyrics()
				a.publishNowPlaying()
			})
			a.scheduleSavePlayerSession()
		}
		a.pl.Error = func(id string, err error) {
			log.Printf("playback %s: %v", id, err)
			if errors.Is(err, ytmusic.ErrUnavailable) {
				a.playbackErr = locales.PlaybackUnavailable()
			} else {
				a.playbackErr = locales.PlaybackFailed()
			}
		}
		a.pl.DurationResolved = func(id string, durationMs int64) {
			a.post(func() {
				a.applyResolvedDuration(id, durationMs)
			})
		}

		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "Melo",
			Width:                1200,
			Height:               800,
			MinWidth:             920,
			MinHeight:            620,
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       52,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #030303)",
			Content:              ui.View(a.view),
		})

		a.setMenu()
		a.setupUpdates()
		a.setupMediaKeys()
		a.setupTray()
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.settings.CloseToTray && a.tray != nil && !a.quitting {
				e.PreventDefault()
				a.win.Hide()
				a.releaseMemorySoon()
				return
			}
			if a.pot != nil {
				a.pot.close()
			}
		})
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
			a.quitting = true
		})
		mygo.App.OnActivate(func(hasVisibleWindows bool) {
			if a.win == nil || hasVisibleWindows {
				return
			}
			a.win.Show()
			a.win.Focus()
		})
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
			if a.cancel != nil {
				a.cancel()
			}
			if a.pl != nil {
				_ = a.pl.Close()
			}
			if a.tray != nil {
				a.tray.Destroy()
			}
			if a.pot != nil {
				a.pot.close()
			}
			mygo.NowPlaying.Clear()
			a.savePlayerSession()
			if a.pl != nil && a.pl.Cache != nil && a.pl.Cache.Limit() == 0 {
				a.pl.Cache.Clear()
			}
		})
		a.restorePlayerSession()
		a.boot()
		go a.maintenanceLoop(a.ctx)
		go a.updateLoop(a.ctx)
	})

	return mygo.App.Run()
}

func (a *app) boot() {
	a.homeLoading = true
	go func() {
		// Restore user session
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		sess, err := ytmusic.LoadSession()
		var prof *ytmusic.Profile
		if err == nil && sess != nil {
			if sess.Profile != nil {
				prof = sess.Profile
			} else {
				p, _ := ytmusic.DefaultClient.Account(ctx)
				prof = p
			}
		}

		// Load home sections
		home, _ := ytmusic.DefaultClient.BrowseHome(ctx)

		a.post(func() {
			a.profile = prof
			if a.profile != nil {
				a.restoreLibraryCache()
				a.loadLibrary()
			}
		})

		a.post(func() {
			a.homeSections = home
			a.homeLoading = false
		})
	}()
}

func (a *app) loadLibrary() {
	if a.profile == nil {
		return
	}
	a.libraryLoading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		playlists, _ := ytmusic.DefaultClient.BrowseLibraryPlaylists(ctx)
		items, _ := ytmusic.DefaultClient.BrowseLibrary(ctx)
		liked, _ := ytmusic.DefaultClient.BrowseLikedSongs(ctx)

		a.post(func() {
			a.libraryLoading = false
			updated := false
			if len(playlists) > 0 {
				a.libraryPlaylists = playlists
				updated = true
			}
			if len(items) > 0 {
				a.libraryItems = items
				updated = true
			}
			if liked != nil {
				a.playlists["LM"] = liked
				if len(liked.Tracks) > 0 {
					a.liked = append([]ytmusic.Track(nil), liked.Tracks...)
				}
				updated = true
			}
			if updated {
				a.saveLibraryCache()
			}
		})
	}()
}

func (a *app) open(r route) {
	if a.route == r {
		return
	}
	a.history = append(a.history, a.route)
	a.forwardHistory = nil
	a.route = r
	a.contentScroll.Y = 0
	a.contentScroll.MaxY = 0
	a.loadCurrentRoute()
	a.releaseMemorySoon()
}

func (a *app) back() {
	if len(a.history) == 0 {
		return
	}
	prev := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.forwardHistory = append(a.forwardHistory, a.route)
	a.route = prev
	a.contentScroll.Y = 0
	a.contentScroll.MaxY = 0
	a.loadCurrentRoute()
	a.releaseMemorySoon()
}

func (a *app) forward() {
	if len(a.forwardHistory) == 0 {
		return
	}
	next := a.forwardHistory[len(a.forwardHistory)-1]
	a.forwardHistory = a.forwardHistory[:len(a.forwardHistory)-1]
	a.history = append(a.history, a.route)
	a.route = next
	a.contentScroll.Y = 0
	a.contentScroll.MaxY = 0
	a.loadCurrentRoute()
	a.releaseMemorySoon()
}

func (a *app) loadCurrentRoute() {
	ctx := context.Background()
	switch a.route.page {
	case pageHome:
		if len(a.homeSections) == 0 && !a.homeLoading {
			a.homeLoading = true
			go func() {
				sec, _ := ytmusic.DefaultClient.BrowseHome(ctx)
				a.post(func() {
					a.homeSections = sec
					a.homeLoading = false
				})
			}()
		}
	case pageExplore:
		if len(a.exploreSections) == 0 && !a.exploreLoading {
			a.exploreLoading = true
			go func() {
				sec, _ := ytmusic.DefaultClient.BrowseExplore(ctx)
				a.post(func() {
					a.exploreSections = sec
					a.exploreLoading = false
				})
			}()
		}
	case pageCharts:
		if len(a.chartsSections) == 0 && !a.chartsLoading {
			a.chartsLoading = true
			go func() {
				page, _ := ytmusic.DefaultClient.BrowseChartsPage(ctx, "")
				a.post(func() {
					if page != nil {
						a.chartsSections = page.Sections
						a.chartCountries = page.Countries
						a.chartCountry = page.Selected
					}
					a.chartsLoading = false
				})
			}()
		}
	case pagePlaylist:
		id := a.route.id
		if isLikedPlaylistID(id) && a.profile == nil {
			return
		}
		if a.playlists[id] == nil {
			go func() {
				pl, _ := ytmusic.DefaultClient.BrowsePlaylist(ctx, id)
				a.post(func() {
					if pl != nil {
						a.playlists[id] = pl
					}
				})
			}()
		}
	case pageAlbum:
		id := a.route.id
		if a.albums[id] == nil {
			go func() {
				al, _ := ytmusic.DefaultClient.BrowseAlbum(ctx, id)
				a.post(func() {
					if al != nil {
						a.albums[id] = al
					}
				})
			}()
		}
	case pageArtist:
		id := a.route.id
		if a.artists[id] == nil {
			go func() {
				art, _ := ytmusic.DefaultClient.BrowseArtist(ctx, id)
				a.post(func() {
					if art != nil {
						a.artists[id] = art
					}
				})
			}()
		}
	case pageArtistSongs:
		id := a.route.id
		key := artistSongsKey(id)
		art := a.artists[id]
		if a.playlists[key] != nil || a.loadingMore[key] || art == nil || art.SongsID == "" {
			return
		}
		a.loadingMore[key] = true
		go func(browseID, params string) {
			songs, _ := ytmusic.DefaultClient.BrowseArtistSongs(ctx, browseID, params)
			a.post(func() {
				a.loadingMore[key] = false
				if songs == nil {
					songs = &ytmusic.Playlist{ID: browseID}
				}
				a.playlists[key] = songs
			})
		}(art.SongsID, art.SongsParams)
	case pageSearch:
		r := a.route
		if a.searches[r] == nil && r.query != "" {
			go func() {
				res, _ := ytmusic.DefaultClient.Search(ctx, r.query, r.kind)
				a.post(func() {
					if res != nil {
						a.searches[r] = res
					}
				})
			}()
		}
	case pageLiked:
		if (a.playlists["LM"] == nil || len(a.liked) == 0) && a.profile != nil {
			go func() {
				liked, _ := ytmusic.DefaultClient.BrowseLikedSongs(ctx)
				if liked != nil {
					a.post(func() {
						a.playlists["LM"] = liked
						if len(liked.Tracks) > 0 {
							a.liked = append([]ytmusic.Track(nil), liked.Tracks...)
						}
					})
				}
			}()
		}
	case pageCategory:
		id, params := a.route.id, a.route.params
		key := categoryKey(id, params)
		if a.categories[key] == nil {
			go func() {
				cat, _ := ytmusic.DefaultClient.BrowseCategory(ctx, id, params)
				a.post(func() {
					if cat != nil {
						a.categories[key] = cat
					}
				})
			}()
		}
	}
}

func categoryKey(id, params string) string {
	return id + "|" + params
}

func (a *app) search(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	a.searchQuery = query
	a.showSuggestions = false
	// Add to history
	if !slices.Contains(a.settings.SearchHistory, query) {
		a.settings.SearchHistory = append([]string{query}, a.settings.SearchHistory...)
		if len(a.settings.SearchHistory) > 10 {
			a.settings.SearchHistory = a.settings.SearchHistory[:10]
		}
		a.settings.save()
	}
	a.open(route{
		page:  pageSearch,
		query: query,
		kind:  a.searchFilter,
	})
}

func (a *app) syncNowPlayingLyrics() {
	cur := a.pl.Current()
	if cur == nil || cur.ID == a.lyricsLastVideoID {
		return
	}
	a.lyricsLastVideoID = cur.ID
	a.lyrics = nil
	a.lyricsLoading = true
	a.npLyricScroll = ui.ScrollState{}
	a.npQueueScroll = ui.ScrollState{}
	a.npLyricsManual = false
	a.npTargetY = 0
	a.npCurrentY = 0
	a.npRelatedScroll = ui.ScrollState{}
	a.npScrub = scrubState{}
	a.relatedVideoID = ""
	a.relatedSections = nil
	a.relatedLoading = false
	a.relatedLoaded = false
	if a.npPane == paneRelated && a.nowPlayingOpen {
		a.loadRelated()
	}

	go func(t ytmusic.Track) {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		setLyrics := func(l *ytmusic.Lyrics) {
			a.post(func() {
				if a.lyricsLastVideoID == t.ID {
					a.lyrics = l
					a.lyricsLoading = false
				}
			})
		}

		// 1. YouTube Android timed lyrics.
		var browseID string
		_, browseID, _ = ytmusic.DefaultClient.GetNext(ctx, t.ID, "")
		if browseID != "" {
			timed, err := ytmusic.DefaultClient.GetTimedLyrics(ctx, browseID)
			if err == nil && timed != nil && len(timed.Lines) > 0 {
				setLyrics(timed)
				return
			}
		}

		// 2. LRCLIB lyrics.
		var plain *ytmusic.Lyrics
		lrc, err := ytmusic.FetchLrcLibLyrics(ctx, t.Title, t.ArtistNames(), int(t.DurationMs/1000))
		if err == nil && lrc != nil && len(lrc.Lines) > 0 {
			if lrc.Synced {
				setLyrics(lrc)
				return
			}
			plain = lrc
		}

		// 3. YouTube Music plain lyrics.
		if browseID != "" {
			lyrics, err := ytmusic.DefaultClient.GetLyrics(ctx, browseID, t.DurationMs)
			if err == nil && lyrics != nil && len(lyrics.Lines) > 0 {
				setLyrics(lyrics)
				return
			}
		}

		// 4. LRCLIB plain lyrics.
		if plain != nil {
			setLyrics(plain)
			return
		}
		a.post(func() {
			if a.lyricsLastVideoID == t.ID {
				a.lyricsLoading = false
			}
		})
	}(*cur)
}

func (a *app) toggleLike(track ytmusic.Track) {
	for i, t := range a.liked {
		if t.ID == track.ID {
			a.liked = append(a.liked[:i], a.liked[i+1:]...)
			a.saveLibraryCache()
			if a.profile != nil {
				go func(id string) {
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					_ = ytmusic.DefaultClient.UnlikeSong(ctx, id)
				}(track.ID)
			}
			return
		}
	}
	track.Liked = true
	if track.DurationMs <= 0 {
		if cur := a.pl.Current(); cur != nil && cur.ID == track.ID {
			track.DurationMs = a.pl.Duration().Milliseconds()
		}
	}
	a.liked = append([]ytmusic.Track{track}, a.liked...)
	a.saveLibraryCache()
	if a.profile != nil {
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = ytmusic.DefaultClient.LikeSong(ctx, id)
		}(track.ID)
	}
}

// applyResolvedDuration fills a liked track's duration once playback resolves
// it, covering likes made before the stream finished loading.
func (a *app) applyResolvedDuration(id string, durationMs int64) {
	if durationMs <= 0 {
		return
	}
	changed := false
	for i := range a.liked {
		if a.liked[i].ID == id && a.liked[i].DurationMs <= 0 {
			a.liked[i].DurationMs = durationMs
			changed = true
		}
	}
	if changed {
		a.saveLibraryCache()
	}
}

// likedTracks backfills metadata on locally liked tracks from the last
// synced liked-songs playlist, then returns the shared liked list.
func (a *app) likedTracks() []ytmusic.Track {
	if pl := a.playlists["LM"]; pl != nil {
		byID := make(map[string]ytmusic.Track, len(pl.Tracks))
		for _, t := range pl.Tracks {
			byID[t.ID] = t
		}
		for i := range a.liked {
			cached, ok := byID[a.liked[i].ID]
			if !ok {
				continue
			}
			if a.liked[i].DurationMs <= 0 {
				a.liked[i].DurationMs = cached.DurationMs
			}
			if len(a.liked[i].Artists) == 0 {
				a.liked[i].Artists = cached.Artists
			}
			if a.liked[i].Thumbnail == "" {
				a.liked[i].Thumbnail = cached.Thumbnail
			}
		}
	}
	return a.liked
}

func (a *app) maybeLoadMorePlaylist(c *ui.Context) {
	key := a.route.id
	switch a.route.page {
	case pagePlaylist:
		if isLikedPlaylistID(key) {
			key = "LM"
		}
	case pageLiked:
		key = "LM"
	case pageArtistSongs:
		key = artistSongsKey(a.route.id)
	default:
		return
	}

	pl := a.playlists[key]
	if pl == nil || pl.Continuation == "" || a.loadingMore[key] {
		return
	}
	loaded := len(pl.Tracks)
	if key == "LM" {
		loaded = len(a.liked)
	}
	if pl.TrackCount > 0 && loaded >= pl.TrackCount {
		pl.Continuation = ""
		return
	}

	if a.pageUsesTrackList() {
		state := a.currentTrackListState()
		if state == nil {
			return
		}
		_, last := state.Visible()
		if last < 0 || last < loaded-2 {
			return
		}
	} else if a.contentScroll.MaxY <= 0 || a.contentScroll.Y < a.contentScroll.MaxY-300 {
		return
	}

	token := pl.Continuation
	a.loadingMore[key] = true
	c.Invalidate()

	go func() {
		tracks, next, err := ytmusic.DefaultClient.PlaylistTracks(context.Background(), token)
		a.post(func() {
			a.loadingMore[key] = false
			if err != nil {
				return
			}
			added := 0
			if p := a.playlists[key]; p != nil {
				before := len(p.Tracks)
				p.Tracks = appendUniqueTracks(p.Tracks, tracks)
				added = len(p.Tracks) - before
				p.Continuation = next
				if p.TrackCount < len(p.Tracks) {
					p.TrackCount = len(p.Tracks)
				}
				if added == 0 {
					p.Continuation = ""
				}
			}
			if key == "LM" {
				before := len(a.liked)
				a.liked = appendUniqueTracks(a.liked, tracks)
				if len(a.liked) > before {
					added++
				}
			}
			if added == 0 {
				if p := a.playlists[key]; p != nil {
					p.Continuation = ""
				}
			}
		})
	}()
}

func appendUniqueTracks(dst, src []ytmusic.Track) []ytmusic.Track {
	seen := make(map[string]bool, len(dst))
	for _, t := range dst {
		seen[t.ID] = true
	}
	for _, t := range src {
		if !seen[t.ID] {
			dst = append(dst, t)
			seen[t.ID] = true
		}
	}
	return dst
}

// playAllPlaylist starts playing the first page immediately and feeds
// further pages to the player as the queue nears its end.
func (a *app) playAllPlaylist(c *ui.Context, key string, tracks []ytmusic.Track) {
	pl := a.playlists[key]
	if pl == nil || len(tracks) == 0 {
		return
	}
	a.pl.SetQueue(tracks, 0)
	if pl.Continuation == "" {
		return
	}
	token := pl.Continuation
	a.pl.AttachFeed(func(got func([]ytmusic.Track)) {
		if token == "" {
			got(nil)
			return
		}
		more, next, err := ytmusic.DefaultClient.PlaylistTracks(context.Background(), token)
		if err != nil {
			got(nil)
			return
		}
		token = next
		got(more)
		a.post(func() {
			if p := a.playlists[key]; p != nil {
				p.Tracks = appendUniqueTracks(p.Tracks, more)
				p.Continuation = next
				if p.TrackCount < len(p.Tracks) {
					p.TrackCount = len(p.Tracks)
				}
			}
			if key == "LM" {
				a.liked = appendUniqueTracks(a.liked, more)
			}
		})
	})
}

// isLikedPlaylistID reports whether a playlist is the auto-generated liked
// songs list, which shares its state with a.liked.
func isLikedPlaylistID(id string) bool {
	return id == "LM" || id == "VLLM"
}

func (a *app) isLiked(videoID string) bool {
	for _, t := range a.liked {
		if t.ID == videoID {
			return true
		}
	}
	return false
}
