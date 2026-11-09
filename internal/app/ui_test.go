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

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestNavGestureLeavesShelvesAlone(t *testing.T) {
	a := newTestApp(t)
	a.homeSections = []ytmusic.Section{{
		Title: "每日发现",
		Items: []ytmusic.CardItem{
			{ID: "a", Title: "A", Type: "track", Track: &ytmusic.Track{ID: "a", Title: "A"}},
			{ID: "b", Title: "B", Type: "track", Track: &ytmusic.Track{ID: "b", Title: "B"}},
		},
	}}
	a.exploreSections = []ytmusic.Section{{}}
	a.route = route{page: pageHome}
	a.history = []route{{page: pageExplore}}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	tt.Frame()
	if len(a.shelfRects) == 0 {
		t.Fatal("no shelf bounds were recorded")
	}
	r := a.shelfRects[0]
	tt.Scroll(r.X+10, r.Y+10, -80, 0)
	if a.route.page != pageHome {
		t.Fatal("a swipe over the shelf navigated")
	}

	tt.Scroll(20, 300, -80, 0)
	if a.route.page != pageExplore {
		t.Fatalf("a swipe outside the shelf did not go back: %v", a.route.page)
	}
}

func TestReconcileQueue(t *testing.T) {
	a := newTestApp(t)
	a.pl.SetQueue([]ytmusic.Track{
		{ID: "a", Title: "A", Artists: []ytmusic.ArtistRef{{Name: "X"}}},
		{ID: "dead", Title: "粉红色的回忆", Artists: []ytmusic.ArtistRef{{Name: "韩宝仪"}}},
		{ID: "b", Title: "B", Artists: []ytmusic.ArtistRef{{Name: "X"}}},
	}, 1)
	a.reconcileQueue(&ytmusic.Playlist{Tracks: []ytmusic.Track{
		{ID: "a", Title: "A", Artists: []ytmusic.ArtistRef{{Name: "X"}}},
		{ID: "b", Title: "B", Artists: []ytmusic.ArtistRef{{Name: "X"}}},
		{ID: "new", Title: "粉红色的回忆", Artists: []ytmusic.ArtistRef{{Name: "韩宝仪"}}},
	}})
	if got := a.pl.Queue()[1].ID; got != "new" {
		t.Fatalf("queue id = %s, want new", got)
	}
}

func TestNavScrollGesture(t *testing.T) {
	a := newTestApp(t)
	a.homeSections = []ytmusic.Section{{}}
	a.route = route{page: pageSearch, query: "x"}
	a.history = []route{{page: pageHome}}

	if a.navScroll(ui.InputEvent{Kind: ui.InputScroll, DX: -30, Precise: true}) {
		t.Fatal("a short swipe was taken")
	}
	if len(a.history) != 1 {
		t.Fatal("a short swipe navigated")
	}
	if !a.navScroll(ui.InputEvent{Kind: ui.InputScroll, DX: -40, Precise: true}) {
		t.Fatal("the swipe was not taken")
	}
	if a.route.page != pageHome || len(a.forwardHistory) != 1 {
		t.Fatalf("after back: route=%v forward=%d", a.route.page, len(a.forwardHistory))
	}

	a.navFired = false
	a.navDX = 0
	a.navAt = time.Time{}
	a.shelfRects = []ui.Rect{{X: 0, Y: 0, W: 200, H: 100}}
	if a.navScroll(ui.InputEvent{Kind: ui.InputScroll, X: 50, Y: 50, DX: -100, Precise: true}) {
		t.Fatal("a swipe over a shelf was taken")
	}
}

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

	// 2. Navigate to 媒体库 (Library)
	if err := tt.Click("媒体库"); err != nil {
		t.Fatalf("click 媒体库 failed: %v", err)
	}
	tt.Frame()
	if a.route.page != pageLibrary {
		t.Fatalf("expected pageLibrary, got %v", a.route.page)
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

func TestExploreShortcutOpensCharts(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageExplore}
	a.exploreSections = []ytmusic.Section{{
		Layout: "shortcuts",
		Items: []ytmusic.CardItem{
			{ID: "FEmusic_new_releases", Title: "新发行", Type: "mood", Icon: "MUSIC_NEW_RELEASE"},
			{ID: "FEmusic_charts", Title: "排行榜", Type: "mood", Icon: "TRENDING_UP"},
		},
	}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("新发行") || !tt.HasText("排行榜") {
		t.Fatal("shortcut cards did not render")
	}
	if err := tt.Click("排行榜"); err != nil {
		t.Fatalf("click 排行榜 shortcut: %v", err)
	}
	tt.Frame()
	if a.route.page != pageCharts {
		t.Fatalf("shortcut opened %+v, want pageCharts", a.route)
	}
}

func TestLibraryPageRendersPlaylists(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageLibrary}
	a.libraryPlaylists = []ytmusic.PlaylistItem{{
		ID:         "pl1",
		Title:      "华语流行精选",
		Author:     "leo",
		TrackCount: "3 首歌曲",
	}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, text := range []string{"媒体库", "播放列表", "赞过的音乐", "华语流行精选", "近期活动"} {
		if !tt.HasText(text) {
			t.Fatalf("library page missing %q", text)
		}
	}
	for _, text := range []string{"下载内容", "上传的内容"} {
		if tt.HasText(text) {
			t.Fatalf("library page still shows removed tab %q", text)
		}
	}

	if err := tt.Click("歌曲"); err != nil {
		t.Fatalf("click 歌曲 filter: %v", err)
	}
	tt.Frame()
	if a.libraryFilter != "songs" || !tt.HasText("暂无歌曲") {
		t.Fatalf("songs filter = %q, page = %v", a.libraryFilter, tt.HasText("暂无歌曲"))
	}
}

func TestLibraryFiltersUseLandingItems(t *testing.T) {
	a := newTestApp(t)
	a.route = route{page: pageLibrary}
	a.profile = &ytmusic.Profile{Name: "测试用户", Handle: "@testuser"}
	a.libraryItems = []ytmusic.CardItem{
		{ID: "VLLM", Title: "赞过的音乐", Subtitle: "自动播放列表", Type: "playlist"},
		{ID: "UCtestartist", Title: "测试音乐人", Subtitle: "100位订阅者", Type: "artist"},
		{ID: "MPSPPLtestpodcast", Title: "测试播客", Subtitle: "测试主播", Type: "podcast"},
		{ID: "VLPLtestplaylist", Title: "测试歌单", Subtitle: "10 首曲目", Type: "playlist"},
	}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, text := range []string{"全部", "测试音乐人", "测试播客", "测试歌单"} {
		if !tt.HasText(text) {
			t.Fatalf("library all filter missing %q", text)
		}
	}

	if err := tt.Click("音乐人"); err != nil {
		t.Fatalf("click 音乐人 filter: %v", err)
	}
	tt.Frame()
	if !tt.HasText("测试音乐人") || tt.HasText("测试播客") {
		t.Fatalf("artists filter did not isolate artist items")
	}

	if err := tt.Click("播客"); err != nil {
		t.Fatalf("click 播客 filter: %v", err)
	}
	tt.Frame()
	if !tt.HasText("测试播客") || tt.HasText("测试音乐人") {
		t.Fatalf("podcasts filter did not isolate podcast items")
	}

	if err := tt.Click("个人资料"); err != nil {
		t.Fatalf("click 个人资料 filter: %v", err)
	}
	tt.Frame()
	if a.libraryFilter != "profile" || tt.HasText("暂无个人资料") {
		t.Fatalf("profile filter = %q", a.libraryFilter)
	}

	if err := tt.Click("专辑"); err != nil {
		t.Fatalf("click 专辑 filter: %v", err)
	}
	tt.Frame()
	if !tt.HasText("暂无专辑") {
		t.Fatal("albums filter should show an empty state")
	}
}

func TestCategoryMoodButtonsOpenSubcategory(t *testing.T) {
	a := newTestApp(t)
	const rootID = "FEmusic_moods_and_genres"
	a.route = route{page: pageCategory, id: rootID}
	a.categories[categoryKey(rootID, "")] = &ytmusic.Category{
		ID:    rootID,
		Title: "心情与流派",
		Sections: []ytmusic.Section{{
			Title:  "心情和场景",
			Layout: "moods",
			Items: []ytmusic.CardItem{{
				ID:     "FEmusic_moods_and_genres_category",
				Title:  "悲伤",
				Type:   "mood",
				Params: "sad-params",
			}},
		}},
	}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("悲伤"); err != nil {
		t.Fatalf("click 悲伤: %v", err)
	}
	tt.Frame()
	if a.route.page != pageCategory || a.route.params != "sad-params" {
		t.Fatalf("route = %+v, want sad category", a.route)
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
	a.playlistRefreshing = map[string]bool{}
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
		Sections: []ytmusic.Section{{
			Title: "精选播放列表",
			Items: []ytmusic.CardItem{{
				ID:    "VLRDCLAK5uy_lMzHW51iFg1Kx0d_2EHpzbOgCrwtu8cgI",
				Title: "1980年代西洋金曲精选",
				Type:  "playlist",
			}},
		}},
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
	if !tt.HasText("精选播放列表") || !tt.HasText("1980年代西洋金曲精选") {
		t.Fatal("category sections did not render")
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
	a.playlists["pl1"].TrackCount = 4
	a.loadingMore["pl1"] = true
	a.route = route{page: pagePlaylist, id: "pl1"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.trackListState("playlist:pl1").ScrollToEnd()
	tt.Frame()
	tt.Frame()
	if !tt.HasText("正在加载更多...") {
		t.Fatal("playlist did not show the load-more footer")
	}
}

func TestPlaylistHidesFooterWhenComplete(t *testing.T) {
	a := newTestApp(t)
	a.playlists["pl1"].Continuation = "stale-token"
	a.playlists["pl1"].TrackCount = 3
	a.route = route{page: pagePlaylist, id: "pl1"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if tt.HasText("继续向下滚动加载更多") || tt.HasText("正在加载更多...") {
		t.Fatal("complete playlist still shows a load-more footer")
	}
}

func TestPlaylistHidesFooterWhenTrackCountUnknown(t *testing.T) {
	a := newTestApp(t)
	tracks := make([]ytmusic.Track, 120)
	for i := range tracks {
		tracks[i] = ytmusic.Track{ID: fmt.Sprintf("track-%d", i), Title: fmt.Sprintf("Song %d", i)}
	}
	a.playlists["pl1"].Tracks = tracks
	a.playlists["pl1"].TrackCount = 0
	a.playlists["pl1"].Continuation = "stale-token"
	a.route = route{page: pagePlaylist, id: "pl1"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if tt.HasText("继续向下滚动加载更多") || tt.HasText("正在加载更多...") {
		t.Fatal("playlist shows a footer while not loading")
	}
}

func TestLikedHidesFooterWhenComplete(t *testing.T) {
	a := newTestApp(t)
	a.liked = []ytmusic.Track{
		{ID: "liked-1", Title: "One"},
		{ID: "liked-2", Title: "Two"},
	}
	a.playlists["LM"] = &ytmusic.Playlist{
		ID:           "LM",
		Title:        "我喜欢的音乐",
		Tracks:       a.liked,
		TrackCount:   len(a.liked),
		Continuation: "stale-token",
	}
	a.route = route{page: pageLiked}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if tt.HasText("继续向下滚动加载更多") || tt.HasText("正在加载更多...") {
		t.Fatal("complete liked playlist still shows a load-more footer")
	}
}

func TestLargePlaylistUsesVirtualList(t *testing.T) {
	a := newTestApp(t)
	tracks := make([]ytmusic.Track, 500)
	for i := range tracks {
		tracks[i] = ytmusic.Track{ID: fmt.Sprintf("track-%d", i), Title: fmt.Sprintf("Song %d", i)}
	}
	a.playlists["big"] = &ytmusic.Playlist{ID: "big", Title: "Big", Tracks: tracks, TrackCount: len(tracks)}
	a.route = route{page: pagePlaylist, id: "big"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	first, last := a.trackListState("playlist:big").Visible()
	if first < 0 || last < 0 || last-first > 40 {
		t.Fatalf("visible rows = %d..%d, expected a small window", first, last)
	}
	if last >= len(tracks) {
		t.Fatalf("visible last row = %d, want below %d", last, len(tracks))
	}

	a.trackListState("playlist:big").ScrollTo(400, ui.Start)
	tt.Frame()
	tt.Frame()
	first, last = a.trackListState("playlist:big").Visible()
	if first > 400 || last < 400 {
		t.Fatalf("visible rows after scroll = %d..%d, want row 400", first, last)
	}
}

func TestSongSearchUsesVirtualList(t *testing.T) {
	a := newTestApp(t)
	tracks := make([]ytmusic.Track, 300)
	for i := range tracks {
		tracks[i] = ytmusic.Track{ID: fmt.Sprintf("search-%d", i), Title: fmt.Sprintf("Result %d", i)}
	}
	r := route{page: pageSearch, query: "test", kind: ytmusic.SearchSongs}
	a.route = r
	a.searches[r] = &ytmusic.SearchResult{Tracks: tracks}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	first, last := a.trackListState(fmt.Sprintf("search:test:%d", ytmusic.SearchSongs)).Visible()
	if first < 0 || last < 0 || last-first > 40 {
		t.Fatalf("visible search rows = %d..%d, expected a small window", first, last)
	}
	if !tt.HasText("歌曲") {
		t.Fatal("search list header was not rendered")
	}
}

func TestArtistPageUsesVirtualList(t *testing.T) {
	a := newTestApp(t)
	tracks := make([]ytmusic.Track, 120)
	for i := range tracks {
		tracks[i] = ytmusic.Track{ID: fmt.Sprintf("artist-%d", i), Title: fmt.Sprintf("Track %d", i)}
	}
	a.artists["art-big"] = &ytmusic.Artist{ID: "art-big", Name: "Artist", TopTracks: tracks}
	a.route = route{page: pageArtist, id: "art-big"}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	first, last := a.trackListState("artist:art-big").Visible()
	if first < 0 || last < 0 || last-first > 40 {
		t.Fatalf("visible artist rows = %d..%d, expected a small window", first, last)
	}
}

func TestArtistAllSongsOpensTheArtistSongList(t *testing.T) {
	a := newTestApp(t)
	a.artists["art-song"] = &ytmusic.Artist{
		ID:          "art-song",
		Name:        "古典樂精選",
		SongsID:     "VLOLAK5uy_songs",
		SongsParams: "ggMCCAI%3D",
		TopTracks:   []ytmusic.Track{{ID: "preview1", Title: "月光奏鳴曲"}},
	}
	a.playlists[artistSongsKey("art-song")] = &ytmusic.Playlist{
		ID: "VLOLAK5uy_songs",
		Tracks: []ytmusic.Track{
			{ID: "song1", Title: "月光奏鳴曲"},
			{ID: "song2", Title: "致愛麗絲"},
		},
	}
	a.route = route{page: pageArtist, id: "art-song"}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if err := tt.Click("查看所有歌曲"); err != nil {
		t.Fatalf("click 查看所有歌曲: %v", err)
	}
	tt.Frame()

	// The old behaviour searched YouTube Music for the artist name, which
	// mixed in other artists' tracks; the button must open the artist's own
	// song list instead.
	if a.route.page != pageArtistSongs || a.route.id != "art-song" {
		t.Fatalf("route = %+v, want the artist songs page", a.route)
	}
	for _, text := range []string{"古典樂精選", "共 2 首歌曲", "致愛麗絲"} {
		if !tt.HasText(text) {
			t.Fatalf("artist songs page missing %q", text)
		}
	}
}

func TestArtistWithoutSongsShelfHidesAllSongs(t *testing.T) {
	a := newTestApp(t)
	a.artists["art-plain"] = &ytmusic.Artist{ID: "art-plain", Name: "Artist"}
	a.route = route{page: pageArtist, id: "art-plain"}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if tt.HasText("查看所有歌曲") {
		t.Fatal("查看所有歌曲 shown without a song list to open")
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
	a.playlistRefreshing = map[string]bool{}
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
	a.profile = &ytmusic.Profile{Name: "测试用户"}
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

func TestAlbumHeaderArtistOpensArtist(t *testing.T) {
	a := newTestApp(t)
	a.albums["al-zhou"] = &ytmusic.Album{
		ID:      "al-zhou",
		Title:   "绿豆沙",
		Year:    "2026年",
		Artists: []ytmusic.ArtistRef{{ID: "UCzhou", Name: "周传雄"}},
		// A different name on the track keeps the byline the only 周传雄 on
		// the page, so the click can only hit the header.
		Tracks: []ytmusic.Track{{ID: "t1", Title: "绿豆沙", Artists: []ytmusic.ArtistRef{{ID: "UCother", Name: "别的歌手"}}}},
	}
	a.artists["UCzhou"] = &ytmusic.Artist{ID: "UCzhou", Name: "周传雄"}
	a.route = route{page: pageAlbum, id: "al-zhou"}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if err := tt.Click("周传雄"); err != nil {
		t.Fatalf("click header artist: %v", err)
	}
	tt.Frame()
	if a.route.page != pageArtist || a.route.id != "UCzhou" {
		t.Fatalf("header artist opened %+v, want artist UCzhou", a.route)
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

func TestLoggedOutLibraryShowsLikedCard(t *testing.T) {
	a := newTestApp(t)
	a.profile = nil
	a.libraryItems = nil
	a.libraryPlaylists = nil
	a.route = route{page: pageLibrary}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("赞过的音乐") {
		t.Fatal("logged-out library should still show the liked music card")
	}
	if err := tt.Click("赞过的音乐"); err != nil {
		t.Fatalf("click liked card: %v", err)
	}
	tt.Frame()
	if a.route.page != pageLiked {
		t.Fatalf("liked card opened %+v, want pageLiked", a.route)
	}
	if tt.HasText("正在加载歌单...") {
		t.Fatal("logged-out liked page should not wait for a network playlist")
	}
}

func TestSidebarHidesLikedShortcut(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, text := range []string{"我的音乐库", "我喜欢的音乐"} {
		if tt.HasText(text) {
			t.Fatalf("sidebar still shows removed shortcut %q", text)
		}
	}
}

func TestSearchPodcastResults(t *testing.T) {
	a := newTestApp(t)
	r := route{page: pageSearch, query: "test", kind: ytmusic.SearchPodcasts}
	a.route = r
	a.searches[r] = &ytmusic.SearchResult{Podcasts: []ytmusic.CardItem{{
		ID:        "MPSPPLtest",
		Title:     "测试播客",
		Subtitle:  "测试主播",
		Type:      "podcast",
		Thumbnail: "",
	}}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("播客") || !tt.HasText("测试播客") {
		t.Fatal("podcast search results did not render")
	}
	if err := tt.Click("测试播客"); err != nil {
		t.Fatalf("click podcast result: %v", err)
	}
	tt.Frame()
	if a.route.page != pagePlaylist || a.route.id != "MPSPPLtest" {
		t.Fatalf("podcast opened %+v", a.route)
	}
}

func TestSearchPodcastEpisodesUseTrackList(t *testing.T) {
	a := newTestApp(t)
	r := route{page: pageSearch, query: "test", kind: ytmusic.SearchPodcastEpisodes}
	a.route = r
	a.searches[r] = &ytmusic.SearchResult{Tracks: []ytmusic.Track{{
		ID: "episode1", Title: "测试分集", Video: true,
	}}}

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("分集") || !tt.HasText("测试分集") {
		t.Fatal("podcast episode results did not render")
	}
}

func TestNowPlayingArtistLinkOpensArtist(t *testing.T) {
	a := newTestApp(t)
	a.pl.SetQueue([]ytmusic.Track{{
		ID:      "track1",
		Title:   "尘缘",
		Artists: []ytmusic.ArtistRef{{ID: "UCartist", Name: "罗文"}},
	}}, 0)
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// The player bar behind the overlay also shows the artist, so target the
	// link of the now-playing panel.
	if err := tt.Click("正在播放 · 罗文"); err != nil {
		t.Fatalf("click artist link: %v", err)
	}
	tt.Frame()
	if a.nowPlayingOpen {
		t.Fatal("artist link should close the now playing overlay")
	}
	if a.route.page != pageArtist || a.route.id != "UCartist" {
		t.Fatalf("route = %+v, want artist UCartist", a.route)
	}
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

	if !tt.HasText("待播清单") {
		t.Fatal("expected the queue tab")
	}
	if err := tt.Click("待播清单"); err != nil {
		t.Fatalf("click queue tab failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("搁浅") {
		t.Fatal("expected the queue pane to show 搁浅")
	}

	if err := tt.Click("搁浅"); err != nil {
		t.Fatalf("click queue item failed: %v", err)
	}
	tt.Frame()

	if a.pl.Current() == nil || a.pl.Current().ID != "track3" {
		t.Fatalf("expected queue click to play track3, got %+v", a.pl.Current())
	}
	if !tt.HasText("晴天") || !tt.HasText("七里香") {
		t.Error("the queue pane should keep listing the whole queue")
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
		t.Fatal("related content should be hidden while another tab is active")
	}

	if err := tt.Click("相关推荐"); err != nil {
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

func TestNowPlayingCloseAnimates(t *testing.T) {
	a := newTestApp(t)
	a.pl.SetQueue([]ytmusic.Track{{ID: "track1", Title: "晴天", DurationMs: 269000}}, 0)
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("收起正在播放"); err != nil {
		t.Fatalf("click the close chevron: %v", err)
	}
	if a.nowPlayingOpen {
		t.Fatal("the close chevron should start the view closing")
	}

	// It stays drawn as it sinks away, and the page under it takes clicks.
	tt.Frame()
	if !tt.HasText("收起正在播放") {
		t.Fatal("the view should still draw while it closes")
	}
	if err := tt.Click("探索"); err != nil {
		t.Fatalf("click the page under the closing view: %v", err)
	}
	tt.Frame()
	if a.route.page != pageExplore {
		t.Fatalf("route = %v, want the page under the closing view", a.route.page)
	}

	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if tt.HasText("收起正在播放") {
		t.Fatal("the view should be gone once its animation ends")
	}
	if a.npOpen {
		t.Fatal("the view is still open after closing")
	}
}

func TestNowPlayingShowsMediaInfo(t *testing.T) {
	a := newTestApp(t)
	track := ytmusic.Track{
		ID:         "track1",
		Title:      "晴天",
		Artists:    []ytmusic.ArtistRef{{Name: "周杰伦"}},
		Album:      ytmusic.AlbumRef{Name: "叶惠美"},
		DurationMs: 269000,
	}
	a.pl.SetQueue([]ytmusic.Track{track}, 0)
	a.publishNowPlaying()

	info, ok := mygo.NowPlaying.Info()
	if !ok {
		t.Fatal("the system shows nothing for the track playing")
	}
	if info.Title != "晴天" || info.Artist != "周杰伦" || info.Album != "叶惠美" {
		t.Fatalf("info = %q / %q / %q", info.Title, info.Artist, info.Album)
	}
	if info.Duration != 269*time.Second {
		t.Fatalf("duration = %v, want 4m29s", info.Duration)
	}
	if info.State != mygo.PlaybackPaused {
		t.Fatalf("state = %v, want paused", info.State)
	}
	if info.Commands&mygo.MediaPlay == 0 || info.Commands&mygo.MediaNext == 0 || info.Commands&mygo.MediaSeek == 0 {
		t.Fatalf("commands = %v, want play, next and seek", info.Commands)
	}
}

func TestMediaKeysDriveThePlayer(t *testing.T) {
	a := newTestApp(t)
	t1 := ytmusic.Track{ID: "track1", Title: "晴天", DurationMs: 269000}
	t2 := ytmusic.Track{ID: "track2", Title: "七里香", DurationMs: 299000}
	a.pl.SetQueue([]ytmusic.Track{t1, t2}, 0)

	a.handleMediaCommand(mygo.MediaNext, 0)
	if cur := a.pl.Current(); cur == nil || cur.ID != "track2" {
		t.Fatalf("next played %+v, want track2", cur)
	}
	a.handleMediaCommand(mygo.MediaPrevious, 0)
	if cur := a.pl.Current(); cur == nil || cur.ID != "track1" {
		t.Fatalf("previous played %+v, want track1", cur)
	}
	a.handleMediaCommand(mygo.MediaSeek, 42*time.Second)
	if got := a.pl.Position(); got != 42*time.Second {
		t.Fatalf("seek moved to %v, want 42s", got)
	}
}

func TestPlayerBarQueueButtonOpensQueue(t *testing.T) {
	a := newTestApp(t)
	a.pl.SetQueue([]ytmusic.Track{{ID: "track1", Title: "晴天", DurationMs: 269000}}, 0)

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("打开播放队列"); err != nil {
		t.Fatalf("click queue button: %v", err)
	}
	tt.Frame()
	if !a.nowPlayingOpen || a.npPane != paneQueue {
		t.Fatalf("open = %v pane = %v, want the queue pane", a.nowPlayingOpen, a.npPane)
	}
}

func TestLyricsFollowPlayback(t *testing.T) {
	a := newTestApp(t)
	track := ytmusic.Track{ID: "track-lyrics", Title: "测试曲目", DurationMs: 300000}
	a.pl.SetQueue([]ytmusic.Track{track}, 0)
	a.pl.Seek(40 * time.Second)
	lines := make([]ytmusic.LyricLine, 40)
	for i := range lines {
		lines[i] = ytmusic.LyricLine{TimeMs: int64(i) * 2000, Text: fmt.Sprintf("第 %d 行歌词", i+1)}
	}
	a.lyrics = &ytmusic.Lyrics{Synced: true, Lines: lines}
	a.nowPlayingOpen = true

	tt := ui.NewTester(a.view, 1400, 900)
	for range 60 {
		tt.Frame()
	}
	if a.npLyricsManual {
		t.Fatal("following playback should not read as a manual scroll")
	}
	if a.npLyricScroll.Y <= 0 {
		t.Fatalf("lyrics did not follow the line sung: y=%v target=%v", a.npLyricScroll.Y, a.npTargetY)
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

	box, ok := tt.Find("歌词内容")
	if !ok {
		t.Fatal("lyrics pane not found")
	}
	if a.npLyricScroll.MaxY <= 0 {
		t.Fatal("lyrics did not overflow, nothing to scroll")
	}

	tt.Scroll(box.X+box.W/2, box.Y+box.H/2, 0, 160)
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
			prep: func(a *app) {
				a.open(route{page: pagePlaylist, id: "pl1"})
				a.playlistRefreshing = map[string]bool{}
			},
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
