package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
	"github.com/douglarek/melo/locales"
)

func (a *app) loadRelated() {
	cur := a.pl.Current()
	if cur == nil {
		return
	}
	if a.relatedVideoID == cur.ID && (a.relatedLoaded || a.relatedLoading) {
		return
	}

	a.relatedVideoID = cur.ID
	a.relatedSections = nil
	a.relatedLoading = true
	a.relatedLoaded = false
	a.npRelatedScroll = ui.ScrollState{}

	// A show whose related page is empty still has episodes to offer: keep
	// the ones this page already holds, and let the fetch take the show when
	// none were opened yet.
	showID := podcastShowID(*cur)
	var known []ytmusic.Track
	if showID != "" {
		if show := a.playlists[showID]; show != nil {
			known = append([]ytmusic.Track(nil), show.Tracks...)
		}
	}

	go func(videoID, showID string, known []ytmusic.Track) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		sections, err := ytmusic.DefaultClient.GetRelated(ctx, videoID)
		if err == nil && len(sections) == 0 && showID != "" {
			tracks := known
			if len(tracks) == 0 {
				if show, err := ytmusic.DefaultClient.BrowsePlaylist(ctx, showID); err == nil && show != nil {
					tracks = show.Tracks
				}
			}
			if sec := otherEpisodes(videoID, tracks); sec != nil {
				sections = []ytmusic.Section{*sec}
			}
		}
		a.post(func() {
			if a.relatedVideoID != videoID {
				return
			}
			a.relatedLoading = false
			a.relatedLoaded = true
			if err == nil {
				a.relatedSections = sections
			}
		})
	}(cur.ID, showID, known)
}

// relatedEpisodeLimit keeps the pane light: a show can hold hundreds, and the
// pane lists every row it is given.
const relatedEpisodeLimit = 20

// podcastShowID is the show an episode plays under, when its row names it.
func podcastShowID(t ytmusic.Track) string {
	for _, artist := range t.Artists {
		if ytmusic.IsPodcastShowID(artist.ID) {
			return artist.ID
		}
	}
	return ""
}

// otherEpisodes lists a show's episodes but the one playing, which is what a
// podcast has to recommend: YouTube keeps an unlisted show's related page
// empty, and the rest of the show is what a listener wants next.
func otherEpisodes(currentID string, tracks []ytmusic.Track) *ytmusic.Section {
	items := make([]ytmusic.CardItem, 0, min(len(tracks), relatedEpisodeLimit))
	for i := range tracks {
		if len(items) == relatedEpisodeLimit {
			break
		}
		if tracks[i].ID == currentID {
			continue
		}
		t := tracks[i]
		items = append(items, ytmusic.CardItem{
			ID:        t.ID,
			Title:     t.Title,
			Subtitle:  t.ArtistNames(),
			Thumbnail: t.Thumbnail,
			Type:      "track",
			Track:     &t,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &ytmusic.Section{Title: locales.MoreEpisodes(), Items: items}
}
