package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"

	"github.com/douglarek/melo/internal/ytmusic"
)

func trackCopyText(t ytmusic.Track) string {
	if artists := t.ArtistNames(); artists != "" {
		return t.Title + " - " + artists
	}
	return t.Title
}

func (a *app) trackCopyMenu(c *ui.Context, t ytmusic.Track) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item(locales.CopySongName()).Chosen() {
			c.WriteClipboard(t.Title)
		}
		if artists := t.ArtistNames(); artists != "" {
			if m.Item(locales.CopyArtist()).Chosen() {
				c.WriteClipboard(artists)
			}
		}
		m.Separator()
		if m.Item(locales.CopySongInformation()).Chosen() {
			c.WriteClipboard(trackCopyText(t))
		}
	}
}

func (a *app) cardCopyMenu(c *ui.Context, item ytmusic.CardItem) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item(locales.CopyTitle()).Chosen() {
			c.WriteClipboard(item.Title)
		}
		if item.Subtitle != "" {
			if m.Item(locales.CopySubtitle()).Chosen() {
				c.WriteClipboard(item.Subtitle)
			}
			m.Separator()
			if m.Item(locales.CopyInformation()).Chosen() {
				c.WriteClipboard(item.Title + " - " + item.Subtitle)
			}
		}
	}
}

func (a *app) playlistCopyMenu(c *ui.Context, title string) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item(locales.CopyPlaylistName()).Chosen() {
			c.WriteClipboard(title)
		}
	}
}
