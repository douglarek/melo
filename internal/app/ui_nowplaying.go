package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
)

// The Now Playing view fills the window with what plays: its cover over a
// backdrop of the cover's own colors, the controls under it, and the pane
// its tabs choose beside it.

const (
	// npMaxWidth is how wide the cover and the panes together go, at most.
	npMaxWidth = 1360
	// npTopBarHeight is the draggable strip along the top, holding the
	// chevron that closes the view.
	npTopBarHeight = 44
	// npTabsHeight is how tall the row of tabs is.
	npTabsHeight = 40
	// npSideGap is the room between the cover's column and the pane.
	npSideGap = 52
	// npBottomPadding keeps the pane clear of the window's foot.
	npBottomPadding = 24
	// npPaneInset is the room between the pane's left edge and its rows.
	npPaneInset = 10
	// npSideBlock is how tall everything under the cover is.
	npSideBlock = 262
)

var (
	npWhite = ui.RGB(255, 255, 255)
	npInk   = ui.Hex("#0b0b0f")
	npDeep  = ui.Hex("#0a0d15")
	npCard  = ui.RGBA(255, 255, 255, 0.08)
)

// activeLyricLine returns the last line whose timestamp has been reached, or
// -1 for plain lyrics.
func activeLyricLine(lines []ytmusic.LyricLine, synced bool, pos time.Duration) int {
	if !synced {
		return -1
	}
	curMs := pos.Milliseconds()
	active := -1
	for i, line := range lines {
		if curMs >= line.TimeMs {
			active = i
		} else {
			break
		}
	}
	return active
}

// npPanes returns the tabs Now Playing shows.
func (a *app) npPanes() []paneKind {
	return []paneKind{paneLyrics, paneQueue, paneRelated}
}

func npPaneLabel(pane paneKind) string {
	switch pane {
	case paneQueue:
		return "待播清单"
	case paneRelated:
		return "相关推荐"
	default:
		return "歌词"
	}
}

func npPaneKey(pane paneKind) string {
	switch pane {
	case paneQueue:
		return "queue"
	case paneRelated:
		return "related"
	default:
		return "lyrics"
	}
}

// choosePane shows pane in Now Playing, remembering where playback stands so
// that the queue opens at the track playing.
func (a *app) choosePane(pane paneKind) {
	a.npPane = pane
	switch pane {
	case paneRelated:
		a.loadRelated()
	case paneQueue:
		a.npQueueJump = true
	}
}

// closeNowPlaying starts hiding the view.
func (a *app) closeNowPlaying() {
	a.nowPlayingOpen = false
}

// npSpring is how Now Playing comes and goes.
var npSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 450 * time.Millisecond, Bounce: 0.08}}

// npSettle is how long Now Playing takes to come or go, its spring at rest.
const npSettle = 600 * time.Millisecond

// nowPlayingLayer holds Now Playing over the whole window, rising into view
// as it opens and sinking away as it closes: it moves itself, staying on
// top all the way, rather than leaving at once, which would blink it away
// over the page under it. Its pose moves it in frames that only paint,
// building nothing.
func (a *app) nowPlayingLayer(c *ui.Context) {
	open := a.nowPlayingOpen && a.pl.Current() != nil
	now := c.Now()
	if open != a.npOpen {
		a.npOpen = open
		a.npChangedAt = now
		a.npRest = false
		// A build once it is at rest: to stop painting what it covers, or
		// itself once gone.
		c.After(npSettle)
	}
	settled := now.Sub(a.npChangedAt) >= npSettle
	layer := ui.Box(c.Key("now-playing-layer")).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	pose := ui.Pose{Y: 60, Fade: 1}
	if open {
		pose = ui.Pose{}
	}
	layer.AnimatePose(pose, npSpring)
	if !open && settled {
		if !a.npRest {
			a.npRest = true
		}
		layer.Invisible()
		return
	}
	if !open {
		// On its way out, the window under it takes the clicks.
		layer.PassThrough().IgnoreInput(true)
	}
	layer.Children(func() { a.nowPlayingView(c) })
}

// syncNowPlayingWindowButtons lights the window's buttons for the dark view
// while it shows, and back for the appearance's own once it is gone.
func (a *app) syncNowPlayingWindowButtons() {
	if a.win == nil {
		return
	}
	src := mygo.ThemeSystem
	switch a.settings.Appearance {
	case "dark":
		src = mygo.ThemeDark
	case "light":
		src = mygo.ThemeLight
	}
	if a.nowPlayingOpen {
		src = mygo.ThemeDark
	}
	if a.npButtonTheme == src {
		return
	}
	a.npButtonTheme = src
	a.win.SetWindowButtonsTheme(src)
}

// npSideInset is how far in from the sides the cover and the pane sit: in a
// wide window they keep together in the middle.
func npSideInset(w float32) float32 {
	return max(48, (w-npMaxWidth)/2)
}

// npSideWidth is how wide the cover's column is.
func npSideWidth(w float32) float32 {
	return min(max(w*0.28, 248), 380)
}

// npTint is the average color of a cover, which the backdrop takes.
func (a *app) npTint(u string) ui.Color {
	tint := ui.Hex("#464d68")
	if u != "" && a.art != nil {
		for _, side := range []int{artLargeSide, artMediumSide, artSmallSide} {
			if art := a.art.cached(u, side); art != nil && art.color.A > 0 {
				tint = art.color
				break
			}
		}
	}
	// Averages run grey; the brightest channel at 150 restores the hue's
	// saturation, which the backdrop wants.
	mx := max(tint.R, tint.G, tint.B)
	if mx < 40 {
		return tint
	}
	f := float32(150) / float32(mx)
	ch := func(v uint8) uint8 { return uint8(min(255, float32(v)*f)) }
	return ui.RGB(ch(tint.R), ch(tint.G), ch(tint.B))
}

func (a *app) nowPlayingView(c *ui.Context) {
	cur := a.pl.Current()
	if cur == nil {
		a.nowPlayingOpen = false
		return
	}

	// Request frequent frames for smooth lyrics scrolling and the scrubber.
	if a.pl.Playing() {
		c.After(100 * time.Millisecond)
	}
	a.npShortcuts(c)

	w, h := c.Size()
	tint := a.npTint(cur.Thumbnail)
	overlay := ui.Box(c.Key("now-playing-overlay")).Absolute().Fill()
	overlay.LinearGradient(ui.LinearGradient{
		From:  tint.Mix(npDeep, 0.62),
		To:    npDeep,
		Angle: 180,
		Oklab: true,
	})
	overlay.Children(func() {
		a.npBackdrop(c, tint, w)
		ui.Column(c).Fill().Children(func() {
			a.npTopBar(c)
			a.npBody(c, cur, w, h)
		})
	})
}

// npBackdrop adds the cover's color, gathered at the top of the view, to the
// backdrop's gradient.
func (a *app) npBackdrop(c *ui.Context, tint ui.Color, w float32) {
	ui.Box(c.Key("np-glow")).Absolute().Fill().PassThrough().RadialGradient(ui.RadialGradient{
		Center: ui.Point{X: 0.3, Y: -0.04},
		Radius: max(560, w*0.8),
		Oklab:  true,
		Stops: []ui.GradientStop{
			{Color: tint.Alpha(0.28), At: 0},
			{Color: tint.Alpha(0.08), At: 0.55},
			{Color: ui.Transparent, At: 1},
		},
	})
}

// npShortcuts binds Esc to closing the view and ⌘1 to ⌘4 to its tabs.
func (a *app) npShortcuts(c *ui.Context) {
	c.OnShortcut(0, ui.KeyEscape, a.closeNowPlaying)
	for i, pane := range a.npPanes() {
		if i >= 4 {
			break
		}
		p := pane
		c.OnShortcut(ui.Cmd, ui.Key(int(ui.Key1)+i), func() {
			a.choosePane(p)
			c.Invalidate()
		})
	}
}

func (a *app) npTopBar(c *ui.Context) {
	bar := ui.Row(c.Key("np-top-bar")).FillWidth().Height(npTopBarHeight).AlignItems(ui.Center).Justify(ui.Center).DragWindow()
	if bar.DoubleClicked() {
		a.toggleMaximize()
	}
	bar.Children(func() {
		close := ui.Box(c.Key("np-close")).Label("收起正在播放").Size(34, 34).Radius(17).
			AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
		bg := ui.RGBA(255, 255, 255, 0.06)
		if close.Hovered() {
			bg = ui.RGBA(255, 255, 255, 0.16)
		}
		close.Background(bg)
		if close.Clicked() {
			a.closeNowPlaying()
		}
		close.Children(func() {
			ui.Icon(c, iconChevron).Size(18, 18).TextColor(npWhite.Alpha(0.8))
		})
	})
}

func (a *app) npTabs(c *ui.Context) {
	ui.Row(c.Key("np-tabs")).FillWidth().Height(npTabsHeight).Padding(0, 0, 0, npPaneInset).
		AlignItems(ui.Center).Gap(4).Children(func() {
		for _, pane := range a.npPanes() {
			a.npTabButton(c, pane)
		}
		ui.Spacer(c)
		a.npLyricsBadges(c)
	})
}

// npLyricsBadges say what the lyrics are, and resume following them after
// the listener scrolled them, beside the tabs.
func (a *app) npLyricsBadges(c *ui.Context) {
	if a.npPane != paneLyrics || a.lyrics == nil {
		return
	}
	if !a.lyrics.Synced {
		badge := ui.Row(c.Key("lyrics-plain")).Padding(2, 8).Radius(6).Background(npWhite.Alpha(0.08)).
			AlignItems(ui.Center)
		badge.Children(func() {
			ui.Text(c, "纯文本歌词").FontSize(11).FontWeight(600).TextColor(npWhite.Alpha(0.5))
		})
		return
	}
	if !a.npLyricsManual {
		return
	}
	follow := ui.Row(c.Key("lyrics-follow")).Label("跟随播放").Height(28).Radius(14).
		Background(npWhite.Alpha(0.12)).Padding(0, 12).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if follow.Hovered() {
		follow.Background(npWhite.Alpha(0.2))
	}
	if follow.Clicked() {
		a.npLyricsManual = false
		a.npCurrentY = a.npLyricScroll.Y
		c.Invalidate()
	}
	follow.Children(func() {
		ui.Text(c, "跟随播放").FontSize(12).TextColor(npWhite)
	})
}

func (a *app) npTabButton(c *ui.Context, pane paneKind) {
	label := npPaneLabel(pane)
	active := a.npPane == pane
	btn := ui.ButtonBase(c.Key("np-tab-"+npPaneKey(pane))).Padding(6, 14).Radius(999).
		FocusRing(false).Label(label).Cursor(ui.CursorPointer).Gap(6)
	bg := ui.Transparent
	fg := npWhite.Alpha(0.55)
	switch {
	case active:
		bg = npWhite.Alpha(0.14)
		fg = npWhite
	case btn.Hovered():
		bg = npWhite.Alpha(0.07)
	}
	btn.Background(bg)
	btn.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg)
		if pane == paneQueue {
			if tracks, at := a.pl.QueueView(); len(tracks) > 0 {
				ui.Text(c, fmt.Sprint(max(0, len(tracks)-max(at+1, 0)))).
					FontSize(11).FontWeight(600).TextColor(fg.Alpha(0.6))
			}
		}
	})
	if btn.Clicked() {
		a.choosePane(pane)
	}
}

// npLayout is where the cover and the pane beside it lie in a window w×h.
type npLayout struct {
	inset   float32
	sideW   float32
	cover   float32
	paneX   float32
	paneW   float32
	rowH    float32
	paneTop float32
}

func npMetrics(w, h float32) npLayout {
	inset := npSideInset(w)
	sideW := npSideWidth(w)
	cover := min(sideW, max(160, h*0.40))
	sideW = max(cover, 264)
	rowH := h - npTopBarHeight - npBottomPadding
	paneX := inset + sideW + npSideGap
	return npLayout{
		inset:   inset,
		sideW:   sideW,
		cover:   cover,
		paneX:   paneX,
		paneW:   w - inset - paneX,
		rowH:    rowH,
		paneTop: max(0, (rowH-cover-npSideBlock)/2),
	}
}

func (a *app) npBody(c *ui.Context, cur *ytmusic.Track, w, h float32) {
	m := npMetrics(w, h)

	ui.Row(c.Key("np-body")).Grow(1).FillWidth().Padding(0, m.inset, npBottomPadding, m.inset).
		Gap(npSideGap).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c.Key("np-side")).Width(m.sideW).FillHeight().Justify(ui.Center).Children(func() {
			a.npCover(c, cur, m.cover)
			a.npInfo(c, cur)
			a.npScrubber(c)
			a.npTimes(c)
			a.npTransport(c)
			a.npVolume(c)
		})

		ui.Column(c.Key("np-pane")).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
			if m.paneTop > 0 {
				ui.Box(c).Height(m.paneTop)
			}
			a.npTabs(c)
			a.npPaneContent(c, cur, m.paneW, m.rowH-m.paneTop-npTabsHeight)
		})
	})
}

func (a *app) npCover(c *ui.Context, cur *ytmusic.Track, size float32) {
	box := ui.Box(c.Key("np-cover")).Size(size, size).Radius(12).Cursor(ui.CursorPointer)
	// The cover breathes as music plays, and settles as it pauses.
	scale := float32(1)
	if !a.pl.Playing() {
		scale = 0.965
	}
	pose := ui.Pose{Scale: scale}
	anim := ui.Animation{Spring: ui.SpringOptions{Duration: 650 * time.Millisecond, Bounce: 0.12}}
	shadowY, shadowBlur := float32(26), float32(52)
	if x, _, over := box.PointerPosition(); over {
		scale += 0.035
		pose = ui.Pose{Scale: scale, Y: -5, Rotate: max(-2, min(2, (x/size-0.5)*4))}
		anim = ui.Animation{Spring: ui.SpringOptions{Duration: 220 * time.Millisecond, Bounce: 0.06}}
		shadowY, shadowBlur = 34, 64
		c.After(16 * time.Millisecond)
	}
	box.Shadow(0, shadowY, shadowBlur, -18, ui.RGBA(0, 0, 0, 0.7))
	box.AnimatePose(pose, anim)
	if box.Clicked() {
		a.pl.TogglePlay()
	}
	box.Children(func() {
		if cur.Thumbnail == "" {
			ui.Box(c).Fill().Radius(12).Background(npCard).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(c, iconMusic).Size(size*0.3, size*0.3).TextColor(npWhite.Alpha(0.35))
			})
			return
		}
		a.coverImageTinted(c, cur.Thumbnail, size, 12, true, npCard, npWhite.Alpha(0.35))
	})
}

func (a *app) npInfo(c *ui.Context, cur *ytmusic.Track) {
	ui.Column(c).FillWidth().Margin(26, 0, 0, 0).Gap(6).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(2).Children(func() {
			ui.Text(c, cur.Title).FontSize(22).FontWeight(700).LetterSpacing(-0.3).TextColor(npWhite).
				SingleLine().Grow(1).MinWidth(0).Tooltip(cur.Title)
			a.npLikeButton(c, *cur)
			a.npMoreButton(c, *cur)
		})
		a.npArtists(c, *cur)
		if a.pl.StreamFallback() {
			ui.Text(c, "音质已回退至 96 kbps").FontSize(11).TextColor(npWhite.Alpha(0.4))
		}
	})
}

func (a *app) npLikeButton(c *ui.Context, t ytmusic.Track) {
	liked := a.isLiked(t.ID)
	label := "喜欢 " + t.Title
	if liked {
		label = "取消喜欢 " + t.Title
	}
	btn := ui.Box(c.Key("np-like")).Label(label).Size(30, 30).Radius(15).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
	if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.1))
	}
	if btn.Clicked() {
		a.toggleLike(t)
		c.Invalidate()
	}
	btn.Children(func() {
		if liked {
			ui.Icon(c, iconHeartFill).Size(18, 18).TextColor(npWhite)
		} else {
			ui.Icon(c, iconHeart).Size(18, 18).TextColor(npWhite.Alpha(0.65))
		}
	})
}

func (a *app) npMoreButton(c *ui.Context, t ytmusic.Track) {
	btn := ui.Box(c.Key("np-more")).Label("更多操作").Size(30, 30).Radius(15).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0).
		ContextMenu(a.trackCopyMenu(c, t))
	if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.1))
	}
	btn.Children(func() {
		ui.Icon(c, iconMore).Size(18, 18).TextColor(npWhite.Alpha(0.65))
	})
}

// npArtists shows who sings what plays, each opening their page.
func (a *app) npArtists(c *ui.Context, t ytmusic.Track) {
	artists := make([]ytmusic.ArtistRef, 0, len(t.Artists))
	isVideo := t.Video
	for _, ar := range t.Artists {
		if ytmusic.VideoByline(ar.Name) {
			isVideo = true
		}
		ar.Name = ytmusic.CleanArtistName(ar.Name)
		if ar.Name != "" {
			artists = append(artists, ar)
		}
	}
	ui.Row(c).Wrap().AlignItems(ui.Center).Children(func() {
		if isVideo {
			ui.Box(c.Key("np-video-badge")).Padding(2, 7).Margin(0, 8, 0, 0).Radius(4).
				Background(ui.RGBA(255, 255, 255, 0.1)).Children(func() {
				ui.Text(c, "音乐视频").FontSize(10.5).TextColor(npWhite.Alpha(0.6))
			})
		}
		if len(artists) == 0 {
			ui.Text(c, "未知音乐人").FontSize(14).TextColor(npWhite.Alpha(0.5))
			return
		}
		for i, ar := range artists {
			if i > 0 {
				ui.Text(c, ", ").FontSize(14).TextColor(npWhite.Alpha(0.42))
			}
			artist := ar
			r := route{page: pageSearch, query: artist.Name, kind: ytmusic.SearchArtists}
			if artist.ID != "" && strings.HasPrefix(artist.ID, "UC") {
				r = route{page: pageArtist, id: artist.ID}
			}
			link := ui.Row(c.Key(fmt.Sprintf("np-artist-%s-%d", t.ID, i))).
				Label("正在播放 · " + artist.Name).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			var label ui.Element
			link.Children(func() {
				label = ui.Text(c, artist.Name).FontSize(14).TextColor(npWhite.Alpha(0.55))
			})
			if link.Hovered() {
				label.TextColor(npWhite).Underline()
			}
			if link.Clicked() {
				a.closeNowPlaying()
				a.open(r)
			}
		}
	})
}

func (a *app) npScrubber(c *ui.Context) {
	dur := a.pl.Duration()
	durSec := dur.Seconds()
	if durSec <= 0 {
		durSec = 1
	}
	if !a.npScrub.held && !a.npScrub.drag {
		a.npScrub.v = a.pl.Position().Seconds()
	}
	s := ui.SliderBase(c.Key("np-scrub"), &a.npScrub.v, 0, durSec).Height(18).PaddingX(7).
		Margin(20, 0, 0, 0).FocusRing(false).Label("播放进度").Cursor(ui.CursorPointer)
	// The press and its release come to the bar wherever the pointer is, and
	// go on to the slider, which follows it.
	s.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button == 0 {
				a.npScrub.held = true
			}
		case ui.InputPointerUp:
			a.npScrub.held = false
		}
		return false
	})
	switch {
	case a.npScrub.held:
		a.npScrub.drag = true
	case a.npScrub.drag:
		a.npScrub.drag = false
		a.seekScrub(dur)
	case s.Changed():
		a.seekScrub(dur)
	}
	hot := s.Hovered() || a.npScrub.held
	thick := float32(4)
	if hot {
		thick = 6
	}
	thick = s.AnimateTo("thick", thick, ui.Snappy)
	knob := float32(0)
	if hot {
		knob = 1
	}
	knob = s.AnimateTo("knob", knob, ui.Snappy)
	v := float32(min(1, max(0, a.npScrub.v/durSec)))
	s.Draw(func(pt *ui.Painter, r ui.Rect) {
		y := r.Y + (r.H-thick)/2
		head := r.X + r.W*v
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, npWhite.Alpha(0.18), thick/2)
		pt.Fill(ui.Rect{X: r.X, Y: y, W: max(thick, head-r.X), H: thick}, npWhite.Alpha(0.92), thick/2)
		if knob > 0.01 {
			k := 12 * knob
			pt.Fill(ui.Rect{X: head - k/2, Y: r.Y + (r.H-k)/2, W: k, H: k}, npWhite, k/2)
		}
	})
}

// seekScrub seeks to where the progress bar is dragged, short of its end,
// where the track would end as it began.
func (a *app) seekScrub(dur time.Duration) {
	if dur <= 0 {
		return
	}
	pos := time.Duration(a.npScrub.v * float64(time.Second))
	pos = min(max(pos, 0), max(0, dur-time.Second))
	a.pl.Seek(pos)
}

func (a *app) npTimes(c *ui.Context) {
	pos := a.pl.Position()
	dur := a.pl.Duration()
	ui.Row(c).FillWidth().Margin(4, 0, 0, 0).Height(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, formatTime(pos)).FontSize(11).FontFeatures("tnum").TextColor(npWhite.Alpha(0.5))
		ui.Spacer(c)
		rem := "-" + formatTime(max(0, dur-pos))
		if a.pl.Loading() {
			rem = "正在缓冲…"
		}
		ui.Text(c, rem).FontSize(11).FontFeatures("tnum").TextColor(npWhite.Alpha(0.5))
	})
}

func (a *app) npTransport(c *ui.Context) {
	ui.Row(c).FillWidth().Margin(16, 0, 0, 0).Height(56).AlignItems(ui.Center).Justify(ui.Center).
		Gap(8).Children(func() {
		a.npIconButton(c, "np-shuffle", iconShuffle, 18, "随机播放", a.pl.Shuffle(), func() {
			a.pl.SetShuffle(!a.pl.Shuffle())
		})
		a.npIconButton(c, "np-prev", iconPrev, 20, "上一首", false, a.pl.Prev)
		a.npPlayButton(c)
		a.npIconButton(c, "np-next", iconNext, 20, "下一首", false, a.pl.Next)
		icon := iconRepeat
		if a.pl.Repeat() == player.RepeatOne {
			icon = iconRepeat1
		}
		a.npIconButton(c, "np-repeat", icon, 18, "循环播放", a.pl.Repeat() != player.RepeatOff, a.pl.ToggleRepeat)
	})
}

func (a *app) npIconButton(c *ui.Context, key string, icon *ui.SVG, size float32, label string, on bool, onClick func()) {
	btn := ui.Box(c.Key(key)).Label(label).Size(40, 40).Radius(20).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
	if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.08))
	}
	if btn.Clicked() {
		onClick()
	}
	col := npWhite.Alpha(0.5)
	if on {
		col = npWhite
	}
	btn.Children(func() {
		ui.Icon(c, icon).Size(size, size).TextColor(col)
	})
}

func (a *app) npPlayButton(c *ui.Context) {
	active := a.pl.Active()
	label := "播放"
	if active {
		label = "暂停"
	}
	btn := ui.Box(c.Key("np-play")).Label(label).Size(52, 52).Radius(26).Background(npWhite).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
	if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.88))
	}
	if btn.Clicked() {
		a.pl.TogglePlay()
	}
	btn.Children(func() {
		icon := iconPlay
		if active {
			icon = iconPause
		}
		ui.Icon(c, icon).Size(22, 22).TextColor(npInk)
	})
}

func (a *app) npVolume(c *ui.Context) {
	vol := a.pl.Volume()
	ui.Row(c).FillWidth().Margin(18, 0, 0, 0).AlignItems(ui.Center).Justify(ui.Center).
		Gap(8).Children(func() {
		icon := iconVolume
		switch {
		case vol == 0:
			icon = iconMute
		case vol < 0.5:
			icon = iconVolumeLow
		}
		mute := ui.Box(c.Key("np-mute")).Label("静音").Size(26, 26).Radius(13).
			AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
		if mute.Hovered() {
			mute.Background(ui.RGBA(255, 255, 255, 0.08))
		}
		if mute.Clicked() {
			if vol > 0 {
				a.pl.SetVolume(0)
			} else {
				a.pl.SetVolume(0.8)
			}
		}
		mute.Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(npWhite.Alpha(0.6))
		})

		v := vol
		s := ui.SliderBase(c.Key("np-volume"), &v, 0, 1).Width(124).Height(16).FocusRing(false).Label("音量")
		if s.Changed() {
			a.pl.SetVolume(v)
		}
		hot := s.Hovered() || s.Pressed()
		thick := float32(4)
		if hot {
			thick = 5
		}
		thick = s.AnimateTo("thick", thick, ui.Snappy)
		fill := float32(v)
		s.Draw(func(pt *ui.Painter, r ui.Rect) {
			y := r.Y + (r.H-thick)/2
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, npWhite.Alpha(0.18), thick/2)
			pt.Fill(ui.Rect{X: r.X, Y: y, W: max(thick, r.W*fill), H: thick}, npWhite.Alpha(0.85), thick/2)
		})
	})
}

// npPaneContent fills the pane beside the cover, as the chosen tab says.
func (a *app) npPaneContent(c *ui.Context, cur *ytmusic.Track, paneW, paneH float32) {
	switch a.npPane {
	case paneQueue:
		a.npQueuePane(c)
	case paneRelated:
		a.npRelatedPane(c, paneW)
	default:
		a.npLyricsPane(c)
	}
}

// npPaneMessage centers a message in place of a pane's content.
func npPaneMessage(c *ui.Context, title, detail string) {
	ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(18).FontWeight(600).TextColor(npWhite.Alpha(0.7)).TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(13).TextColor(npWhite.Alpha(0.35)).TextAlign(ui.Center)
		}
	})
}

func clamp32(v, lo, hi float32) float32 {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
