package app

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"

	"github.com/douglarek/melo/internal/ytmusic"
)

// npLyricsPane shows the lyrics of what plays, following the line sung.
func (a *app) npLyricsPane(c *ui.Context) {
	if a.lyricsLoading {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingLyrics()).FontSize(14).TextColor(npWhite.Alpha(0.55))
		})
		return
	}
	if a.lyrics == nil || len(a.lyrics.Lines) == 0 {
		npPaneMessage(c, locales.NoLyrics(), locales.LyricsEmptyHint())
		return
	}

	synced := a.lyrics.Synced
	pos := a.pl.Position()
	curLine := activeLyricLine(a.lyrics.Lines, synced, pos)
	// Redraw as the next line begins, rather than every frame.
	if synced && a.pl.Playing() && curLine+1 < len(a.lyrics.Lines) {
		next := time.Duration(a.lyrics.Lines[curLine+1].TimeMs) * time.Millisecond
		if d := next - pos + 20*time.Millisecond; d > 0 {
			c.After(d)
		}
	}

	ui.Column(c).Fill().Children(func() {
		viewport := ui.Box(c.Key("np-lyrics-box")).Grow(1).FillWidth()
		viewport.Children(func() {
			// Hand the scroll position to the listener when they scroll or
			// drag the lyrics; playback keeps highlighting lines meanwhile.
			expectedY := a.npCurrentY
			if a.npLyricScroll.MaxY > 0 && expectedY > a.npLyricScroll.MaxY {
				expectedY = a.npLyricScroll.MaxY
			}
			if !a.npLyricsManual && math.Abs(float64(a.npLyricScroll.Y-expectedY)) > 4 {
				a.npLyricsManual = true
			}
			if a.npLyricsManual {
				a.npCurrentY = a.npLyricScroll.Y
			} else if a.npTargetY != a.npCurrentY {
				diff := a.npTargetY - a.npCurrentY
				if math.Abs(float64(diff)) > 0.5 {
					a.npCurrentY += diff * 0.16
					a.npCurrentY = clamp32(a.npCurrentY, 0, max(0, a.npLyricScroll.MaxY))
					c.AnimationFrame()
				} else {
					a.npCurrentY = a.npTargetY
				}
				a.npLyricScroll.Y = a.npCurrentY
			}

			scroll := ui.Scroll(c.Key("np-lyrics-scroll")).Vertical().TrackScroll(&a.npLyricScroll).
				Label(locales.LyricsContent()).Fill()
			var activeEl ui.Element
			scroll.Children(func() {
				ui.Column(c).FillWidth().Gap(4).Padding(0, 0, 220, 12).Children(func() {
					for i := range a.lyrics.Lines {
						el := a.npLyricLine(c, i, synced, curLine)
						if synced && i == curLine {
							activeEl = el
						}
					}
				})
			})

			// Center the line sung at about a third of the pane, measured
			// from the last layout so that long lines and wide glyphs land
			// where they really are.
			if synced && !a.npLyricsManual {
				if curLine < 0 {
					a.npTargetY = 0
				} else if b := activeEl.LayoutBounds(); b.H > 0 {
					if vb := viewport.LayoutBounds(); vb.H > 0 {
						target := b.Y - vb.Y + a.npLyricScroll.Y - vb.H*0.32
						a.npTargetY = clamp32(target, 0, max(0, a.npLyricScroll.MaxY))
					} else {
						c.AnimationFrame() // not laid out yet
					}
				} else {
					c.AnimationFrame() // not laid out yet
				}
			}
		})
	})
}

// npLyricLine builds the i-th line, lit while it is sung, and seeking to it
// when it is clicked.
func (a *app) npLyricLine(c *ui.Context, i int, synced bool, curLine int) ui.Element {
	line := a.lyrics.Lines[i]
	active := synced && i == curLine
	row := ui.Row(c.Key(fmt.Sprintf("lyric-row-%d", i))).FillWidth().Radius(10).Padding(6, 10)
	if synced {
		row.Cursor(ui.CursorPointer)
	}
	if row.Hovered() && !active {
		row.Background(npWhite.Alpha(0.06))
	}
	if synced && row.Clicked() {
		a.npLyricsManual = false
		a.npCurrentY = a.npLyricScroll.Y
		a.pl.Seek(time.Duration(line.TimeMs) * time.Millisecond)
	}
	row.Children(func() {
		text := ui.Text(c, line.Text)
		switch {
		case active:
			text.FontSize(27).FontWeight(700).LetterSpacing(-0.3).TextColor(npWhite)
		case synced:
			text.FontSize(19).FontWeight(500).TextColor(npWhite.Alpha(0.36))
			if row.Hovered() {
				text.TextColor(npWhite.Alpha(0.62))
			}
		default:
			text.FontSize(17).TextColor(npWhite.Alpha(0.72))
		}
	})
	return row
}

// npQueuePane lists the whole queue in play order, the track playing marked
// among the others: a click plays one there.
func (a *app) npQueuePane(c *ui.Context) {
	tracks, at := a.pl.QueueView()
	if len(tracks) == 0 {
		npPaneMessage(c, locales.NothingUpNext(), locales.NowPlayingEmptyHint())
		return
	}
	if a.npQueueJump {
		a.npQueueJump = false
		if at > 0 {
			a.npQueueScroll.Y = float32(at) * 58
		}
	}
	ui.Scroll(c.Key("np-queue-scroll")).Vertical().TrackScroll(&a.npQueueScroll).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(2).Padding(0, 0, 24, 0).Children(func() {
			for i := range tracks {
				idx := i
				t := tracks[i]
				a.npTrackRow(c, fmt.Sprintf("np-queue-%d", idx), t, idx == at, func() {
					a.pl.PlayAt(idx)
				})
			}
		})
	})
}

// npTrackRow is a row of a pane's list: its cover, what it is, and how long
// it runs.
func (a *app) npTrackRow(c *ui.Context, key string, t ytmusic.Track, playing bool, onClick func()) {
	row := ui.Row(c.Key(key)).FillWidth().Height(56).Radius(10).Padding(0, 10).
		AlignItems(ui.Center).Gap(12).Cursor(ui.CursorPointer)
	switch {
	case playing:
		row.Background(npWhite.Alpha(0.12))
	case row.Hovered():
		row.Background(npWhite.Alpha(0.06))
	}
	if row.Clicked() && !playing {
		onClick()
	}
	row.Children(func() {
		a.coverImageTinted(c, t.Thumbnail, 44, 7, true, npCard, npWhite.Alpha(0.35))
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
			title := ui.Text(c, t.Title).FontSize(14).TextColor(npWhite.Alpha(0.88)).
				SingleLine().Tooltip(t.Title)
			artists := ui.Text(c, t.ArtistNames()).FontSize(12).TextColor(npWhite.Alpha(0.45)).
				SingleLine().Tooltip(t.ArtistNames())
			if playing {
				title.FontWeight(600).TextColor(npWhite)
				artists.TextColor(npWhite.Alpha(0.6))
			}
		})
		if playing {
			ui.Icon(c, iconVolume).Size(14, 14).TextColor(npWhite.Alpha(0.75))
		}
		if t.DurationMs > 0 {
			ui.Text(c, t.DurationString()).FontSize(12).FontFeatures("tnum").TextColor(npWhite.Alpha(0.4))
		}
	})
}

// npRelatedPane shows what to play next, beside what plays: tracks you may
// like, and playlists to open.
func (a *app) npRelatedPane(c *ui.Context, paneW float32) {
	if !a.relatedLoaded && !a.relatedLoading {
		a.loadRelated()
	}
	if a.relatedLoading {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.LoadingRelatedMusic()).FontSize(13).TextColor(npWhite.Alpha(0.55))
		})
		return
	}

	var trackShelf, cardShelf *ytmusic.Section
	for i := range a.relatedSections {
		sec := &a.relatedSections[i]
		if len(sec.Items) == 0 {
			continue
		}
		switch sec.Items[0].Type {
		case "track":
			if trackShelf == nil {
				trackShelf = sec
			}
		case "playlist", "album":
			if cardShelf == nil {
				cardShelf = sec
			}
		}
	}
	if trackShelf == nil && cardShelf == nil {
		npPaneMessage(c, locales.NothingRelatedYet(), locales.TryAnotherSong())
		return
	}

	ui.Scroll(c.Key("np-related-scroll")).Vertical().TrackScroll(&a.npRelatedScroll).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(28).Padding(0, 0, 24, npPaneInset).Children(func() {
			if trackShelf != nil {
				var tracks []ytmusic.Track
				for _, item := range trackShelf.Items {
					if item.Track != nil {
						tracks = append(tracks, *item.Track)
					}
				}
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					a.npShelfTitle(c, shelfTitle(trackShelf.Title, locales.YouMightAlsoLike()))
					for i := range tracks {
						idx := i
						tr := tracks[i]
						a.npTrackRow(c, fmt.Sprintf("np-related-track-%d", idx), tr, false, func() {
							a.pl.SetQueue(tracks, idx)
						})
					}
				})
			}
			if cardShelf != nil {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					a.npShelfTitle(c, shelfTitle(cardShelf.Title, locales.RecommendedPlaylists()))
					a.npRelatedCards(c, cardShelf.Items, paneW)
				})
			}
		})
	})
}

func (a *app) npShelfTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).FontWeight(700).TextColor(npWhite.Alpha(0.85)).Margin(0, 10, 4, 10)
}

func shelfTitle(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}

// npRelatedCards lays playlist and album cards out in as many columns as fit.
func (a *app) npRelatedCards(c *ui.Context, items []ytmusic.CardItem, paneW float32) {
	const gap = float32(14)
	cols := max(2, min(int((paneW+gap)/156), 6))
	cardW := max(72, (paneW-gap*float32(cols-1))/float32(cols))
	for i := 0; i < len(items); i += cols {
		ui.Row(c).FillWidth().Gap(gap).Children(func() {
			for j := 0; j < cols && i+j < len(items); j++ {
				idx := i + j
				a.npRelatedCard(c, fmt.Sprintf("np-related-card-%d", idx), items[idx], cardW)
			}
		})
	}
}

func (a *app) npRelatedCard(c *ui.Context, key string, item ytmusic.CardItem, width float32) {
	card := ui.Column(c.Key(key)).Width(width).Gap(8).Cursor(ui.CursorPointer)
	if card.Clicked() {
		a.closeNowPlaying()
		switch item.Type {
		case "playlist":
			a.open(route{page: pagePlaylist, id: item.ID})
		case "album":
			a.open(route{page: pageAlbum, id: item.ID})
		case "artist":
			a.open(route{page: pageArtist, id: item.ID})
		}
	}
	card.Children(func() {
		a.coverImageTinted(c, item.Thumbnail, width, 10, true, npCard, npWhite.Alpha(0.35))
		ui.Text(c, item.Title).FontSize(13).FontWeight(600).TextColor(npWhite.Alpha(0.9)).
			MaxLines(2).Tooltip(item.Title)
		if item.Subtitle != "" {
			ui.Text(c, item.Subtitle).FontSize(11.5).TextColor(npWhite.Alpha(0.45)).
				SingleLine().Tooltip(item.Subtitle)
		}
	})
}
