package app

import (
	"time"

	"github.com/egoist/mygo"
)

// mediaCommands are what the media keys and the system's media controls may
// ask of the app.
const mediaCommands = mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext |
	mygo.MediaPrevious | mygo.MediaSeek

// setupMediaKeys has the media keys of keyboards and headsets, and the
// system's own media controls, drive the player.
func (a *app) setupMediaKeys() {
	mygo.NowPlaying.OnCommand(a.handleMediaCommand)
}

func (a *app) handleMediaCommand(cmd mygo.MediaCommand, pos time.Duration) {
	switch cmd {
	case mygo.MediaPlay:
		a.pl.Play()
	case mygo.MediaPause:
		a.pl.Pause()
	case mygo.MediaNext:
		a.pl.Next()
	case mygo.MediaPrevious:
		a.pl.Prev()
	case mygo.MediaSeek:
		a.pl.Seek(pos)
	}
}

// publishNowPlaying shows what plays where the system shows media, so its
// controls and the media keys reach the app.
func (a *app) publishNowPlaying() {
	cur := a.pl.Current()
	if cur == nil {
		mygo.NowPlaying.Clear()
		a.mediaTrackID, a.mediaArtwork, a.mediaDuration = "", "", 0
		return
	}

	pos := a.pl.Position()
	dur := a.pl.Duration()
	if dur <= 0 && cur.DurationMs > 0 {
		dur = time.Duration(cur.DurationMs) * time.Millisecond
	}
	state := mygo.PlaybackPaused
	if a.pl.Playing() {
		state = mygo.PlaybackPlaying
	}

	art, settled := a.mediaArtworkBytes(cur.Thumbnail)
	artURL := a.mediaArtwork
	if settled {
		artURL = cur.Thumbnail
	}
	if a.mediaTrackID == cur.ID && a.mediaArtwork == artURL && a.mediaDuration == dur {
		mygo.NowPlaying.SetPlayback(state, pos)
		return
	}

	a.mediaTrackID, a.mediaArtwork, a.mediaDuration = cur.ID, artURL, dur
	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    cur.Title,
		Artist:   cur.ArtistNames(),
		Album:    cur.Album.Name,
		Artwork:  art,
		Duration: dur,
		Position: pos,
		State:    state,
		Commands: mediaCommands,
	})
}

// syncMediaArtwork shows the artwork of what plays once it is loaded, which
// the system's media controls show beside the track.
func (a *app) syncMediaArtwork() {
	cur := a.pl.Current()
	if cur == nil || cur.Thumbnail == "" || a.mediaArtwork == cur.Thumbnail {
		return
	}
	if _, settled := a.mediaArtworkBytes(cur.Thumbnail); settled {
		a.publishNowPlaying()
	}
}

// mediaArtworkBytes returns the cover of a track as the system takes it,
// and whether it is known already: the cover is loaded when it is not.
func (a *app) mediaArtworkBytes(u string) ([]byte, bool) {
	if u == "" || a.art == nil {
		return nil, true
	}
	for _, side := range []int{artLargeSide, artMediumSide, artSmallSide} {
		art := a.art.cached(u, side)
		if art == nil {
			continue
		}
		if len(art.data) > 0 {
			return art.data, true
		}
		if art.failed {
			return nil, true
		}
	}
	a.art.get(u, artLargeSide)
	return nil, false
}
