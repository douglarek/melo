package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/douglarek/melo/internal/player"
	"github.com/douglarek/melo/internal/ytmusic"
	"github.com/douglarek/melo/locales"
)

type libraryCache struct {
	Playlists []ytmusic.PlaylistItem `json:"playlists"`
	Items     []ytmusic.CardItem     `json:"items,omitempty"`
	Liked     *ytmusic.Playlist      `json:"liked,omitempty"`
}

var userDataDir = func() (string, error) {
	return mygo.App.Path(mygo.PathUserData)
}

func userDataFile(name string) (string, error) {
	dir, err := userDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *app) savePlayerSession() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if a.pl == nil {
		return
	}
	sess, ok := a.pl.Session()
	if !ok {
		return
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return
	}
	path, err := userDataFile("player.json")
	if err != nil {
		return
	}
	_ = writeFileAtomic(path, data)
}

func (a *app) scheduleSavePlayerSession() {
	a.saveMu.Lock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(750*time.Millisecond, a.savePlayerSession)
	a.saveMu.Unlock()
}

func (a *app) restorePlayerSession() {
	path, err := userDataFile("player.json")
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var sess player.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return
	}
	markSavedPodcasts(sess.Queue)
	a.pl.Restore(sess)
}

// markSavedPodcasts fills the kind of the episodes a session saved before a
// track carried it. YouTube serves a podcast episode's art through its
// ingestion proxy, which is what such a queue still holds.
func markSavedPodcasts(tracks []ytmusic.Track) {
	for i := range tracks {
		if strings.Contains(tracks[i].Thumbnail, "youtube-podcasts-ingestion-proxy") {
			tracks[i].Podcast = true
		}
	}
}

func (a *app) saveLibraryCache() {
	liked := &ytmusic.Playlist{ID: "LM", Title: locales.LikedMusic()}
	if pl := a.playlists["LM"]; pl != nil {
		*liked = *pl
	}
	liked.Tracks = append([]ytmusic.Track(nil), a.liked...)
	liked.TrackCount = len(a.liked)

	cache := libraryCache{
		Playlists: a.libraryPlaylists,
		Items:     a.libraryItems,
		Liked:     liked,
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	path, err := userDataFile("library.json")
	if err != nil {
		return
	}
	_ = writeFileAtomic(path, data)
}

func (a *app) restoreLibraryCache() {
	path, err := userDataFile("library.json")
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cache libraryCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}
	if len(cache.Playlists) > 0 {
		a.libraryPlaylists = cache.Playlists
	}
	if len(cache.Items) > 0 {
		a.libraryItems = cache.Items
	}
	if cache.Liked != nil {
		a.playlists["LM"] = cache.Liked
		if len(cache.Liked.Tracks) > 0 {
			a.liked = append([]ytmusic.Track(nil), cache.Liked.Tracks...)
		}
	}
}

func (a *app) deleteLibraryCache() {
	path, err := userDataFile("library.json")
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
