package app

import (
	"github.com/egoist/mygo/ui"

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
		if m.Item("复制歌曲名").Chosen() {
			c.WriteClipboard(t.Title)
		}
		if artists := t.ArtistNames(); artists != "" {
			if m.Item("复制艺人").Chosen() {
				c.WriteClipboard(artists)
			}
		}
		m.Separator()
		if m.Item("复制歌曲信息").Chosen() {
			c.WriteClipboard(trackCopyText(t))
		}
	}
}

func (a *app) cardCopyMenu(c *ui.Context, item ytmusic.CardItem) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("复制标题").Chosen() {
			c.WriteClipboard(item.Title)
		}
		if item.Subtitle != "" {
			if m.Item("复制副标题").Chosen() {
				c.WriteClipboard(item.Subtitle)
			}
			m.Separator()
			if m.Item("复制信息").Chosen() {
				c.WriteClipboard(item.Title + " - " + item.Subtitle)
			}
		}
	}
}

func (a *app) playlistCopyMenu(c *ui.Context, title string) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("复制歌单名称").Chosen() {
			c.WriteClipboard(title)
		}
	}
}
