package ytmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// artistPageJSON is an artist page whose songs shelf previews one track and
// links to the full list through its more button.
const artistPageJSON = `{
	"header": {"musicImmersiveHeaderRenderer": {
		"title": {"runs": [{"text": "古典樂精選"}]},
		"subscriptionButton": {"subscribeButtonRenderer": {"subscriberCountText": {"runs": [{"text": "437"}]}}}
	}},
	"contents": {
		"singleColumnBrowseResultsRenderer": {
			"tabs": [{
				"tabRenderer": {
					"content": {
						"sectionListRenderer": {
							"contents": [{
								"musicShelfRenderer": {
									"title": {"runs": [{"text": "热门歌曲"}]},
									"contents": [{
										"musicResponsiveListItemRenderer": {
											"flexColumns": [
												{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "月光奏鳴曲"}]}}},
												{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
													{"text": "古典樂精選", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCDYxf07bnfqYae1KfSBHQAA"}}}
												]}}}
											],
											"playlistItemData": {"videoId": "preview1"}
										}
									}],
									"bottomEndpoint": {"browseEndpoint": {
										"browseId": "VLOLAK5uy_songs",
										"params": "ggMCCAI%3D",
										"browseEndpointContextSupportedConfigs": {
											"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_PLAYLIST"}
										}
									}}
								}
							}]
						}
					}
				}
			}]
		}
	}
}`

func TestBrowseArtistCapturesSongsShelfEndpoint(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(artistPageJSON)),
		}, nil
	})}

	art, err := c.BrowseArtist(context.Background(), "UCDYxf07bnfqYae1KfSBHQAA")
	if err != nil {
		t.Fatal(err)
	}
	if art.SongsID != "VLOLAK5uy_songs" || art.SongsParams != "ggMCCAI%3D" {
		t.Fatalf("songs endpoint = %q %q, want the shelf's more button", art.SongsID, art.SongsParams)
	}
}

// artistSongsPageJSON is the first page of an artist's full song list.
const artistSongsPageJSON = `{
	"contents": {
		"singleColumnBrowseResultsRenderer": {
			"tabs": [{
				"tabRenderer": {
					"content": {
						"sectionListRenderer": {
							"contents": [{
								"musicPlaylistShelfRenderer": {
									"playlistId": "OLAK5uy_songs",
									"contents": [
										{"musicResponsiveListItemRenderer": {
											"flexColumns": [
												{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "月光奏鳴曲"}]}}},
												{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
													{"text": "古典樂精選", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCDYxf07bnfqYae1KfSBHQAA"}}}
												]}}}
											],
											"fixedColumns": [{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "6:14"}]}}}],
											"playlistItemData": {"videoId": "song1"}
										}},
										{"continuationItemRenderer": {"continuationEndpoint": {"continuationCommand": {
											"token": "page2", "request": "CONTINUATION_REQUEST_TYPE_BROWSE"
										}}}}
									]
								}
							}]
						}
					}
				}
			}]
		}
	}
}`

// artistSongsNextPageJSON is what the continuation token above answers with.
const artistSongsNextPageJSON = `{
	"continuationContents": {
		"musicPlaylistShelfContinuation": {
			"contents": [{"musicResponsiveListItemRenderer": {
				"flexColumns": [
					{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "致愛麗絲"}]}}},
					{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": []}}}
				],
				"playlistItemData": {"videoId": "song2"}
			}}]
		}
	}
}`

func TestBrowseArtistSongsPagesThroughTheSongList(t *testing.T) {
	c := NewClient()
	var bodies []string
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		bodies = append(bodies, body)
		page := artistSongsPageJSON
		if strings.Contains(body, "page2") {
			page = artistSongsNextPageJSON
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(page)),
		}, nil
	})}

	songs, err := c.BrowseArtistSongs(context.Background(), "VLOLAK5uy_songs", "ggMCCAI%3D")
	if err != nil {
		t.Fatal(err)
	}
	if len(songs.Tracks) != 1 || songs.Tracks[0].ID != "song1" {
		t.Fatalf("first page tracks = %+v", songs.Tracks)
	}
	if songs.Tracks[0].DurationMs != 374000 {
		t.Errorf("track duration = %d, want 374000", songs.Tracks[0].DurationMs)
	}
	if songs.Continuation != "page2" {
		t.Fatalf("continuation = %q, want page2", songs.Continuation)
	}
	if songs.TrackCount != 0 {
		t.Errorf("paged list reported a fixed count of %d", songs.TrackCount)
	}

	// The browse body carries the shelf's params: without them YouTube Music
	// answers with an album-style two-column page instead of the song list.
	var sent map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["params"] != "ggMCCAI%3D" {
		t.Errorf("browse params = %v, want the shelf's params", sent["params"])
	}

	more, next, err := c.PlaylistTracks(context.Background(), songs.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 1 || more[0].ID != "song2" || next != "" {
		t.Fatalf("second page = %+v, next = %q", more, next)
	}
}

func TestBrowseArtistHandlesMissingHeaderAndTitles(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"contents": {
					"singleColumnBrowseResultsRenderer": {
						"tabs": [{
							"tabRenderer": {
								"content": {
									"sectionListRenderer": {
										"contents": [
											{"musicShelfRenderer": {"contents": []}},
											{"musicCarouselShelfRenderer": {"contents": []}}
										]
									}
								}
							}
						}]
					}
				}
			}`)),
		}, nil
	})}

	art, err := c.BrowseArtist(context.Background(), "UCtest")
	if err != nil {
		t.Fatal(err)
	}
	if art.ID != "UCtest" || art.Name != "" || len(art.TopTracks) != 0 || len(art.Albums) != 0 || len(art.Singles) != 0 {
		t.Fatalf("artist = %+v", art)
	}
	if art.SongsID != "" || art.SongsParams != "" {
		t.Fatalf("songs endpoint = %q %q, want empty without a more button", art.SongsID, art.SongsParams)
	}
}
