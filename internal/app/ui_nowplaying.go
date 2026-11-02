package app

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
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

func (a *app) nowPlayingView(c *ui.Context) {
	p := a.pal
	cur := a.pl.Current()
	if cur == nil {
		a.nowPlayingOpen = false
		a.closeVideo()
		return
	}
	if !cur.Video {
		a.npVideoMode = false
		a.closeVideo()
	}

	// Request frequent frames for smooth lyrics scrolling & visualizer
	if a.pl.Playing() {
		c.After(100 * time.Millisecond)
	}

	// Full-screen overlay
	overlay := ui.Box(c.Key("now-playing-overlay")).Absolute().Fill().Background(p.bg)
	overlay.Children(func() {
		ui.Column(c).Fill().Padding(24, 36).Children(func() {
			// Top bar with close button
			npTopBar := ui.Row(c.Key("np-top-bar")).FillWidth().Height(44).AlignItems(ui.Center).DragWindow()
			if npTopBar.DoubleClicked() {
				a.toggleMaximize()
			}
			npTopBar.Children(func() {
				closeBtn := ui.Box(c.Key("np-close")).Size(36, 36).Radius(18).Background(p.card).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if closeBtn.Hovered() {
					closeBtn.Background(p.hover)
				}
				if closeBtn.Clicked() {
					a.nowPlayingOpen = false
					a.closeVideo()
				}
				closeBtn.Children(func() {
					ui.Icon(c, iconClose).Size(18, 18).TextColor(p.text)
				})

				ui.Spacer(c)
				if cur.Video {
					a.nowPlayingModeSwitch(c)
				} else {
					ui.Text(c, "正在播放").FontSize(14).Bold().TextColor(p.muted)
				}
				ui.Spacer(c)
				ui.Box(c).Size(36, 36)
			})

			// Main area: video, or artwork/lyrics and the upcoming queue.
			if a.npVideoMode && cur.Video {
				a.nowPlayingVideo(c)
				return
			}
			ui.Row(c).Grow(1).FillWidth().Margin(24, 0).Gap(32).Children(func() {
				// Left Column: Artwork + Visualizer + Title
				ui.Column(c).Width(320).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(20).Children(func() {
					// Large Cover Artwork
					a.coverImage(c, cur.Thumbnail, 300, 16)

					// Visualizer Spectrum Bars
					spectrum := a.pl.Spectrum()
					ui.Row(c).Height(24).AlignItems(ui.End).Gap(4).Children(func() {
						for idx, val := range spectrum {
							barH := max(4, float32(val*24))
							ui.Box(c.Key(fmt.Sprintf("spec-%d", idx))).Width(4).Height(barH).Radius(2).Background(p.accent)
						}
					})

					// Track title & artist
					ui.Column(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, cur.Title).FontSize(22).Bold().TextColor(p.text).TextAlign(ui.Center)
						a.artistLinks(c, *cur, p.muted, p.text)
					})
				})

				// Middle Column: Lyrics
				ui.Column(c).Grow(1).Basis(0).FillHeight().Children(func() {
					ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Gap(20).Children(func() {
						ui.Text(c, "歌词").FontSize(18).Bold().TextColor(p.text)
						if a.lyrics != nil && !a.lyrics.Synced {
							ui.Text(c, "未同步").FontSize(11).TextColor(p.faint)
						}
						if a.npLyricsManual {
							followBtn := ui.Row(c.Key("lyrics-follow")).Height(28).Radius(14).Background(p.card).Padding(0, 12).AlignItems(ui.Center).Cursor(ui.CursorPointer)
							if followBtn.Hovered() {
								followBtn.Background(p.hover)
							}
							if followBtn.Clicked() {
								a.npLyricsManual = false
								a.npCurrentY = a.npLyricScroll.Y
								c.Invalidate()
							}
							followBtn.Children(func() {
								ui.Text(c, "跟随播放").FontSize(12).TextColor(p.text)
							})
						}
						ui.Spacer(c)
					})

					ui.Box(c).Grow(1).FillWidth().Margin(12, 0).Children(func() {
						if a.lyricsLoading {
							ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
								ui.Spinner(c)
								ui.Text(c, "正在加载歌词...").FontSize(14).TextColor(p.muted)
							})
							return
						}

						if a.lyrics == nil || len(a.lyrics.Lines) == 0 {
							ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
								ui.Text(c, "暂无歌词").FontSize(16).TextColor(p.faint)
							})
							return
						}

						// Synchronized lyric scrolling. Plain lyrics have no
						// active line.
						synced := a.lyrics.Synced
						curLine := activeLyricLine(a.lyrics.Lines, synced, a.pl.Position())

						// Calculate target scroll offset (approx 46px per line, center active line at ~35% height)
						const lineStep = float32(46)
						targetY := float32(curLine)*lineStep - 120
						if targetY < 0 {
							targetY = 0
						}
						a.npTargetY = targetY

						// Hand the scroll position to the user when they scroll or drag
						// the lyrics bar; playback keeps highlighting lines meanwhile.
						expectedY := a.npCurrentY
						if a.npLyricScroll.MaxY > 0 && expectedY > a.npLyricScroll.MaxY {
							expectedY = a.npLyricScroll.MaxY
						}
						if !a.npLyricsManual && math.Abs(float64(a.npLyricScroll.Y-expectedY)) > 4 {
							a.npLyricsManual = true
						}

						if !a.npLyricsManual {
							diff := a.npTargetY - a.npCurrentY
							if math.Abs(float64(diff)) > 0.5 {
								a.npCurrentY += diff * 0.15
								a.npLyricScroll.Y = a.npCurrentY
								c.AnimationFrame()
							} else {
								a.npCurrentY = a.npTargetY
								a.npLyricScroll.Y = a.npTargetY
							}
						}

						ui.Scroll(c.Key("lyrics-scroll")).Vertical().TrackScroll(&a.npLyricScroll).Fill().Children(func() {
							ui.Column(c).FillWidth().Gap(14).Padding(60, 20, 240, 20).Children(func() {
								for i, line := range a.lyrics.Lines {
									idx := i
									l := line
									active := synced && idx == curLine

									lineBox := ui.Row(c.Key(fmt.Sprintf("lyric-row-%d", idx))).FillWidth().Radius(8).Padding(6, 12)
									if synced {
										lineBox.Cursor(ui.CursorPointer)
									}
									if lineBox.Hovered() && !active {
										lineBox.Background(p.hover)
									}
									if synced && lineBox.Clicked() {
										a.npLyricsManual = false
										a.npCurrentY = a.npLyricScroll.Y
										a.pl.Seek(time.Duration(l.TimeMs) * time.Millisecond)
									}

									lineBox.Children(func() {
										lineText := ui.Text(c, l.Text)
										if active {
											lineText.Bold().FontSize(24).TextColor(p.text)
										} else {
											lineText.FontSize(17).TextColor(p.muted)
										}
									})
								}
							})
						})
					})
				})

				// Right Column: Up Next / Related
				a.nowPlayingRightPanel(c)
			})
		})
	})
}

// nowPlayingRightPanel shows either the playback queue or related content.
func (a *app) nowPlayingRightPanel(c *ui.Context) {
	tracks, at := a.pl.QueueView()
	start := at + 1
	if start < 0 {
		start = 0
	}
	upNext := 0
	if start < len(tracks) {
		upNext = len(tracks) - start
	}

	ui.Column(c).Width(a.npRightWidth()).FillHeight().Children(func() {
		ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Gap(22).Children(func() {
			a.npTabButton(c, "接下来", npTabQueue, "np-tab-queue", upNext)
			a.npTabButton(c, "相关", npTabRelated, "np-tab-related", 0)
		})

		ui.Box(c).Grow(1).FillWidth().Margin(16, 0).Children(func() {
			if a.npTab == npTabRelated {
				a.nowPlayingRelated(c)
				return
			}
			a.nowPlayingQueueList(c, tracks, start)
		})
	})
}

func (a *app) npRightWidth() float32 {
	if a.win == nil {
		return 480
	}
	w, _ := a.win.ContentSize()
	if w <= 0 {
		return 480
	}
	pw := float32(w) * 0.30
	if pw < 320 {
		pw = 320
	}
	if pw > 520 {
		pw = 520
	}
	return pw
}

func (a *app) npTabButton(c *ui.Context, label string, tab npRightTab, key string, count int) {
	p := a.pal
	active := a.npTab == tab
	col := p.muted
	if active {
		col = p.accent
	}

	btn := ui.Row(c.Key(key)).Height(44).AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer)
	if btn.Clicked() {
		a.npTab = tab
		if tab == npTabRelated {
			a.loadRelated()
		}
		c.Invalidate()
	}
	btn.Children(func() {
		text := ui.Text(c, label).FontSize(18).Bold().TextColor(col)
		if active {
			text.BorderWidth(0, 0, 2, 0).BorderColor(p.accent)
		}
		if count > 0 {
			ui.Text(c, fmt.Sprintf("%d", count)).FontSize(11).TextColor(col)
		}
	})
}

func (a *app) nowPlayingQueueList(c *ui.Context, tracks []ytmusic.Track, start int) {
	p := a.pal
	if start >= len(tracks) {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, "暂无待播歌曲").FontSize(14).TextColor(p.faint)
		})
		return
	}

	ui.Scroll(c.Key("np-queue-scroll")).Vertical().TrackScroll(&a.npQueueScroll).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(4).Padding(0, 0, 24, 0).Children(func() {
			for i := start; i < len(tracks); i++ {
				idx := i
				t := tracks[i]
				a.npTrackRow(c, fmt.Sprintf("np-queue-%d", idx), t, func() {
					a.pl.PlayAt(idx)
				})
			}
		})
	})
}

func (a *app) npTrackRow(c *ui.Context, key string, t ytmusic.Track, onClick func()) {
	p := a.pal
	row := ui.Row(c.Key(key)).FillWidth().Height(52).Radius(8).Padding(0, 8).AlignItems(ui.Center).Gap(10).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		onClick()
	}
	row.Children(func() {
		a.coverImage(c, t.Thumbnail, 36, 6)
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, t.Title).FontSize(13).TextColor(p.text).SingleLine().Tooltip(t.Title)
			ui.Text(c, t.ArtistNames()).FontSize(11).TextColor(p.muted).SingleLine().Tooltip(t.ArtistNames())
		})
		if t.DurationMs > 0 {
			ui.Text(c, t.DurationString()).FontSize(11).TextColor(p.muted).Width(38)
		}
	})
}

func (a *app) nowPlayingRelated(c *ui.Context) {
	p := a.pal
	if !a.relatedLoaded && !a.relatedLoading {
		a.loadRelated()
	}
	if a.relatedLoading {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载相关推荐...").FontSize(13).TextColor(p.muted)
		})
		return
	}

	var trackShelf, playlistShelf *ytmusic.Section
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
		case "playlist":
			if playlistShelf == nil {
				playlistShelf = sec
			}
		}
	}
	if trackShelf == nil && playlistShelf == nil {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, "暂无相关推荐").FontSize(14).TextColor(p.faint)
		})
		return
	}

	ui.Scroll(c.Key("np-related-scroll")).Vertical().TrackScroll(&a.npRelatedScroll).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(20).Padding(0, 0, 24, 0).Children(func() {
			if trackShelf != nil {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, shelfTitle(trackShelf.Title, "你可能还喜欢")).FontSize(15).Bold().TextColor(p.text)
					var relatedTracks []ytmusic.Track
					for _, item := range trackShelf.Items {
						if item.Track != nil {
							relatedTracks = append(relatedTracks, *item.Track)
						}
					}
					for i, t := range relatedTracks {
						idx := i
						track := t
						a.npTrackRow(c, fmt.Sprintf("np-related-track-%d", idx), track, func() {
							a.pl.SetQueue(relatedTracks, idx)
						})
					}
				})
			}
			if playlistShelf != nil {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, shelfTitle(playlistShelf.Title, "推荐的播放列表")).FontSize(15).Bold().TextColor(p.text)
					panelW := a.npRightWidth()
					cols := 2
					if panelW >= 420 {
						cols = 3
					}
					const gap = float32(8)
					cardW := (panelW - 12 - gap*float32(cols-1)) / float32(cols)
					for i := 0; i < len(playlistShelf.Items); i += cols {
						ui.Row(c).FillWidth().Gap(gap).Children(func() {
							for j := 0; j < cols && i+j < len(playlistShelf.Items); j++ {
								idx := i + j
								a.npRelatedCard(c, fmt.Sprintf("np-related-card-%d", idx), playlistShelf.Items[idx], cardW)
							}
						})
					}
				})
			}
		})
	})
}

func shelfTitle(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}

func (a *app) npRelatedCard(c *ui.Context, key string, item ytmusic.CardItem, width float32) {
	p := a.pal
	card := ui.Column(c.Key(key)).Width(width).Gap(6).Cursor(ui.CursorPointer)
	if card.Clicked() {
		a.nowPlayingOpen = false
		a.closeVideo()
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
		a.coverImage(c, item.Thumbnail, width, 8)
		ui.Text(c, item.Title).FontSize(12).Bold().TextColor(p.text).MaxLines(2).Tooltip(item.Title)
		if item.Subtitle != "" {
			ui.Text(c, item.Subtitle).FontSize(11).TextColor(p.muted).SingleLine().Tooltip(item.Subtitle)
		}
	})
}

func (a *app) nowPlayingModeSwitch(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(30).Radius(15).Background(p.card).Padding(2).AlignItems(ui.Center).Children(func() {
		for _, opt := range []struct {
			key   string
			label string
			video bool
		}{
			{"np-mode-song", "歌曲", false},
			{"np-mode-video", "视频", true},
		} {
			opt := opt
			active := a.npVideoMode == opt.video
			bg := ui.Transparent
			col := p.muted
			if active {
				bg = p.text
				col = p.sidebar
			}
			btn := ui.Box(c.Key(opt.key)).Height(26).Radius(13).Background(bg).Padding(0, 12).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
			if btn.Clicked() {
				a.npVideoMode = opt.video
				if !opt.video {
					a.closeVideo()
				}
				c.Invalidate()
			}
			btn.Children(func() {
				t := ui.Text(c, opt.label).FontSize(12).TextColor(col)
				if active {
					t.Bold()
				}
			})
		}
	})
}

func (a *app) nowPlayingVideo(c *ui.Context) {
	p := a.pal
	ui.Box(c).Grow(1).FillWidth().Margin(24, 0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		a.ensureVideo(c)
		if a.npVideo == nil {
			ui.Column(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在准备视频...").FontSize(13).TextColor(p.muted)
			})
			return
		}
		ui.Video(c, a.npVideo).Fill().Fit(ui.Contain)
	})
}
