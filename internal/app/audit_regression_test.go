package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
	"github.com/egoist/mygo/ui"
)

type appRoundTripFunc func(*http.Request) (*http.Response, error)

func (f appRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonHTTPResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func makeTrackItemsJSON(prefix string, start, count int) string {
	items := make([]map[string]any, 0, count)
	for i := range count {
		idx := start + i
		items = append(items, map[string]any{
			"musicResponsiveListItemRenderer": map[string]any{
				"playlistItemData": map[string]any{
					"videoId": fmt.Sprintf("%s-%d", prefix, idx),
				},
				"flexColumns": []any{
					map[string]any{
						"musicResponsiveListItemFlexColumnRenderer": map[string]any{
							"text": map[string]any{
								"runs": []any{map[string]any{"text": fmt.Sprintf("%s Track %d", prefix, idx)}},
							},
						},
					},
					map[string]any{
						"musicResponsiveListItemFlexColumnRenderer": map[string]any{
							"text": map[string]any{
								"runs": []any{map[string]any{"text": "Artist"}},
							},
						},
					},
				},
			},
		})
	}
	b, _ := json.Marshal(items)
	return string(b)
}

func makePlaylistPageJSON(title, totalText, prefix string, start, count int, cont string) string {
	var subtitle string
	if totalText != "" {
		subtitle = fmt.Sprintf(`,"subtitle":{"runs":[{"text":"Playlist"},{"text":" • "},{"text":%q}]}`, totalText)
	}
	var contJSON string
	if cont != "" {
		contJSON = fmt.Sprintf(`,"continuations":[{"nextContinuationData":{"continuation":%q}}]`, cont)
	}
	return fmt.Sprintf(`{
		"header":{"musicResponsiveHeaderRenderer":{"title":{"runs":[{"text":%q}]}%s}},
		"contents":{
			"singleColumnBrowseResultsRenderer":{
				"tabs":[{
					"tabRenderer":{
						"content":{
							"sectionListRenderer":{
								"contents":[{
									"musicPlaylistShelfRenderer":{
										"contents":%s%s
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`, title, subtitle, makeTrackItemsJSON(prefix, start, count), contJSON)
}

func makeContinuationPageJSON(prefix string, start, count int, cont string) string {
	var contJSON string
	if cont != "" {
		contJSON = fmt.Sprintf(`,"continuations":[{"nextContinuationData":{"continuation":%q}}]`, cont)
	}
	return fmt.Sprintf(`{
		"continuationContents":{
			"musicPlaylistShelfContinuation":{
				"contents":%s%s
			}
		}
	}`, makeTrackItemsJSON(prefix, start, count), contJSON)
}

func makeAccountMenuJSON(name string) string {
	return fmt.Sprintf(`{
		"actions":[{
			"openPopupAction":{
				"popup":{
					"multiPageMenuRenderer":{
						"header":{
							"activeAccountHeaderRenderer":{
								"accountName":{"runs":[{"text":%q}]},
								"accountPhoto":{"thumbnails":[{"url":"https://img.example/avatar.jpg"}]}
							}
						}
					}
				}
			}
		}]
	}`, name)
}

type auditHarness struct {
	app     *app
	dataDir string
	uiQueue chan func()
}

func newAuditHarness(t *testing.T, rt http.RoundTripper) *auditHarness {
	t.Helper()
	dataDir := t.TempDir()
	restoreSess := ytmusic.SetTestSessionStore(dataDir)
	t.Cleanup(restoreSess)
	if rt != nil {
		restoreHTTP := ytmusic.DefaultClient.SetTestTransport(rt)
		t.Cleanup(restoreHTTP)
	}

	a := newTestAppAt(t, dataDir)
	// Clear pre-seeded mock fixtures unless a test explicitly populates them.
	a.playlists = map[string]*ytmusic.Playlist{}
	a.homeSections = nil
	a.exploreSections = nil
	a.chartsSections = nil
	a.ctx, a.cancel = context.WithCancel(t.Context())
	a.accountCtx, a.accountCancel = context.WithCancel(a.ctx)
	a.playlistGen = map[string]uint64{}
	a.playlistFilling = map[string]bool{}
	a.playlistRefreshing = map[string]bool{}

	uiQ := make(chan func(), 512)
	a.postFn = func(fn func()) {
		select {
		case uiQ <- fn:
		default:
			t.Errorf("auditHarness UI queue overflow")
		}
	}
	if a.pl != nil {
		a.pl.Close()
	}
	pl := player.New(func(fn func()) { a.post(fn) })
	pl.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
		return nil, errors.New("test offline")
	}
	t.Cleanup(func() { pl.Close() })
	a.pl = pl
	a.art = newArtCache(func(fn func()) { a.post(fn) }, t.TempDir())

	t.Cleanup(func() {
		a.advanceAccount()
		a.cancel()
	})
	return &auditHarness{app: a, dataDir: dataDir, uiQueue: uiQ}
}

func (h *auditHarness) drain() {
	for {
		select {
		case fn := <-h.uiQueue:
			if fn != nil {
				fn()
			}
		default:
			return
		}
	}
}

func (h *auditHarness) waitUntil(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.drain()
		if cond() {
			return
		}
		select {
		case fn := <-h.uiQueue:
			if fn != nil {
				fn()
			}
		case <-time.After(5 * time.Millisecond):
		}
	}
	h.drain()
	if !cond() {
		t.Fatalf("timed out waiting for %s", desc)
	}
}

func readJSONBody(r *http.Request) map[string]any {
	if r.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func TestAuditPlaylistAutoFillNoRaceAndConsistentTotals(t *testing.T) {
	// Subtest 1: Understated positive TrackCount keeps remaining continuation,
	// fills subsequent pages cleanly, settles TrackCount on completion, and
	// stays race-free under concurrent UI renders.
	t.Run("understated_positive_total_and_concurrent_renders", func(t *testing.T) {
		var h *auditHarness
		rt := appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "browse") {
				body := readJSONBody(req)
				switch body["continuation"] {
				case "cont-2":
					// Page 2 brings total tracks to 4 (>= understated TrackCount 3),
					// while continuation "cont-3" STILL remains!
					return jsonHTTPResponse(req, makeContinuationPageJSON("under", 3, 2, "cont-3")), nil
				case "cont-3":
					return jsonHTTPResponse(req, makeContinuationPageJSON("under", 5, 2, "")), nil
				default:
					return jsonHTTPResponse(req, makePlaylistPageJSON("Understated", "3 tracks", "under", 1, 2, "cont-2")), nil
				}
			}
			return jsonHTTPResponse(req, `{}`), nil
		})
		h = newAuditHarness(t, rt)
		a := h.app

		a.open(route{page: pagePlaylist, id: "pl-under"})

		// Render UI frames concurrently while draining UI updates under -race.
		tt := ui.NewTester(a.view, 1200, 800)
		h.waitUntil(t, 3*time.Second, "playlist auto-fill to finish", func() bool {
			tt.Frame()
			pl := a.playlists["pl-under"]
			return pl != nil && !a.playlistFilling["pl-under"] && !a.playlistRefreshing["pl-under"]
		})

		pl := a.playlists["pl-under"]
		if len(pl.Tracks) != 6 {
			t.Fatalf("len(Tracks) = %d, want 6", len(pl.Tracks))
		}
		if pl.TrackCount != 6 {
			t.Fatalf("TrackCount = %d, want 6 after completion", pl.TrackCount)
		}
		if pl.Continuation != "" {
			t.Fatalf("Continuation = %q, want empty after last page", pl.Continuation)
		}
	})

	// Subtest 2: Unknown total (TrackCount=0) interrupted/paused preserves 0
	// while continuation remains, and settles to actual count once completed.
	t.Run("understated_or_unknown_intermediate_state", func(t *testing.T) {
		pl := &ytmusic.Playlist{
			ID:           "pl-x",
			TrackCount:   3, // understated positive
			Tracks:       []ytmusic.Track{{ID: "t1"}, {ID: "t2"}},
			Continuation: "c2",
		}
		// Adding 2 tracks (loaded=4 >= 3) with remaining continuation "c3"
		// must NOT drop "c3"; it must mark total unknown (0) rather than capped 3.
		applyPlaylistPage(pl, []ytmusic.Track{{ID: "t3"}, {ID: "t4"}}, "c3")
		if pl.Continuation != "c3" {
			t.Fatalf("Continuation dropped: got %q, want c3", pl.Continuation)
		}
		if pl.TrackCount != 0 {
			t.Fatalf("TrackCount = %d, want 0 (unknown) when understated positive surpassed with more pages remaining", pl.TrackCount)
		}

		// Completing the remaining page settles TrackCount to exact loaded count (5).
		applyPlaylistPage(pl, []ytmusic.Track{{ID: "t5"}}, "")
		if pl.Continuation != "" {
			t.Fatalf("Continuation = %q, want empty", pl.Continuation)
		}
		if pl.TrackCount != 5 {
			t.Fatalf("TrackCount = %d, want 5 upon completion", pl.TrackCount)
		}
	})
}

func TestAuditSignOutClearsCompletePrivateLibrary(t *testing.T) {
	h := newAuditHarness(t, appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, `{}`), nil
	}))
	a := h.app

	if err := ytmusic.SaveSession(&ytmusic.SessionData{
		Cookie:  "SAPISID=priv; SID=priv",
		Profile: &ytmusic.Profile{Name: "Private User"},
	}); err != nil {
		t.Fatal(err)
	}
	ytmusic.DefaultClient.SetAuth("SAPISID=priv; SID=priv")
	a.profile = &ytmusic.Profile{Name: "Private User"}
	a.libraryPlaylists = []ytmusic.PlaylistItem{{ID: "VLpriv", Title: "My Secret Playlist"}}
	a.libraryItems = []ytmusic.CardItem{{ID: "priv-card", Title: "Private Album", Type: "album"}}
	a.liked = []ytmusic.Track{{ID: "priv-song", Title: "Private Liked Song"}}
	a.likedLoading = false
	a.playlists["LM"] = &ytmusic.Playlist{ID: "LM", Tracks: a.liked, TrackCount: 1}
	a.playlists["VLpriv"] = &ytmusic.Playlist{ID: "VLpriv", Title: "My Secret Playlist", Tracks: []ytmusic.Track{{ID: "p1", Title: "Secret Track"}}}
	a.relatedSections = []ytmusic.Section{{Title: "Private Related"}}
	a.saveLibraryCache()

	cachePath, err := userDataFile("library.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected library cache file before signOut: %v", err)
	}

	a.signOut()

	if a.profile != nil {
		t.Fatalf("profile not cleared: %+v", a.profile)
	}
	if len(a.libraryPlaylists) != 0 || len(a.libraryItems) != 0 || len(a.liked) != 0 || len(a.playlists) != 0 || len(a.relatedSections) != 0 {
		t.Fatalf("private state leaked after signOut: libPl=%d libItems=%d liked=%d pls=%d rel=%d",
			len(a.libraryPlaylists), len(a.libraryItems), len(a.liked), len(a.playlists), len(a.relatedSections))
	}
	if a.likedLoading {
		t.Fatal("likedLoading remained true after signOut")
	}
	if ytmusic.DefaultClient.Cookie() != "" {
		t.Fatalf("DefaultClient cookie leaked after signOut: %q", ytmusic.DefaultClient.Cookie())
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("library.json not deleted on signOut: %v", err)
	}

	a.route = route{page: pageLibrary}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if tt.HasText("My Secret Playlist") || tt.HasText("Private Album") {
		t.Fatal("library UI still displays private content after signOut")
	}
}

func TestAuditAccountIsolationAcrossBootRoutesLoginAndFeed(t *testing.T) {
	t.Run("late_boot_does_not_overwrite_new_login", func(t *testing.T) {
		bootHomeStarted := make(chan struct{})
		releaseBootHome := make(chan struct{})
		var mu sync.Mutex
		var accountMenus []string

		h := newAuditHarness(t, appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "account/account_menu"):
				mu.Lock()
				accountMenus = append(accountMenus, req.Header.Get("Cookie"))
				mu.Unlock()
				if strings.Contains(req.Header.Get("Cookie"), "SAPISID=new") {
					return jsonHTTPResponse(req, makeAccountMenuJSON("New User")), nil
				}
				return jsonHTTPResponse(req, makeAccountMenuJSON("Old User")), nil
			case strings.Contains(req.URL.Path, "browse"):
				select {
				case bootHomeStarted <- struct{}{}:
				default:
				}
				<-releaseBootHome
				return jsonHTTPResponse(req, `{"contents":{"singleColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[]}}}}]}}}`), nil
			default:
				return jsonHTTPResponse(req, `{}`), nil
			}
		}))
		a := h.app

		// Start signed-out boot and block inside BrowseHome.
		a.boot()
		<-bootHomeStarted

		// User logs in while old boot() is still in flight.
		attempt := a.beginAuthAttempt()
		a.saveCookieSession("SAPISID=new; SID=new", attempt)
		h.waitUntil(t, 2*time.Second, "new login profile commit", func() bool {
			return a.profile != nil && a.profile.Name == "New User"
		})

		// Now release the stale signed-out boot response.
		close(releaseBootHome)
		time.Sleep(30 * time.Millisecond)
		h.drain()

		if a.profile == nil || a.profile.Name != "New User" {
			t.Fatalf("late boot overwrote committed login profile: %+v", a.profile)
		}
		if !strings.Contains(ytmusic.DefaultClient.Cookie(), "SAPISID=new") {
			t.Fatalf("DefaultClient cookie lost after late boot: %q", ytmusic.DefaultClient.Cookie())
		}
	})

	t.Run("late_loaders_routes_and_feed_dropped_after_signout", func(t *testing.T) {
		loadersEntered := make(chan string, 16)
		releaseLoaders := make(chan struct{})

		h := newAuditHarness(t, appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "browse") {
				body := readJSONBody(req)
				bid, _ := body["browseId"].(string)
				cont, _ := body["continuation"].(string)
				key := bid
				if key == "" {
					key = cont
				}
				select {
				case loadersEntered <- key:
				default:
				}
				<-releaseLoaders
				switch key {
				case "FEmusic_library_landing":
					return jsonHTTPResponse(req, makePlaylistPageJSON("Lib", "", "lib", 1, 1, "")), nil
				case "FEmusic_liked_playlists":
					return jsonHTTPResponse(req, `{"contents":{}}`), nil
				case "VLLM", "LM":
					return jsonHTTPResponse(req, makePlaylistPageJSON("Liked", "", "liked", 1, 1, "")), nil
				case "priv-pl":
					return jsonHTTPResponse(req, makePlaylistPageJSON("Private PL", "", "privpl", 1, 1, "")), nil
				case "feed-tok":
					return jsonHTTPResponse(req, makeContinuationPageJSON("privfeed", 1, 1, "")), nil
				case "FEmusic_home":
					title := "Public Home Shelf"
					if req.Header.Get("Cookie") != "" {
						title = "Private Home Shelf"
					}
					return jsonHTTPResponse(req, fmt.Sprintf(`{
						"contents":{"singleColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{
							"musicCarouselShelfRenderer":{
								"header":{"musicCarouselShelfBasicHeaderRenderer":{"title":{"runs":[{"text":%q}]}}},
								"contents":[]
							}
						}]}}}}]}}
					}`, title)), nil
				}
			}
			return jsonHTTPResponse(req, `{}`), nil
		}))
		a := h.app

		a.profile = &ytmusic.Profile{Name: "Signed In"}
		ytmusic.DefaultClient.SetAuth("SAPISID=priv; SID=priv")
		_ = ytmusic.SaveSession(&ytmusic.SessionData{
			Cookie:  "SAPISID=priv; SID=priv",
			Profile: a.profile,
		})

		// Kick off library loader, liked loader, home route, private playlist route,
		// and playListAt auto-feed while signed in.
		a.loadLibrary()
		a.loadLiked()
		a.open(route{page: pagePlaylist, id: "priv-pl"})
		a.open(route{page: pageHome})
		a.playlists["priv-feed-pl"] = &ytmusic.Playlist{
			ID:           "priv-feed-pl",
			Tracks:       []ytmusic.Track{{ID: "seed-1", Title: "Seed"}},
			Continuation: "feed-tok",
		}
		a.playListAt("priv-feed-pl", a.playlists["priv-feed-pl"].Tracks, 0)

		// Wait until background requests are actually in flight on the server.
		seen := map[string]bool{}
		deadline := time.Now().Add(2 * time.Second)
		for (!seen["FEmusic_home"] || !seen["priv-pl"] || !seen["feed-tok"]) && time.Now().Before(deadline) {
			select {
			case k := <-loadersEntered:
				seen[k] = true
			case <-time.After(10 * time.Millisecond):
			}
		}

		// Sign out while all of them are in flight, then release server responses.
		a.signOut()
		close(releaseLoaders)
		time.Sleep(60 * time.Millisecond)
		h.drain()

		if len(a.liked) != 0 || len(a.libraryItems) != 0 || len(a.libraryPlaylists) != 0 {
			t.Fatalf("late loaders repopulated private library after signOut")
		}
		if a.playlists["priv-pl"] != nil || a.playlists["LM"] != nil {
			t.Fatalf("late route/liked populated private playlist after signOut: %+v", a.playlists)
		}
		for _, sec := range a.homeSections {
			if sec.Title == "Private Home Shelf" {
				t.Fatal("stale private home sections overwrote signed-out home")
			}
		}
		for _, qItem := range a.pl.Queue() {
			if strings.HasPrefix(qItem.ID, "privfeed") {
				t.Fatalf("stale feed appended private track %q to player queue after signOut", qItem.ID)
			}
		}
		if cachePath, err := userDataFile("library.json"); err == nil {
			if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
				t.Fatalf("stale loader recreated library.json after signOut")
			}
		}
	})
}

func TestAuditPotProviderCloseDoesNotDeadlockWithToken(t *testing.T) {
	p := newPOTProvider()

	// Closing must set closed state immediately and cause subsequent Token calls
	// to fail cleanly without touching GUI windows.
	p.close()
	if !p.isClosed() {
		t.Fatal("potProvider.isClosed() = false after close()")
	}
	if _, err := p.Token(t.Context(), "vis-1"); err == nil {
		t.Fatal("Token succeeded after potProvider.close()")
	}

	// Verify lock-order freedom: hold mintMu (as Token does while opening/minting);
	// idleClose releases p.mu before waiting on mintMu, so concurrent close()/armIdle()/
	// stopIdle() complete immediately without deadlocking against idleClose or mintMu.
	p2 := newPOTProvider()
	p2.mintMu.Lock()

	idleDone := make(chan struct{})
	go func() {
		p2.idleClose()
		close(idleDone)
	}()
	// Give idleClose a moment to acquire & release p.mu and block on mintMu.
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	var raced atomic.Int32
	for range 4 {
		wg.Go(func() {
			p2.armIdle()
			p2.stopIdle()
			p2.close()
			raced.Add(1)
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		p2.mintMu.Unlock()
		t.Fatal("potProvider.close deadlocked while mintMu was held and idleClose was waiting")
	}
	p2.mintMu.Unlock()
	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("idleClose did not finish after mintMu unlocked")
	}
	if raced.Load() != 4 || !p2.isClosed() {
		t.Fatalf("unexpected close state: raced=%d closed=%v", raced.Load(), p2.isClosed())
	}
}

func TestAuditLikedPaginationPreservesUnknownAndKnownTotalsAndScrollsPastPage2(t *testing.T) {
	// Part 1: Known positive total (e.g. 100) is preserved across save + restore.
	h1 := newAuditHarness(t, appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, `{}`), nil
	}))
	a1 := h1.app
	knownTracks := make([]ytmusic.Track, 50)
	for i := range knownTracks {
		knownTracks[i] = ytmusic.Track{ID: fmt.Sprintf("k-%d", i), Title: fmt.Sprintf("Known %d", i)}
	}
	a1.liked = knownTracks
	a1.likedLoading = false
	a1.playlists["LM"] = &ytmusic.Playlist{
		ID:           "LM",
		Title:        "Liked Music",
		Tracks:       knownTracks,
		TrackCount:   100,
		Continuation: "page-2",
	}
	a1.saveLibraryCache()

	a1Restored := newTestAppAt(t, h1.dataDir)
	a1Restored.restoreLibraryCache()
	if pl := a1Restored.playlists["LM"]; pl == nil || pl.TrackCount != 100 || pl.Continuation != "page-2" {
		t.Fatalf("restored known liked metadata = %+v, want TrackCount=100 Continuation=page-2", pl)
	}

	// Part 2: Unknown total (TrackCount=0 from BrowseLikedSongs with continuation)
	// survives BrowseLikedSongs -> saveLibraryCache -> restoreLibraryCache ->
	// UI scrolling page 2 ("cont-2" -> "cont-3") -> page 3 ("cont-3" -> "")!
	h := newAuditHarness(t, appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "browse") {
			body := readJSONBody(req)
			switch body["continuation"] {
			case "cont-2":
				return jsonHTTPResponse(req, makeContinuationPageJSON("liked", 21, 20, "cont-3")), nil
			case "cont-3":
				return jsonHTTPResponse(req, makeContinuationPageJSON("liked", 41, 10, "")), nil
			default:
				// Page 1 of Liked Music without total subtitle (unknown total = 0).
				return jsonHTTPResponse(req, makePlaylistPageJSON("Liked Music", "", "liked", 1, 20, "cont-2")), nil
			}
		}
		return jsonHTTPResponse(req, `{}`), nil
	}))
	a := h.app
	a.profile = &ytmusic.Profile{Name: "Test User"}

	a.loadLiked()
	h.waitUntil(t, 2*time.Second, "initial loadLiked", func() bool {
		return !a.likedLoading && len(a.liked) == 20
	})
	if pl := a.playlists["LM"]; pl == nil || pl.TrackCount != 0 || pl.Continuation != "cont-2" {
		t.Fatalf("after page 1 BrowseLikedSongs: pl=%+v, want TrackCount=0 Continuation=cont-2", pl)
	}

	a.saveLibraryCache()

	// Simulate app restart restoring the cache in the same dataDir.
	a2 := newTestAppAt(t, h.dataDir)
	a2.profile = a.profile
	a2.ctx, a2.cancel = a.ctx, a.cancel
	a2.accountCtx, a2.accountCancel = a.accountCtx, a.accountCancel
	a2.playlistGen = map[string]uint64{}
	a2.playlistFilling = map[string]bool{}
	a2.playlistRefreshing = map[string]bool{}
	a2.postFn = a.postFn
	a2.restoreLibraryCache()
	if pl := a2.playlists["LM"]; pl == nil || pl.TrackCount != 0 || pl.Continuation != "cont-2" {
		t.Fatalf("after restoreLibraryCache: pl=%+v, want TrackCount=0 Continuation=cont-2", pl)
	}

	// Render pageLiked and scroll to trigger page 2 ("cont-2").
	a2.route = route{page: pageLiked}
	tt := ui.NewTester(a2.view, 1200, 800)
	tt.Frame()
	a2.trackListState("liked").ScrollTo(19, ui.Start)
	tt.Frame()
	tt.Frame()

	h.waitUntil(t, 2*time.Second, "liked page 2 loaded", func() bool {
		tt.Frame()
		return len(a2.liked) == 40
	})
	if pl := a2.playlists["LM"]; pl == nil || pl.TrackCount != 0 || pl.Continuation != "cont-3" {
		t.Fatalf("after page 2: pl TrackCount=%d Continuation=%q, want 0 and cont-3", pl.TrackCount, pl.Continuation)
	}

	// Scroll to end of page 2 to trigger page 3 ("cont-3") -> completion!
	a2.trackListState("liked").ScrollTo(39, ui.Start)
	tt.Frame()
	tt.Frame()

	h.waitUntil(t, 2*time.Second, "liked page 3 loaded", func() bool {
		tt.Frame()
		return len(a2.liked) == 50
	})
	if pl := a2.playlists["LM"]; pl == nil || pl.TrackCount != 50 || pl.Continuation != "" {
		t.Fatalf("after final page 3: pl TrackCount=%d Continuation=%q, want 50 and empty", pl.TrackCount, pl.Continuation)
	}
	tt.Frame()
	if tt.HasText("继续向下滚动加载更多") || tt.HasText("正在加载更多...") {
		t.Fatal("completed liked playlist still shows load-more footer")
	}
}
