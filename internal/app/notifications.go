package app

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) maybeNotifyTrack(t *ytmusic.Track) {
	if t == nil {
		a.notifyTrackID = ""
		return
	}
	if t.ID == a.notifyTrackID {
		return
	}
	a.notifyTrackID = t.ID
	if !a.settings.TrackNotifications || a.win == nil || a.win.IsFocused() {
		return
	}
	if !a.pl.Playing() && !a.pl.PendingPlay() {
		return
	}
	a.notifySeq++
	seq := a.notifySeq
	go a.notifyTrack(t, seq)
}

func (a *app) notifyTrack(t *ytmusic.Track, seq int) {
	icon := a.notificationIcon(t)
	if seq != a.notifySeq {
		return
	}
	n := mygo.NewNotification(a.notificationOptions(t, icon))
	n.OnClick(func() {
		a.showFromNotification()
		n.Close()
	})
	_ = n.Show()
}

// notificationOptions describes the notification of a track, whose cover is
// its Image: an attachment on macOS, the image hint on Linux and the
// balloon's large icon on Windows.
//
// It is not a Sender: that is the avatar of a person's message, which macOS
// shows only for a signed app with the communication entitlement, and which
// the other platforms ignore.
func (a *app) notificationOptions(t *ytmusic.Track, cover []byte) mygo.NotificationOptions {
	return mygo.NotificationOptions{
		// One id, so that the next track replaces the notification of the
		// one before rather than stacking beside it.
		ID:     "melo-track",
		Title:  t.Title,
		Body:   t.ArtistNames(),
		Image:  cover,
		Silent: true,
	}
}

func (a *app) showFromNotification() {
	a.post(func() {
		if a.win == nil {
			return
		}
		a.win.Show()
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Focus()
		mygo.App.Focus()
	})
}

// notificationIcon waits briefly for the artwork cache to decode the cover.
func (a *app) notificationIcon(t *ytmusic.Track) []byte {
	if t.Thumbnail == "" {
		return nil
	}
	for i := 0; i < 20; i++ {
		art := a.art.get(t.Thumbnail, artNotification)
		if art != nil {
			if art.failed || len(art.data) == 0 {
				return nil
			}
			return append([]byte(nil), art.data...)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}
