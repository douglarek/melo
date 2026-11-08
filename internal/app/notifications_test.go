package app

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo"

	"github.com/douglarek/melo/internal/ytmusic"
)

// The cover goes in the notification's Image, which every platform shows:
// the Sender it was in is the avatar of a message, which macOS only shows
// to apps with the communication entitlement, and the other platforms drop.
func TestNotificationCarriesTheCover(t *testing.T) {
	a := newTestApp(t)
	track := &ytmusic.Track{ID: "v1", Title: "爱情抗体", Artists: []ytmusic.ArtistRef{{Name: "许慧欣"}}}
	cover := []byte("\x89PNG\r\n\x1a\n not a real png")

	opts := a.notificationOptions(track, cover)
	if !bytes.Equal(opts.Image, cover) {
		t.Fatalf("Image = %q, want the cover", opts.Image)
	}
	if opts.Sender != nil {
		t.Fatalf("the cover is not a communication sender: %+v", opts.Sender)
	}
	if opts.ID != "melo-track" {
		t.Fatalf("ID = %q, want one id for every track, which replaces the last notification", opts.ID)
	}
	if opts.Title != "爱情抗体" || opts.Body != "许慧欣" {
		t.Fatalf("Title/Body = %q/%q", opts.Title, opts.Body)
	}
	if !opts.Silent {
		t.Fatal("the notification should not ring")
	}
}

// A track without a cover still gets its notification.
func TestNotificationWithoutCover(t *testing.T) {
	a := newTestApp(t)
	track := &ytmusic.Track{ID: "v2", Title: "爱情抗体"}

	opts := a.notificationOptions(track, nil)
	if len(opts.Image) != 0 {
		t.Fatalf("Image = %q, want none", opts.Image)
	}
	var _ mygo.NotificationOptions = opts
}
