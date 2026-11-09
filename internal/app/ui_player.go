package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"

	"github.com/douglarek/melo/internal/player"
)

func (a *app) playerBar(c *ui.Context) {
	p := a.pal
	cur := a.pl.Current()
	loading := a.pl.Loading()
	playing := a.pl.Playing()
	active := a.pl.Active()

	// Redraw while loading, and when the next second of the track begins.
	if loading {
		c.After(50 * time.Millisecond)
	} else if playing {
		pos := a.pl.Position()
		rem := time.Second - (pos % time.Second)
		if rem < 20*time.Millisecond {
			rem = time.Second
		}
		c.After(rem)
	}

	ui.Row(c).Fill().Padding(0, 16).AlignItems(ui.Center).Gap(12).Children(func() {
		// 1. Left: Track Info & Like button (flexible width, shrinks cleanly without pushing controls)
		ui.Row(c).Grow(1).Basis(0).Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(10).Children(func() {
			if cur != nil {
				// Cover thumbnail
				cover := ui.Box(c.Key("np-cover-thumb")).Size(42, 42).Radius(6).Cursor(ui.CursorPointer).Shrink(0)
				if cover.Clicked() {
					a.nowPlayingOpen = !a.nowPlayingOpen
				}
				cover.Children(func() {
					a.coverImage(c, cur.Thumbnail, 42, 6)
				})

				// Title & Artist
				info := ui.Column(c).Shrink(1).MinWidth(0).Gap(2).ContextMenu(a.trackCopyMenu(c, *cur))
				info.Children(func() {
					titleText := ui.Text(c, cur.Title).FontSize(13).Bold().TextColor(p.text).Cursor(ui.CursorPointer).SingleLine().Tooltip(cur.Title)
					if titleText.Clicked() {
						a.nowPlayingOpen = !a.nowPlayingOpen
					}

					a.artistLinks(c, *cur, p.muted, p.text).SingleLine().Tooltip(cur.ArtistNames())
				})

				// Like button
				liked := a.isLiked(cur.ID)
				likeLabel := locales.LikePrefix() + cur.Title
				if liked {
					likeLabel = locales.UnlikePrefix() + cur.Title
				}
				likeBtn := ui.Box(c.Key("player-like-btn")).Label(likeLabel).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
				if likeBtn.Hovered() {
					likeBtn.Background(p.hover)
				}
				if likeBtn.Clicked() {
					a.toggleLike(*cur)
					c.Invalidate()
				}
				likeBtn.Children(func() {
					if liked {
						ui.Icon(c, iconHeartFill).Size(16, 16).TextColor(p.accent)
					} else {
						ui.Icon(c, iconHeart).Size(16, 16).TextColor(p.muted)
					}
				})
			} else {
				ui.Box(c).Width(1)
			}
		})

		// 2. Center: Controls & Timeline Scrubbing (responsive width, adapts to window size)
		ui.Column(c).Grow(2).Shrink(1).MinWidth(260).AlignItems(ui.Center).Gap(4).Children(func() {
			// Buttons
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				// Shuffle
				shufBtn := ui.Box(c.Key("ctl-shuffle")).Size(26, 26).Radius(13).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if shufBtn.Hovered() {
					shufBtn.Background(p.hover)
				}
				if shufBtn.Clicked() {
					a.pl.SetShuffle(!a.pl.Shuffle())
				}
				shufBtn.Children(func() {
					col := p.muted
					if a.pl.Shuffle() {
						col = p.accent
					}
					ui.Icon(c, iconShuffle).Size(15, 15).TextColor(col)
				})

				// Prev
				prevBtn := ui.Box(c.Key("ctl-prev")).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if prevBtn.Hovered() {
					prevBtn.Background(p.hover)
				}
				if prevBtn.Clicked() {
					a.pl.Prev()
				}
				prevBtn.Children(func() {
					ui.Icon(c, iconPrev).Size(16, 16).TextColor(p.text)
				})

				// Play/Pause
				playLabel := locales.Play()
				if active {
					playLabel = locales.Pause()
				}
				playBtn := ui.Box(c.Key("ctl-play-pause")).Label(playLabel).Size(34, 34).Radius(17).Background(p.text).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if playBtn.Hovered() {
					playBtn.Opacity(0.9)
				}
				if playBtn.Clicked() {
					a.pl.TogglePlay()
				}
				playBtn.Children(func() {
					iconCol := p.sidebar
					if active {
						ui.Icon(c, iconPause).Size(16, 16).TextColor(iconCol)
					} else {
						ui.Icon(c, iconPlay).Size(16, 16).TextColor(iconCol)
					}
				})

				// Next
				nextBtn := ui.Box(c.Key("ctl-next")).Size(28, 28).Radius(14).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if nextBtn.Hovered() {
					nextBtn.Background(p.hover)
				}
				if nextBtn.Clicked() {
					a.pl.Next()
				}
				nextBtn.Children(func() {
					ui.Icon(c, iconNext).Size(16, 16).TextColor(p.text)
				})

				// Repeat
				repBtn := ui.Box(c.Key("ctl-repeat")).Size(26, 26).Radius(13).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				if repBtn.Hovered() {
					repBtn.Background(p.hover)
				}
				if repBtn.Clicked() {
					a.pl.ToggleRepeat()
				}
				repBtn.Children(func() {
					col := p.muted
					icon := iconRepeat
					if a.pl.Repeat() == player.RepeatOne {
						col = p.accent
						icon = iconRepeat1
					} else if a.pl.Repeat() == player.RepeatAll {
						col = p.accent
					}
					ui.Icon(c, icon).Size(15, 15).TextColor(col)
				})
			})

			// Progress bar row (FillWidth with MaxWidth 420, adapts to available space)
			pos := a.pl.Position()
			dur := a.pl.Duration()
			posSec := pos.Seconds()
			durSec := dur.Seconds()
			if durSec <= 0 {
				durSec = 1
			}

			ui.Row(c).FillWidth().MaxWidth(420).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, formatTime(pos)).FontSize(11).TextColor(p.muted).Width(36)
				if loading {
					ui.Row(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, locales.Loading()).FontSize(11).TextColor(p.muted)
					})
				} else {
					slider := ui.Slider(c, &posSec, 0, durSec).Grow(1)
					if slider.Changed() {
						a.pl.Seek(time.Duration(posSec * float64(time.Second)))
					}
				}
				ui.Text(c, formatTime(dur)).FontSize(11).TextColor(p.muted).Width(36)
			})
		})

		// 3. Right: Volume & Now Playing Buttons (Compact, Shrink(0), never cut off)
		ui.Row(c).Width(196).Shrink(0).Justify(ui.End).AlignItems(ui.Center).Gap(6).Children(func() {
			vol := a.pl.Volume()
			volIcon := iconVolume
			if vol == 0 {
				volIcon = iconMute
			} else if vol < 0.5 {
				volIcon = iconVolumeLow
			}
			ui.Icon(c, volIcon).Size(16, 16).TextColor(p.muted).Shrink(0)

			volSlider := ui.Slider(c, &vol, 0, 1.0).Width(64).Shrink(0)
			if volSlider.Changed() {
				a.pl.SetVolume(vol)
			}

			// Queue Button
			queueBtn := ui.Box(c.Key("ctl-queue-btn")).Label(locales.OpenTheQueue()).Size(28, 28).Radius(14).
				AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
			if queueBtn.Hovered() || (a.nowPlayingOpen && a.npPane == paneQueue) {
				queueBtn.Background(p.hover)
			}
			if queueBtn.Clicked() {
				a.choosePane(paneQueue)
				a.nowPlayingOpen = true
			}
			queueBtn.Children(func() {
				col := p.muted
				if a.nowPlayingOpen && a.npPane == paneQueue {
					col = p.accent
				}
				ui.Icon(c, iconQueue).Size(16, 16).TextColor(col)
			})

			// Lyrics / Now Playing Button, which a podcast has no lyrics for.
			if cur == nil || !cur.Podcast {
				npBtn := ui.Box(c.Key("ctl-lyrics-btn")).Label(locales.OpenTheLyrics()).Size(28, 28).Radius(14).
					AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Shrink(0)
				if a.nowPlayingOpen && a.showingPane() == paneLyrics {
					npBtn.Background(p.hover)
				}
				if npBtn.Hovered() {
					npBtn.Background(p.hover)
				}
				if npBtn.Clicked() {
					a.choosePane(paneLyrics)
					a.nowPlayingOpen = true
				}
				npBtn.Children(func() {
					col := p.muted
					if a.nowPlayingOpen && a.showingPane() == paneLyrics {
						col = p.accent
					}
					ui.Icon(c, iconLyrics).Size(16, 16).TextColor(col)
				})
			}
		})
	})
}
