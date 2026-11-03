package ytmusic

import (
	"fmt"
	"strings"
	"time"
)

// SearchType determines the category of search results.
type SearchType int

const (
	SearchAll SearchType = iota
	SearchSongs
	SearchVideos
	SearchAlbums
	SearchArtists
	SearchPlaylists
	SearchPodcasts
	SearchPodcastEpisodes
)

func (s SearchType) String() string {
	switch s {
	case SearchSongs:
		return "歌曲"
	case SearchVideos:
		return "视频"
	case SearchAlbums:
		return "专辑"
	case SearchArtists:
		return "音乐人"
	case SearchPlaylists:
		return "歌单"
	case SearchPodcasts:
		return "播客"
	case SearchPodcastEpisodes:
		return "分集"
	default:
		return "全部"
	}
}

// Param returns the InnerTube search params string for filtering.
func (s SearchType) Param() string {
	switch s {
	case SearchSongs:
		return "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"
	case SearchVideos:
		return "EgWKAQIQAWoKEAkQBRAKEAMQBA%3D%3D"
	case SearchAlbums:
		return "EgWKAQIYAWoKEAkQBRAKEAMQBA%3D%3D"
	case SearchArtists:
		return "EgWKAQIgAWoKEAkQBRAKEAMQBA%3D%3D"
	case SearchPlaylists:
		return "EgWKAQIoAWoKEAkQBRAKEAMQBA%3D%3D"
	case SearchPodcasts:
		return "EgWKAQJQAWoQEBAQAxAFEBEQCRAEEBUQCg%3D%3D"
	case SearchPodcastEpisodes:
		return "EgWKAQJIAWoQEBAQAxAFEBEQCRAEEBUQCg%3D%3D"
	default:
		return ""
	}
}

// Thumbnail represents an image thumbnail.
type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// ArtistRef is a reference to an artist.
type ArtistRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// AlbumRef is a reference to an album.
type AlbumRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Track represents a playable music track or video.
type Track struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Artists    []ArtistRef `json:"artists"`
	Album      AlbumRef    `json:"album"`
	DurationMs int64       `json:"durationMs"`
	Thumbnail  string      `json:"thumbnail"`
	Thumbnails []Thumbnail `json:"thumbnails,omitempty"`
	Liked      bool        `json:"liked"`
	Explicit   bool        `json:"explicit"`
	Video      bool        `json:"video,omitempty"`
}

// ArtistNames formats the artist names as a comma-separated string.
func (t Track) ArtistNames() string {
	if len(t.Artists) == 0 {
		return ""
	}
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// DurationString returns the track duration formatted as mm:ss.
func (t Track) DurationString() string {
	if t.DurationMs <= 0 {
		return "0:00"
	}
	d := time.Duration(t.DurationMs) * time.Millisecond
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

// CardItem is an item shown in carousels or home sections.
type CardItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Thumbnail string `json:"thumbnail"`
	Type      string `json:"type"` // "track", "album", "playlist", "artist", "mood", "shortcut"
	Params    string `json:"params,omitempty"`
	Color     uint32 `json:"color,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Track     *Track `json:"track,omitempty"`
}

// Section is a group of items on Home, Explore, or Charts.
type Section struct {
	Title  string     `json:"title"`
	Items  []CardItem `json:"items"`
	Layout string     `json:"layout,omitempty"` // "moods" or "shortcuts" for Explore grids
}

// Category is a mood or genre page with its sections.
type Category struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Sections []Section `json:"sections"`
}

// ChartCountry is one choice in the charts country menu.
type ChartCountry struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ChartsPage is the charts page for a country, including its country menu.
type ChartsPage struct {
	Sections  []Section      `json:"sections"`
	Countries []ChartCountry `json:"countries"`
	Selected  string         `json:"selected"`
}

// PlaylistItem represents a playlist summary.
type PlaylistItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Thumbnail  string `json:"thumbnail"`
	TrackCount string `json:"trackCount"`
}

// Playlist represents a full playlist with tracks.
type Playlist struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Author       string  `json:"author"`
	Description  string  `json:"description"`
	Thumbnail    string  `json:"thumbnail"`
	TrackCount   int     `json:"trackCount"`
	Tracks       []Track `json:"tracks"`
	Continuation string  `json:"continuation,omitempty"`
}

// AlbumItem represents an album summary.
type AlbumItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Year      string `json:"year"`
	Thumbnail string `json:"thumbnail"`
	Type      string `json:"type"` // "album", "artist", "playlist", or "track"
	Track     *Track `json:"track,omitempty"`
}

// Album represents a full album with tracks.
type Album struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Artists     []ArtistRef `json:"artists"`
	Year        string      `json:"year"`
	Description string      `json:"description"`
	Thumbnail   string      `json:"thumbnail"`
	Tracks      []Track     `json:"tracks"`
}

// Artist represents a full artist profile.
type Artist struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Thumbnail   string      `json:"thumbnail"`
	Subscribers string      `json:"subscribers"`
	TopTracks   []Track     `json:"topTracks"`
	Albums      []AlbumItem `json:"albums"`
	Singles     []AlbumItem `json:"singles"`
	RadioID     string      `json:"radioId,omitempty"`
}

// SearchResult groups results by category.
type SearchResult struct {
	Tracks    []Track        `json:"tracks"`
	Artists   []ArtistRef    `json:"artists"`
	Albums    []AlbumItem    `json:"albums"`
	Playlists []PlaylistItem `json:"playlists"`
	Podcasts  []CardItem     `json:"podcasts,omitempty"`
}

// LyricLine represents a line of lyrics with optional timestamp.
type LyricLine struct {
	TimeMs int64  `json:"timeMs"`
	Text   string `json:"text"`
}

// Lyrics holds the lyrics of a track.
type Lyrics struct {
	Lines  []LyricLine `json:"lines"`
	Synced bool        `json:"synced"`
}

// Profile represents the authenticated user's profile.
type Profile struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
	Avatar string `json:"avatar"`
}

// StreamSegment represents a chunk in an HLS audio stream.
type StreamSegment struct {
	URI        string
	DurationMs int64
}

// StreamInfo contains audio playback information.
type StreamInfo struct {
	VideoID       string
	DirectURL     string
	Format        string // "mp4", "hls", "file"
	SampleRate    int
	Channels      int
	DurationMs    int64
	ContentLength int64
	// LocalPath is a downloaded file to play instead of a URL, and Sabr
	// downloads it while it plays.
	LocalPath   string
	Sabr        *SabrSession
	HLSPlaylist string
	Segments    []StreamSegment
}
