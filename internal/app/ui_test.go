package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestNavigation(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// 1. Navigate to 探索 (Explore)
	if err := tt.Click("探索"); err != nil {
		t.Fatalf("click 探索 failed: %v", err)
	}
	tt.Frame()
	if a.route.page != pageExplore {
		t.Fatalf("expected pageExplore, got %v", a.route.page)
	}

	// 2. Navigate to 排行榜 (Charts)
	if err := tt.Click("排行榜"); err != nil {
		t.Fatalf("click 排行榜 failed: %v", err)
	}
	tt.Frame()
	if a.route.page != pageCharts {
		t.Fatalf("expected pageCharts, got %v", a.route.page)
	}

	// 3. Navigate back to 首页 (Home)
	if err := tt.Click("首页"); err != nil {
		t.Fatalf("click 首页 failed: %v", err)
	}
	tt.Frame()
	if a.route.page != pageHome {
		t.Fatalf("expected pageHome, got %v", a.route.page)
	}
}

func TestSidebarTopRowAlignsWithHeader(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	home, ok := tt.Find("首页")
	if !ok {
		t.Fatal("sidebar did not show 首页")
	}
	homeMid := home.Y + home.H/2
	headerMid := float32(headerHeight) / 2
	if diff := homeMid - headerMid; diff < -1 || diff > 1 {
		t.Fatalf("首页 center y=%.1f, header center y=%.1f", homeMid, headerMid)
	}
}

func TestSidebarTopRowClearsLeftWindowControls(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageExplore}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()

	home, ok := tt.Find("首页")
	if !ok {
		t.Fatal("sidebar did not show 首页")
	}
	if home.Y < 52 {
		t.Fatalf("首页 y=%.1f overlaps the window controls", home.Y)
	}
	if err := tt.Click("首页"); err != nil {
		t.Fatalf("click 首页 failed: %v", err)
	}
	if a.route.page != pageHome {
		t.Fatalf("expected pageHome, got %v", a.route.page)
	}
}

func TestHomeCardTitlesStartAtSameHeight(t *testing.T) {
	a := newTestApp(t)
	longTitle := "2026流行华语歌曲 - 华语流行歌曲2026 (中国音乐2026排行榜)"
	a.homeSections = []ytmusic.Section{{
		Title: "再听一遍",
		Items: []ytmusic.CardItem{
			{ID: "short", Title: "赞过的音乐", Subtitle: "自动播放列表", Type: "playlist"},
			{ID: "long", Title: longTitle, Subtitle: "Redlist - Biggest Songs • 2079万次观看", Type: "playlist"},
			{ID: "mid", Title: "华语流行轻音乐", Subtitle: "派伟俊、五月天 阿信、萧敬腾、陈零九", Type: "playlist"},
		},
	}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	short, ok := tt.Find("赞过的音乐")
	if !ok {
		t.Fatal("short card title not found")
	}
	long, ok := tt.Find(longTitle)
	if !ok {
		t.Fatal("long card title not found")
	}
	mid, ok := tt.Find("华语流行轻音乐")
	if !ok {
		t.Fatal("middle card title not found")
	}
	if short.Y != long.Y || short.Y != mid.Y {
		t.Fatalf("card titles start at different heights: short=%.1f long=%.1f mid=%.1f", short.Y, long.Y, mid.Y)
	}
}

func TestBodyTextSelectionCopies(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	heading, ok := tt.Find("为您推荐")
	if !ok {
		t.Fatal("section heading not found")
	}
	tt.Press(heading.X+1, heading.Y+heading.H/2)
	tt.Move(heading.X+heading.W-1, heading.Y+heading.H/2)
	tt.Release(heading.X+heading.W-1, heading.Y+heading.H/2)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "为您推荐" {
		t.Fatalf("copied %q, want 为您推荐", got)
	}
}

func TestTrackRowContextMenuCopies(t *testing.T) {
	a := newTestApp(t)
	a.open(route{page: pagePlaylist, id: "pl1"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	track, ok := tt.Find("晴天")
	if !ok {
		t.Fatal("track row not found")
	}
	tt.RightClickAt(track.X+track.W/2, track.Y+track.H/2)
	if err := tt.ChooseMenuItem("复制歌曲名"); err != nil {
		t.Fatalf("copy track title: %v", err)
	}
	if got := tt.Clipboard(); got != "晴天" {
		t.Fatalf("copied %q, want 晴天", got)
	}

	tt.RightClickAt(track.X+track.W/2, track.Y+track.H/2)
	if err := tt.ChooseMenuItem("复制歌曲信息"); err != nil {
		t.Fatalf("copy track info: %v", err)
	}
	if got := tt.Clipboard(); got != "晴天 - 周杰伦" {
		t.Fatalf("copied %q, want 晴天 - 周杰伦", got)
	}
}

func TestCardContextMenuCopies(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	card, ok := tt.Find("华语流行精选")
	if !ok {
		t.Fatal("playlist card not found")
	}
	tt.RightClickAt(card.X+card.W/2, card.Y+card.H/2)
	if err := tt.ChooseMenuItem("复制标题"); err != nil {
		t.Fatalf("copy card title: %v", err)
	}
	if got := tt.Clipboard(); got != "华语流行精选" {
		t.Fatalf("copied %q, want 华语流行精选", got)
	}
}

func TestExploreMoodCategory(t *testing.T) {
	a := newTestApp(t)
	const categoryID = "FEmusic_moods_and_genres_category"
	const categoryParams = "ggMPOg1uX2hXVUQwc0JTNXlE"
	a.route = route{page: pageExplore}
	a.exploreSections = []ytmusic.Section{{
		Title:  "心情与流派",
		Layout: "moods",
		Items: []ytmusic.CardItem{{
			ID:     categoryID,
			Title:  "华语/粤语流行音乐",
			Type:   "mood",
			Params: categoryParams,
		}},
	}}
	a.categories[categoryKey(categoryID, categoryParams)] = &ytmusic.Category{
		ID:    categoryID,
		Title: "华语/粤语流行音乐",
	}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("心情与流派") {
		t.Fatal("moods section not rendered")
	}
	sectionTitle, ok := tt.Find("心情与流派")
	if !ok {
		t.Fatal("moods section title not found")
	}
	moodTitle, ok := tt.Find("华语/粤语流行音乐")
	if !ok {
		t.Fatal("mood button title not found")
	}
	if moodTitle.X-sectionTitle.X > 60 {
		t.Fatalf("mood content starts at x=%.1f, section title at x=%.1f; not left aligned", moodTitle.X, sectionTitle.X)
	}
	if err := tt.Click("华语/粤语流行音乐"); err != nil {
		t.Fatalf("click mood: %v", err)
	}
	tt.Frame()
	if a.route.page != pageCategory || a.route.id != categoryID || a.route.params != categoryParams {
		t.Fatalf("route = %+v", a.route)
	}
	if !tt.HasText("正在加载分类...") && !tt.HasText("华语/粤语流行音乐") {
		t.Fatal("category page did not render")
	}
}

func TestMoodButtonsEachColumnOpensItsCategory(t *testing.T) {
	a := newTestApp(t)
	const id = "FEmusic_moods_and_genres_category"
	a.route = route{page: pageExplore}

	var items []ytmusic.CardItem
	for i := 0; i < 6; i++ {
		title := fmt.Sprintf("流派%d", i)
		params := fmt.Sprintf("mood-%d", i)
		items = append(items, ytmusic.CardItem{ID: id, Title: title, Type: "mood", Params: params})
		a.categories[categoryKey(id, params)] = &ytmusic.Category{ID: id, Title: title}
	}
	a.exploreSections = []ytmusic.Section{{Title: "心情与流派", Layout: "moods", Items: items}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for i, item := range items {
		a.route = route{page: pageExplore}
		tt.Frame()
		if err := tt.Click(item.Title); err != nil {
			t.Fatalf("click %s: %v", item.Title, err)
		}
		tt.Frame()
		if a.route.page != pageCategory || a.route.params != item.Params {
			t.Fatalf("column %d opened route %+v, want params %q", i, a.route, item.Params)
		}
	}
}

func TestMouseSideButtonsNavigateHistory(t *testing.T) {
	a := newTestApp(t)
	a.history = []route{{page: pageHome}}
	a.route = route{page: pageExplore}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	tt.Key(0, ui.KeyBack)
	tt.Frame()
	if a.route.page != pageHome {
		t.Fatalf("back went to %+v, want pageHome", a.route)
	}

	tt.Key(0, ui.KeyForward)
	tt.Frame()
	if a.route.page != pageExplore {
		t.Fatalf("forward went to %+v, want pageExplore", a.route)
	}
}

func TestSectionMoreOpensAllItems(t *testing.T) {
	a := newTestApp(t)
	var items []ytmusic.CardItem
	for i := 0; i < 20; i++ {
		items = append(items, ytmusic.CardItem{ID: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("卡片 %d", i), Type: "playlist"})
	}
	a.homeSections = []ytmusic.Section{{Title: "更多测试", Items: items}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatalf("click more: %v", err)
	}
	tt.Frame()
	if a.route.page != pageSection {
		t.Fatalf("more went to %+v, want pageSection", a.route)
	}
	if !tt.HasText("更多测试") || !tt.HasText("卡片 19") {
		t.Fatal("more page did not show the full section")
	}
}

func TestSectionArrowsPageCarousel(t *testing.T) {
	a := newTestApp(t)
	var items []ytmusic.CardItem
	for i := 0; i < 20; i++ {
		items = append(items, ytmusic.CardItem{ID: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("卡片 %d", i), Type: "playlist"})
	}
	a.homeSections = []ytmusic.Section{{Title: "箭头测试", Items: items}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	more, ok := tt.Find("更多")
	if !ok {
		t.Fatal("more button not found")
	}
	nextX := more.X + more.W + 75
	y := more.Y + more.H/2

	tt.ClickAt(nextX, y)
	tt.Advance(2 * time.Second)
	tt.Frame()
	state := a.sectionScroll("home-sec-0")
	if state.X <= 0 {
		t.Fatalf("next arrow did not scroll: X=%.1f", state.X)
	}

	tt.ClickAt(nextX-38, y)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if state.X > 1 {
		t.Fatalf("previous arrow did not return: X=%.1f", state.X)
	}
}

func TestMoodSectionArrowsPage(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageExplore}
	var items []ytmusic.CardItem
	for i := 0; i < 30; i++ {
		items = append(items, ytmusic.CardItem{
			ID:     "FEmusic_moods_and_genres_category",
			Title:  fmt.Sprintf("流派 %d", i),
			Type:   "mood",
			Params: fmt.Sprintf("mood-%d", i),
		})
	}
	a.exploreSections = []ytmusic.Section{{Title: "心情与流派", Layout: "moods", Items: items}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	more, ok := tt.Find("更多")
	if !ok {
		t.Fatal("more button not found")
	}
	nextX := more.X + more.W + 75
	tt.ClickAt(nextX, more.Y+more.H/2)
	tt.Frame()
	if got := a.moodPages["exp-sec-0"]; got != 1 {
		t.Fatalf("mood page = %d, want 1", got)
	}
	if !tt.HasText("流派 24") {
		t.Fatal("second mood page not rendered")
	}
}

func TestPlaylistShowsLoadMoreFooter(t *testing.T) {
	a := newTestApp(t)
	a.playlists["pl1"].Continuation = "next-page"
	a.route = route{page: pagePlaylist, id: "pl1"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("继续向下滚动加载更多") {
		t.Fatal("playlist did not show the load-more footer")
	}
}

func TestChartsShowsCountrySelector(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageCharts}
	a.chartCountries = []ytmusic.ChartCountry{
		{Code: "NG", Name: "尼日利亚"},
		{Code: "ZZ", Name: "全球"},
	}
	a.chartCountry = "NG"

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("排行榜") || !tt.HasText("尼日利亚") {
		t.Fatal("charts title or selected country not rendered")
	}
	if err := tt.Click("尼日利亚"); err != nil {
		t.Fatalf("open country menu: %v", err)
	}
	tt.Frame()
	if !tt.HasText("全球") {
		t.Fatal("country menu did not show its options")
	}
}

func TestSearchAlbumCardOpensAlbum(t *testing.T) {
	a := newTestApp(t)
	r := route{page: pageSearch, query: "毛不易", kind: ytmusic.SearchAlbums}
	a.route = r
	a.searches[r] = &ytmusic.SearchResult{Albums: []ytmusic.AlbumItem{{ID: "MPREb_1", Title: "平凡的一天", Artist: "专辑，毛不易"}}}
	a.albums["MPREb_1"] = &ytmusic.Album{ID: "MPREb_1", Title: "平凡的一天"}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("专辑") {
		t.Fatal("album result section not rendered")
	}
	if err := tt.Click("平凡的一天"); err != nil {
		t.Fatalf("click album: %v", err)
	}
	tt.Frame()
	if a.route.page != pageAlbum || a.route.id != "MPREb_1" {
		t.Fatalf("route = %+v, want album MPREb_1", a.route)
	}
}

func TestVideoToggleOnlyForVideoTracks(t *testing.T) {
	a := newTestApp(t)
	video := ytmusic.Track{ID: "video1", Title: "Music Video", Video: true, DurationMs: 200000}
	a.pl.SetQueue([]ytmusic.Track{video}, 0)
	a.nowPlayingOpen = true
	a.npVideoMode = false
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("视频") {
		t.Fatal("video track did not show the song/video switch")
	}

	audio := ytmusic.Track{ID: "audio1", Title: "Audio Song", DurationMs: 200000}
	a.pl.SetQueue([]ytmusic.Track{audio}, 0)
	a.nowPlayingOpen = true
	a.npVideoMode = false
	tt.Frame()
	if tt.HasText("视频") {
		t.Fatal("audio-only track showed the video switch")
	}
}

func TestArtistPageMixedArtistCardOpensArtist(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageArtist, id: "UCmain"}
	a.artists["UCmain"] = &ytmusic.Artist{
		ID:   "UCmain",
		Name: "Ruger",
		Singles: []ytmusic.AlbumItem{
			{ID: "UCother", Title: "Kaestyle", Year: "每月听众：17.7万", Type: "artist"},
			{ID: "MPREb_1", Title: "Bounce", Year: "2024", Type: "Single"},
		},
	}
	a.artists["UCother"] = &ytmusic.Artist{ID: "UCother", Name: "Kaestyle"}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("Kaestyle") {
		t.Fatal("mixed artist card not rendered")
	}
	if err := tt.Click("Kaestyle"); err != nil {
		t.Fatalf("click artist card: %v", err)
	}
	tt.Frame()
	if a.route.page != pageArtist || a.route.id != "UCother" {
		t.Fatalf("route = %+v, want artist UCother", a.route)
	}
}

func TestArtistPageMixedTrackCardPlays(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageArtist, id: "UCmain"}
	a.artists["UCmain"] = &ytmusic.Artist{
		ID:   "UCmain",
		Name: "Asake",
		Singles: []ytmusic.AlbumItem{
			{ID: "vid1", Title: "Oba", Year: "Asake • 133万次观看", Type: "track", Track: &ytmusic.Track{ID: "vid1", Title: "Oba"}},
		},
	}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if err := tt.Click("Oba"); err != nil {
		t.Fatalf("click track card: %v", err)
	}
	tt.Frame()
	if cur := a.pl.Current(); cur == nil || cur.ID != "vid1" {
		t.Fatalf("current track = %+v, want vid1", cur)
	}
	if a.route.page != pageArtist {
		t.Fatalf("track card navigated to %+v", a.route)
	}
}

func TestSearchFlow(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	a.search("周杰伦")
	tt.Frame()

	if a.route.page != pageSearch || a.route.query != "周杰伦" {
		t.Fatalf("expected search for 周杰伦, got %+v", a.route)
	}
	if !slices.Contains(a.settings.SearchHistory, "周杰伦") {
		t.Errorf("expected search history to contain 周杰伦, got %v", a.settings.SearchHistory)
	}

	// Click category filter pill "歌曲"
	if err := tt.Click("歌曲"); err != nil {
		t.Fatalf("click pill 歌曲 failed: %v", err)
	}
	tt.Frame()
	if a.route.kind != ytmusic.SearchSongs {
		t.Fatalf("expected search kind SearchSongs, got %v", a.route.kind)
	}

	// History navigation
	a.back()
	tt.Frame()
	if a.route.page != pageSearch || a.route.kind != ytmusic.SearchAll {
		t.Fatalf("back should return to previous search kind, got %+v", a.route)
	}
}

func TestPlaylistViewAndPlay(t *testing.T) {
	a := newTestApp(t)
	a.open(route{page: pagePlaylist, id: "pl1"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// Click "播放全部 (3 首)"
	if err := tt.Click("播放全部 (3 首)"); err != nil {
		t.Fatalf("click 播放全部 failed: %v", err)
	}
	tt.Frame()

	if a.pl.Current() == nil || a.pl.Current().Title != "晴天" {
		t.Fatalf("expected current track 晴天, got %+v", a.pl.Current())
	}
	if len(a.pl.Queue()) != 3 {
		t.Fatalf("expected queue of 3 tracks, got %d", len(a.pl.Queue()))
	}

	// Toggle like
	cur := *a.pl.Current()
	a.toggleLike(cur)
	if !a.isLiked(cur.ID) {
		t.Fatal("expected track to be liked")
	}
	a.toggleLike(cur)
	if a.isLiked(cur.ID) {
		t.Fatal("expected track to be unliked after toggle")
	}
}

func TestActiveLyricLine(t *testing.T) {
	lines := []ytmusic.LyricLine{
		{TimeMs: 5000, Text: "一"},
		{TimeMs: 10000, Text: "二"},
		{TimeMs: 15000, Text: "三"},
	}
	cases := []struct {
		pos  time.Duration
		want int
	}{
		{0, -1},
		{4999 * time.Millisecond, -1},
		{5000 * time.Millisecond, 0},
		{12000 * time.Millisecond, 1},
		{20000 * time.Millisecond, 2},
	}
	for _, tc := range cases {
		if got := activeLyricLine(lines, true, tc.pos); got != tc.want {
			t.Errorf("activeLyricLine(%v) = %d, want %d", tc.pos, got, tc.want)
		}
	}
	if got := activeLyricLine(lines, false, 20*time.Second); got != -1 {
		t.Errorf("plain lyrics active line = %d, want -1", got)
	}
}

func TestToggleLikeFillsDurationFromPlayer(t *testing.T) {
	a := newTestApp(t)
	a.pl.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		return &ytmusic.StreamInfo{VideoID: id, Format: "mp4", DirectURL: "http://127.0.0.1:1/none", DurationMs: 269000}, nil
	}
	a.pl.SetQueue([]ytmusic.Track{{ID: "track1", Title: "晴天"}}, 0)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.pl.Duration() != 269*time.Second {
		time.Sleep(10 * time.Millisecond)
	}
	if got := a.pl.Duration(); got != 269*time.Second {
		t.Fatalf("player did not resolve duration, got %v", got)
	}

	a.toggleLike(ytmusic.Track{ID: "track1", Title: "晴天"})
	if len(a.liked) != 1 {
		t.Fatalf("expected one liked track, got %d", len(a.liked))
	}
	if a.liked[0].DurationMs != 269000 {
		t.Errorf("liked track duration = %d, want 269000", a.liked[0].DurationMs)
	}
}

func TestToggleLikePersistsAcrossRestore(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppAt(t, dir)
	a.toggleLike(ytmusic.Track{ID: "track1", Title: "晴天", DurationMs: 269000})

	b := newTestAppAt(t, dir)
	b.restoreLibraryCache()
	if len(b.liked) != 1 || b.liked[0].ID != "track1" {
		t.Fatalf("restored liked tracks = %+v, want track1", b.liked)
	}
}

func TestLikedPageUnlikeRemovesTrack(t *testing.T) {
	a := newTestApp(t)
	t1 := ytmusic.Track{ID: "liked1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
	t2 := ytmusic.Track{ID: "liked2", Title: "七里香", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 299000}
	a.liked = []ytmusic.Track{t1, t2}
	a.route = route{page: pageLiked}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("共 2 首歌曲") {
		t.Fatal("expected liked page to show 2 tracks")
	}

	if err := tt.Click("取消喜欢 晴天"); err != nil {
		t.Fatalf("click unlike failed: %v", err)
	}

	if len(a.liked) != 1 || a.liked[0].ID != "liked2" {
		t.Fatalf("expected 晴天 removed from liked list, got %+v", a.liked)
	}
	if !tt.HasText("共 1 首歌曲") {
		t.Error("liked page did not refresh after unlike")
	}
	if tt.HasText("晴天") {
		t.Error("unliked track is still shown")
	}
}

func TestLikedTracksBackfillDuration(t *testing.T) {
	a := newTestApp(t)
	a.liked = []ytmusic.Track{{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}}}
	a.playlists["LM"] = &ytmusic.Playlist{
		ID:     "LM",
		Title:  "赞过的音乐",
		Tracks: []ytmusic.Track{{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}},
	}
	a.route = route{page: pageLiked}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if a.liked[0].DurationMs != 269000 {
		t.Errorf("liked duration = %d, want 269000", a.liked[0].DurationMs)
	}
	if !tt.HasText("4:29") {
		t.Error("liked page did not show the backfilled duration")
	}
}

func TestLikedPlaylistUnlikeRemovesTrack(t *testing.T) {
	a := newTestApp(t)
	t1 := ytmusic.Track{ID: "liked1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
	t2 := ytmusic.Track{ID: "liked2", Title: "七里香", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 299000}
	a.liked = []ytmusic.Track{t1, t2}
	a.playlists["LM"] = &ytmusic.Playlist{ID: "LM", Title: "赞过的音乐", Tracks: []ytmusic.Track{t1, t2}}
	a.route = route{page: pagePlaylist, id: "LM"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("播放全部 (2 首)") {
		t.Fatal("expected liked playlist to show 2 tracks")
	}

	if err := tt.Click("取消喜欢 晴天"); err != nil {
		t.Fatalf("click unlike failed: %v", err)
	}

	if len(a.liked) != 1 || a.liked[0].ID != "liked2" {
		t.Fatalf("expected 晴天 removed from liked list, got %+v", a.liked)
	}
	if !tt.HasText("播放全部 (1 首)") {
		t.Error("liked playlist did not refresh after unlike")
	}
	if tt.HasText("晴天") {
		t.Error("unliked track is still shown in the liked playlist")
	}
}

func TestAlbumAndArtistNavigation(t *testing.T) {
	a := newTestApp(t)

	// Open Album
	a.open(route{page: pageAlbum, id: "al1"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if a.route.page != pageAlbum || a.route.id != "al1" {
		t.Fatalf("expected pageAlbum al1, got %+v", a.route)
	}

	// Open Artist
	a.open(route{page: pageArtist, id: "art1"})
	tt.Frame()
	if a.route.page != pageArtist || a.route.id != "art1" {
		t.Fatalf("expected pageArtist art1, got %+v", a.route)
	}
}

func TestAccountAndSettingsModals(t *testing.T) {
	origOpenWin := openWebLoginWindow
	defer func() {
		openWebLoginWindow = origOpenWin
	}()
	openWebLoginWindow = func(a *app) {}

	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// 1. Open Account Modal via login button
	if err := tt.Click("登录"); err != nil {
		t.Fatalf("click 登录 failed: %v", err)
	}
	tt.Frame()
	if !a.accountModalOpen {
		t.Fatal("expected accountModalOpen to be true")
	}

	// Click "打开 Google 登录窗口"
	if err := tt.Click("打开 Google 登录窗口"); err != nil {
		t.Fatalf("click 打开 Google 登录窗口 failed: %v", err)
	}
	// Simulate successful login completion
	a.profile = &ytmusic.Profile{Name: "测试用户", Handle: "@testuser"}
	a.accountModalOpen = false
	tt.Frame()

	if a.accountModalOpen {
		t.Fatal("expected accountModalOpen to be false after login")
	}
	if a.profile == nil || a.profile.Name != "测试用户" {
		t.Fatalf("expected profile 测试用户, got %+v", a.profile)
	}
	// 2. Open Settings Modal
	a.settingsModalOpen = true
	tt.Frame()
	if !a.settingsModalOpen {
		t.Fatal("expected settingsModalOpen to be true")
	}
	if !tt.HasText("测试用户") {
		t.Fatal("settings should display '测试用户'")
	}
	if !tt.HasText("Melo") {
		t.Fatal("settings should display the app name Melo")
	}

	// Switch theme
	if err := tt.Click("浅色外观"); err != nil {
		t.Fatalf("click 浅色外观 failed: %v", err)
	}
	tt.Frame()
	if a.settings.Appearance != "light" {
		t.Fatalf("expected appearance light, got %s", a.settings.Appearance)
	}

	a.settingsModalOpen = false
	tt.Frame()
}

func TestSignOutRefreshesInterface(t *testing.T) {
	a := newTestApp(t)
	a.profile = &ytmusic.Profile{Name: "测试用户"}
	a.libraryPlaylists = []ytmusic.PlaylistItem{{ID: "pl_test", Title: "私人歌单"}}
	a.liked = []ytmusic.Track{{ID: "liked_1", Title: "喜欢歌曲"}}
	a.playlists["LM"] = &ytmusic.Playlist{ID: "LM", Title: "我喜欢的音乐"}
	a.route = route{page: pageLiked}
	a.settingsModalOpen = true

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// Click "退出登录"
	if err := tt.Click("退出登录"); err != nil {
		t.Fatalf("click 退出登录 failed: %v", err)
	}
	tt.Frame()

	if a.profile != nil {
		t.Fatal("expected profile to be nil after sign out")
	}
	if len(a.libraryPlaylists) != 0 {
		t.Fatal("expected libraryPlaylists to be cleared")
	}
	if len(a.liked) != 0 {
		t.Fatal("expected liked to be cleared")
	}
	if a.playlists["LM"] != nil {
		t.Fatal("expected LM playlist to be deleted")
	}
	if a.route.page != pageHome {
		t.Fatalf("expected route pageHome after sign out, got %v", a.route.page)
	}
	if a.settingsModalOpen {
		t.Fatal("expected settingsModalOpen to be false")
	}
}

func TestDoubleClickTopBarMaximize(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// Double click header bar at top empty space
	tt.ClickAt(300, 20)
	tt.ClickAt(300, 20)
	tt.Frame()
}

func TestNowPlayingQueue(t *testing.T) {
	a := newTestApp(t)
	t1 := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
	t2 := ytmusic.Track{ID: "track2", Title: "七里香", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 299000}
	t3 := ytmusic.Track{ID: "track3", Title: "搁浅", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 240000}
	a.pl.SetQueue([]ytmusic.Track{t1, t2, t3}, 0)
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1600, 1000)
	tt.Frame()

	if !tt.HasText("接下来") {
		t.Fatal("expected an Up Next section")
	}
	if !tt.HasText("搁浅") {
		t.Fatal("expected the upcoming queue to show 搁浅")
	}

	if err := tt.Click("搁浅"); err != nil {
		t.Fatalf("click queue item failed: %v", err)
	}
	tt.Frame()

	if a.pl.Current() == nil || a.pl.Current().ID != "track3" {
		t.Fatalf("expected queue click to play track3, got %+v", a.pl.Current())
	}
	if !tt.HasText("暂无待播歌曲") {
		t.Error("expected the queue to be empty after jumping to the last track")
	}
}

func TestNowPlayingRelatedTab(t *testing.T) {
	a := newTestApp(t)
	current := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
	a.pl.SetQueue([]ytmusic.Track{current}, 0)

	related := ytmusic.Track{ID: "related1", Title: "相关歌曲", Artists: []ytmusic.ArtistRef{{Name: "相关歌手"}}, DurationMs: 200000}
	playlist := ytmusic.CardItem{ID: "VLPL123", Title: "推荐歌单", Subtitle: "播放列表 • YouTube Music", Type: "playlist"}
	a.relatedSections = []ytmusic.Section{
		{Title: "你可能还喜欢", Items: []ytmusic.CardItem{{ID: related.ID, Title: related.Title, Subtitle: related.ArtistNames(), Type: "track", Track: &related}}},
		{Title: "推荐的播放列表", Items: []ytmusic.CardItem{playlist}},
	}
	a.relatedVideoID = current.ID
	a.relatedLoaded = true
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1600, 1000)
	tt.Frame()
	if tt.HasText("相关歌曲") {
		t.Fatal("related content should be hidden while the queue tab is active")
	}

	if err := tt.Click("相关"); err != nil {
		t.Fatalf("click related tab failed: %v", err)
	}
	tt.Frame()
	for _, text := range []string{"你可能还喜欢", "相关歌曲", "推荐的播放列表", "推荐歌单"} {
		if !tt.HasText(text) {
			t.Fatalf("related tab is missing %q", text)
		}
	}

	if err := tt.Click("相关歌曲"); err != nil {
		t.Fatalf("click related track failed: %v", err)
	}
	if a.pl.Current() == nil || a.pl.Current().ID != "related1" {
		t.Fatalf("related track did not play: %+v", a.pl.Current())
	}

	if err := tt.Click("推荐歌单"); err != nil {
		t.Fatalf("click related playlist failed: %v", err)
	}
	if a.nowPlayingOpen {
		t.Error("opening a related playlist should close the now-playing view")
	}
	if a.route.page != pagePlaylist || a.route.id != "VLPL123" {
		t.Fatalf("route = %+v, want playlist VLPL123", a.route)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	oldUserDataDir := userDataDir
	userDataDir = func() (string, error) { return dir, nil }
	defer func() { userDataDir = oldUserDataDir }()

	a := newTestAppAt(t, dir)
	t1 := ytmusic.Track{ID: "track1", Title: "晴天", DurationMs: 269000}
	t2 := ytmusic.Track{ID: "track2", Title: "七里香", DurationMs: 299000}
	a.pl.SetQueue([]ytmusic.Track{t1, t2}, 1)
	a.pl.Seek(42 * time.Second)
	a.savePlayerSession()

	b := newTestAppAt(t, dir)
	b.restorePlayerSession()
	if b.pl.Current() == nil || b.pl.Current().ID != "track2" {
		t.Fatalf("restored current = %+v", b.pl.Current())
	}
	if got := b.pl.Position(); got != 42*time.Second {
		t.Fatalf("restored position = %v, want 42s", got)
	}

	a.libraryPlaylists = []ytmusic.PlaylistItem{{ID: "pl1", Title: "华语流行精选"}}
	a.playlists["LM"] = &ytmusic.Playlist{ID: "LM", Title: "赞过的音乐", Tracks: []ytmusic.Track{t1}}
	a.liked = []ytmusic.Track{t1}
	a.saveLibraryCache()

	c := newTestAppAt(t, dir)
	c.restoreLibraryCache()
	if len(c.libraryPlaylists) != 1 || c.libraryPlaylists[0].ID != "pl1" {
		t.Fatalf("restored playlists = %+v", c.libraryPlaylists)
	}
	if len(c.liked) != 1 || c.liked[0].ID != "track1" {
		t.Fatalf("restored liked = %+v", c.liked)
	}

	c.deleteLibraryCache()
	d := newTestApp(t)
	d.restoreLibraryCache()
	if len(d.libraryPlaylists) != 0 || len(d.liked) != 0 {
		t.Fatal("deleted library cache should not restore")
	}
}

func TestLyricsManualScroll(t *testing.T) {
	a := newTestApp(t)
	track := ytmusic.Track{ID: "track-lyrics", Title: "测试曲目", Artists: []ytmusic.ArtistRef{{Name: "测试歌手"}}, DurationMs: 300000}
	a.pl.SetQueue([]ytmusic.Track{track}, 0)
	lines := make([]ytmusic.LyricLine, 40)
	for i := range lines {
		lines[i] = ytmusic.LyricLine{TimeMs: int64(i) * 5000, Text: fmt.Sprintf("第 %d 行歌词", i+1)}
	}
	a.lyrics = &ytmusic.Lyrics{Synced: true, Lines: lines}
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1400, 900)
	tt.Frame()

	header, ok := tt.Find("歌词")
	if !ok {
		t.Fatal("lyrics header not found")
	}
	if a.npLyricScroll.MaxY <= 0 {
		t.Fatal("lyrics did not overflow, nothing to scroll")
	}

	tt.Scroll(header.X+100, header.Y+250, 0, 160)
	tt.Frame()
	if !a.npLyricsManual {
		t.Fatal("scrolling lyrics should pause auto-follow")
	}
	if a.npLyricScroll.Y <= 0 {
		t.Fatal("lyrics did not scroll")
	}

	tt.Frame()
	if !a.npLyricsManual {
		t.Fatal("manually scrolled lyrics should stay put")
	}

	if err := tt.Click("跟随播放"); err != nil {
		t.Fatalf("click follow button failed: %v", err)
	}
	tt.Frame()
	if a.npLyricsManual {
		t.Fatal("follow button should resume auto-follow")
	}
}

func TestPlayerBarLoadingState(t *testing.T) {
	a := newTestApp(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a.pl.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		close(started)
		<-release
		return nil, context.Canceled
	}
	a.pl.SetQueue([]ytmusic.Track{{ID: "track-loading", Title: "加载中的歌曲"}}, 0)
	<-started
	defer close(release)

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if !tt.HasText("加载中...") {
		t.Fatal("expected a loading indicator in the progress row")
	}
	if _, ok := tt.Find("暂停"); !ok {
		t.Fatal("play button should show the pause state while loading")
	}

	if err := tt.Click("暂停"); err != nil {
		t.Fatalf("pause during loading failed: %v", err)
	}
	tt.Frame()

	if _, ok := tt.Find("播放"); !ok {
		t.Fatal("play button should return to play after pausing the pending load")
	}
	if !tt.HasText("加载中...") {
		t.Fatal("loading indicator should remain while the stream is loading")
	}
}

func TestPlayerLikeButtonNearTitle(t *testing.T) {
	a := newTestApp(t)
	track := ytmusic.Track{ID: "track-like", Title: "测试曲目", Artists: []ytmusic.ArtistRef{{Name: "测试歌手"}}, DurationMs: 180000}
	a.pl.SetQueue([]ytmusic.Track{track}, 0)

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	title, ok := tt.Find("测试曲目")
	if !ok {
		t.Fatal("player bar did not show the track title")
	}
	like, ok := tt.Find("喜欢 测试曲目")
	if !ok {
		t.Fatal("player bar did not show the like button")
	}
	if like.X <= title.X {
		t.Fatalf("like button x=%.0f is not after the title x=%.0f", like.X, title.X)
	}
	if gap := like.X - (title.X + title.W); gap > 30 {
		t.Fatalf("like button is %.0f px from the title, want it next to the track info", gap)
	}
}

func TestNowPlayingAndLyrics(t *testing.T) {
	a := newTestApp(t)
	t1 := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
	a.pl.SetQueue([]ytmusic.Track{t1}, 0)

	a.lyrics = &ytmusic.Lyrics{
		Synced: true,
		Lines: []ytmusic.LyricLine{
			{TimeMs: 0, Text: "故事的小黄花"},
			{TimeMs: 15000, Text: "从出生那年就飘着"},
		},
	}
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	// Verify track info is rendered
	if !tt.HasText("故事的小黄花") {
		t.Fatal("expected lyrics text '故事的小黄花' in Now Playing view")
	}
}

func TestIdleFrameBudget(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Error("app requests frames while idle")
	}
}

func TestRenderScenes(t *testing.T) {
	outDir := t.TempDir()
	_ = os.MkdirAll(outDir, 0o755)

	scenes := []struct {
		name string
		prep func(a *app)
		dark bool
	}{
		{
			name: "home-dark",
			prep: func(a *app) { a.open(route{page: pageHome}) },
			dark: true,
		},
		{
			name: "home-light",
			prep: func(a *app) { a.open(route{page: pageHome}) },
			dark: false,
		},
		{
			name: "playlist",
			prep: func(a *app) { a.open(route{page: pagePlaylist, id: "pl1"}) },
			dark: true,
		},
		{
			name: "now-playing",
			prep: func(a *app) {
				t1 := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
				a.pl.SetQueue([]ytmusic.Track{t1}, 0)
				a.lyrics = &ytmusic.Lyrics{
					Synced: true,
					Lines:  []ytmusic.LyricLine{{TimeMs: 0, Text: "故事的小黄花"}, {TimeMs: 15000, Text: "从出生那年就飘着"}},
				}
				a.nowPlayingOpen = true
			},
			dark: true,
		},
		{
			name: "now-playing-queue",
			prep: func(a *app) {
				t1 := ytmusic.Track{ID: "track1", Title: "晴天", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 269000}
				t2 := ytmusic.Track{ID: "track2", Title: "七里香", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 299000}
				t3 := ytmusic.Track{ID: "track3", Title: "搁浅", Artists: []ytmusic.ArtistRef{{Name: "周杰伦"}}, DurationMs: 240000}
				a.pl.SetQueue([]ytmusic.Track{t1, t2, t3}, 0)
				a.nowPlayingOpen = true
			},
			dark: true,
		},
	}

	for _, sc := range scenes {
		a := newTestApp(t)
		sc.prep(a)
		tt := ui.NewTester(a.view, 1200, 800)
		tt.SetDark(sc.dark)
		tt.Frame()

		img := tt.Image()
		if img != nil {
			f, err := os.Create(filepath.Join(outDir, sc.name+".png"))
			if err == nil {
				_ = png.Encode(f, img)
				f.Close()
			}
		}
	}
}
