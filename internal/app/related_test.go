package app

import (
	"testing"

	"github.com/douglarek/melo/internal/ytmusic"
	"github.com/douglarek/melo/locales"
)

// A show's other episodes are what a podcast has to recommend: the one
// playing is not repeated.
func TestOtherEpisodesSkipsThePlayingOne(t *testing.T) {
	tracks := []ytmusic.Track{
		{ID: "ep1", Title: "第 1 期", Podcast: true},
		{ID: "ep2", Title: "第 2 期", Podcast: true},
		{ID: "ep3", Title: "第 3 期", Podcast: true},
	}
	sec := otherEpisodes("ep2", tracks)
	if sec == nil {
		t.Fatal("no section for a show with other episodes")
	}
	if sec.Title != locales.MoreEpisodes() {
		t.Fatalf("title = %q, want %q", sec.Title, locales.MoreEpisodes())
	}
	if len(sec.Items) != 2 || sec.Items[0].ID != "ep1" || sec.Items[1].ID != "ep3" {
		t.Fatalf("items = %+v", sec.Items)
	}
	for _, item := range sec.Items {
		if item.Type != "track" || item.Track == nil {
			t.Fatalf("item %+v is not a playable track", item)
		}
	}
}

// A show can hold hundreds of episodes; the pane lists every row it is given.
func TestOtherEpisodesCapsTheList(t *testing.T) {
	tracks := make([]ytmusic.Track, 40)
	for i := range tracks {
		tracks[i] = ytmusic.Track{ID: "ep" + string(rune('a'+i%26)), Title: "第 " + string(rune('a'+i%26)) + " 期", Podcast: true}
	}
	sec := otherEpisodes("", tracks)
	if sec == nil || len(sec.Items) != relatedEpisodeLimit {
		t.Fatalf("items = %d, want %d", len(sec.Items), relatedEpisodeLimit)
	}
}

// An episode with no other episode to offer gets no section.
func TestOtherEpisodesWithoutOthers(t *testing.T) {
	if sec := otherEpisodes("ep1", []ytmusic.Track{{ID: "ep1"}}); sec != nil {
		t.Fatalf("section = %+v, want none", sec)
	}
}

// The show an episode plays under is named by its artist rows, whose page is
// a show's.
func TestPodcastShowID(t *testing.T) {
	episode := ytmusic.Track{ID: "ep1", Podcast: true, Artists: []ytmusic.ArtistRef{
		{Name: "相声汇", ID: "MPSPPLshow"},
	}}
	if got := podcastShowID(episode); got != "MPSPPLshow" {
		t.Fatalf("show id = %q", got)
	}
	song := ytmusic.Track{ID: "t1", Artists: []ytmusic.ArtistRef{{Name: "周杰伦", ID: "UCartist"}}}
	if got := podcastShowID(song); got != "" {
		t.Fatalf("a song named a show: %q", got)
	}
}
