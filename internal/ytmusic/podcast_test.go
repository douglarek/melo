package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPodcastStreamReadsTheEnclosure checks the itag 15 URL a podcast
// episode's player response names: the episode's own file, not YouTube's.
func TestPodcastStreamReadsTheEnclosure(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		format string
		url    string
	}{
		{
			name: "mp3",
			raw: `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {"formats": [{
					"itag": 15,
					"mimeType": "audio/mpeg",
					"contentLength": "8682961",
					"approxDurationMs": "2170000",
					"url": "https://anchor.fm/s/2653df74/podcast/play/87407183/episode.mp3"
				}]}
			}`,
			format: "mp3",
			url:    "https://anchor.fm/s/2653df74/podcast/play/87407183/episode.mp3",
		},
		{
			name: "m4a",
			raw: `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {"formats": [{
					"itag": 15,
					"mimeType": "audio/x-m4a",
					"contentLength": "32618321",
					"approxDurationMs": "2016000",
					"url": "https://anchor.fm/s/2653df74/podcast/play/87837482/episode.m4a"
				}]}
			}`,
			format: "mp4",
			url:    "https://anchor.fm/s/2653df74/podcast/play/87837482/episode.m4a",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &resp); err != nil {
				t.Fatal(err)
			}
			info, err := podcastStream("episode", resp)
			if err != nil {
				t.Fatalf("podcastStream: %v", err)
			}
			if info.Format != tc.format {
				t.Errorf("format = %q, want %q", info.Format, tc.format)
			}
			if info.DirectURL != tc.url {
				t.Errorf("url = %q, want %q", info.DirectURL, tc.url)
			}
			if info.DurationMs == 0 || info.ContentLength == 0 {
				t.Errorf("duration = %d, length = %d", info.DurationMs, info.ContentLength)
			}
			if info.Fallback {
				t.Error("a podcast enclosure is not a quality fallback")
			}
		})
	}
}

// TestPodcastStreamRefusesWhatIsNotAnEnclosure checks the responses that
// carry no episode file, which are the ones a track YouTube does not serve
// here answers with.
func TestPodcastStreamRefusesWhatIsNotAnEnclosure(t *testing.T) {
	for _, raw := range []string{
		`{"playabilityStatus": {"status": "ERROR"}}`,
		`{"playabilityStatus": {"status": "OK"}}`,
		`{"playabilityStatus": {"status": "OK"}, "streamingData": {"formats": [
			{"itag": 18, "mimeType": "video/mp4", "url": "https://rr2---sn-a5meknzl.googlevideo.com/videoplayback"}
		]}}`,
		`{"playabilityStatus": {"status": "OK"}, "streamingData": {"formats": [
			{"itag": 140, "mimeType": "audio/mp4", "url": "https://rr2---sn-a5meknzl.googlevideo.com/videoplayback"}
		]}}`,
	} {
		var resp map[string]any
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatal(err)
		}
		if _, err := podcastStream("episode", resp); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("podcastStream(%s) = %v, want ErrUnavailable", raw, err)
		}
	}
}

// podcastShowPageJSON is a show's page: the header names the show and its
// channel, and the shelf lists episodes, which are RSS hosted.
const podcastShowPageJSON = `{
	"contents": {
		"twoColumnBrowseResultsRenderer": {
			"tabs": [{
				"tabRenderer": {
					"content": {
						"sectionListRenderer": {
							"contents": [{
								"musicResponsiveHeaderRenderer": {
									"title": {"runs": [{"text": "相声汇"}]},
									"straplineTextOne": {"runs": [{"text": "夕阳孤影"}]},
									"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
										{"url": "https://example/show.jpg", "width": 400, "height": 400}
									]}}}
								}
							}]
						}
					}
				}
			}],
			"secondaryContents": {
				"sectionListRenderer": {
					"contents": [{
						"musicShelfRenderer": {
							"contents": [{
								"musicMultiRowListItemRenderer": {
									"title": {"runs": [{"text": "《生活.COM》"}]},
									"secondTitle": {"runs": [{"text": "相声汇"}]},
									"playbackProgress": {"musicPlaybackProgressRenderer": {"durationText": {"runs": [{"text": "35:00"}]}}},
									"onTap": {"watchEndpoint": {
										"videoId": "episode1",
										"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {
											"musicVideoType": "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
										}}
									}}
								}
							}]
						}
					}]
				}
			}
		}
	}
}`

func browsePlaylistWith(t *testing.T, raw string) *Playlist {
	t.Helper()
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(raw)),
		}, nil
	})}
	pl, err := c.BrowsePlaylist(context.Background(), "MPSPPLshow")
	if err != nil {
		t.Fatal(err)
	}
	return pl
}

// A show's page is browsed like a playlist, but its rows are episodes: the
// page must know it is a podcast, not a playlist of songs.
func TestBrowsePlaylistMarksAShowAsAPodcast(t *testing.T) {
	pl := browsePlaylistWith(t, podcastShowPageJSON)
	if !pl.Podcast {
		t.Fatalf("a show's page came back as a playlist: %+v", pl)
	}
	if pl.Title != "相声汇" || pl.Author != "夕阳孤影" {
		t.Fatalf("show = %q by %q", pl.Title, pl.Author)
	}
	if len(pl.Tracks) != 1 || pl.Tracks[0].ID != "episode1" || !pl.Tracks[0].Podcast {
		t.Fatalf("episodes = %+v", pl.Tracks)
	}
}

// podcastPlaylistJSON is a playlist, not a show's page, whose rows are
// episodes all the same.
const podcastPlaylistJSON = `{
	"contents": {
		"singleColumnBrowseResultsRenderer": {
			"tabs": [{
				"tabRenderer": {
					"content": {
						"sectionListRenderer": {
							"contents": [{
								"musicPlaylistShelfRenderer": {
									"contents": [{
										"musicMultiRowListItemRenderer": {
											"title": {"runs": [{"text": "第 1 期"}]},
											"onTap": {"watchEndpoint": {
												"videoId": "ep",
												"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {
													"musicVideoType": "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
												}}
											}}
										}
									}]
								}
							}]
						}
					}
				}
			}]
		}
	}
}`

// A playlist that holds episodes is a podcast to listen to as well.
func TestBrowsePlaylistMarksEpisodesAsAPodcast(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(podcastPlaylistJSON)),
		}, nil
	})}
	pl, err := c.BrowsePlaylist(context.Background(), "VLPLepisodes")
	if err != nil {
		t.Fatal(err)
	}
	if !pl.Podcast {
		t.Fatalf("a playlist of episodes came back as songs: %+v", pl)
	}
}
