package ytmusic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
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

// The liked songs' page carries no byline at all: reading it must not take
// the app down, which is what an absent line did.
func TestBrowsePlaylistWithoutAByline(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"contents": {
					"twoColumnBrowseResultsRenderer": {
						"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [{
							"musicResponsiveHeaderRenderer": {"title": {"runs": [{"text": "我喜欢的音乐"}]}}
						}]}}}}],
						"secondaryContents": {"sectionListRenderer": {"contents": [{
							"musicPlaylistShelfRenderer": {"contents": []}
						}]}}
					}
				}
			}`)),
		}, nil
	})}
	pl, err := c.BrowsePlaylist(context.Background(), "LM")
	if err != nil {
		t.Fatal(err)
	}
	if pl.Title != "我喜欢的音乐" || pl.Author != "" || pl.AuthorID != "" {
		t.Fatalf("playlist = %+v", pl)
	}
}

// A show's byline names its host's channel, which the page can open.
func TestBrowsePlaylistReadsTheHostsChannel(t *testing.T) {
	pl := browsePlaylistWith(t, `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [{
					"musicResponsiveHeaderRenderer": {
						"title": {"runs": [{"text": "相声汇"}]},
						"straplineTextOne": {"runs": [{"text": "德云茶舍", "navigationEndpoint": {"browseEndpoint": {"browseId": "UChost"}}}]}
					}
				}]}}}}],
				"secondaryContents": {"sectionListRenderer": {"contents": [{"musicShelfRenderer": {"contents": []}}]}}
			}
		}
	}`)
	if pl.Author != "德云茶舍" || pl.AuthorID != "UChost" {
		t.Fatalf("byline = %q (%q)", pl.Author, pl.AuthorID)
	}
}

type podcastPOTFunc func(context.Context, string) (string, error)

func (f podcastPOTFunc) Token(ctx context.Context, binding string) (string, error) {
	return f(ctx, binding)
}

func TestResolvePodcastPrefersAndroidAACLC(t *testing.T) {
	var webPlayerCalled atomic.Bool
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player"):
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {
					"formats": [{"itag": 18, "url": "https://rr.googlevideo.com/18.mp4", "mimeType": "video/mp4; codecs=\"avc1.42001E, mp4a.40.2\"", "audioSampleRate": "22050"}],
					"adaptiveFormats": [
						{"itag": 139, "url": "https://rr.googlevideo.com/139.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""},
						{"itag": 599, "url": "https://rr.googlevideo.com/599.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""},
						{"itag": 140, "url": "https://rr.googlevideo.com/140-de.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "30732126", "audioSampleRate": "44100", "audioTrack": {"audioIsDefault": false, "isAutoDubbed": true}},
						{"itag": 140, "url": "https://rr.googlevideo.com/140-orig.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "30730756", "approxDurationMs": "1898788", "audioSampleRate": "44100", "audioChannels": 2, "audioTrack": {"audioIsDefault": true}}
					]
				}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		case strings.Contains(req.URL.Host, "music.youtube.com") && strings.Contains(req.URL.Path, "player"):
			webPlayerCalled.Store(true)
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {"formats": [{
					"itag": 15,
					"mimeType": "audio/mp4; codecs=\"mp4a.40.5\"",
					"url": "https://rss.example.com/heaac.m4a"
				}]}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		}
	})}

	info, err := c.ResolveStream(t.Context(), Track{ID: "daFV7pCpR0M", Podcast: true})
	if err != nil {
		t.Fatalf("ResolveStream: %v", err)
	}
	if info.Format != "mp4" || info.DirectURL != "https://rr.googlevideo.com/140-orig.m4a" {
		t.Fatalf("got stream = %+v, want original itag 140", info)
	}
	if info.ContentLength != 30730756 || info.SampleRate != 44100 || info.DurationMs != 1898788 || info.Fallback {
		t.Fatalf("unexpected StreamInfo metadata: %+v", info)
	}
	if webPlayerCalled.Load() {
		t.Fatal("web player was called despite ANDROID having adaptive AAC-LC itag 140")
	}
}

func TestResolvePodcastOnlyHEAAC(t *testing.T) {
	androidHEAAC := `{
		"playabilityStatus": {"status": "OK"},
		"streamingData": {
			"formats": [{"itag": 18, "url": "https://rr.googlevideo.com/18.mp4", "mimeType": "video/mp4; codecs=\"avc1.42001E, mp4a.40.2\"", "audioSampleRate": "22050"}],
			"adaptiveFormats": [
				{"itag": 139, "url": "https://rr.googlevideo.com/139.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""},
				{"itag": 599, "url": "https://rr.googlevideo.com/599.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""}
			]
		}
	}`

	t.Run("returns unsupported-codec error when only HE-AAC exists", func(t *testing.T) {
		c := NewClient()
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player") {
				var reqBody map[string]any
				_ = json.NewDecoder(req.Body).Decode(&reqBody)
				clientName, _ := navMap(reqBody, "context", "client")["clientName"].(string)
				if clientName == "ANDROID" {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(androidHEAAC)), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"playabilityStatus":{"status":"OK"},"streamingData":{}}`)), Request: req}, nil
			}
			// Web player enclosure is also HE-AAC.
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {"formats": [{
					"itag": 15,
					"mimeType": "audio/mp4; codecs=\"mp4a.40.5\"",
					"url": "https://rss.example.com/heaac.m4a"
				}]}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}

		info, err := c.ResolveStream(t.Context(), Track{ID: "he-only", Podcast: true})
		if !errors.Is(err, ErrUnsupportedCodec) {
			t.Fatalf("ResolveStream = (%+v, %v), want ErrUnsupportedCodec", info, err)
		}
	})

	t.Run("falls back to decodable RSS MP3 enclosure instead of itag 18 or 599", func(t *testing.T) {
		c := NewClient()
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player") {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(androidHEAAC)), Request: req}, nil
			}
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {"formats": [{
					"itag": 15,
					"mimeType": "audio/mpeg",
					"contentLength": "4321000",
					"approxDurationMs": "600000",
					"url": "https://rss.example.com/episode.mp3"
				}]}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}

		info, err := c.ResolveStream(t.Context(), Track{ID: "rss-mp3", Podcast: true})
		if err != nil {
			t.Fatalf("ResolveStream: %v", err)
		}
		if info.Format != "mp3" || info.DirectURL != "https://rss.example.com/episode.mp3" {
			t.Fatalf("got stream = %+v, want MP3 RSS enclosure", info)
		}
	})

	t.Run("falls back to SABR AAC-LC instead of itag 18 or 599", func(t *testing.T) {
		c := NewClient()
		c.SetSabr(podcastPOTFunc(func(context.Context, string) (string, error) {
			return base64.StdEncoding.EncodeToString([]byte("tok")), nil
		}), t.TempDir())
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(androidHEAAC)), Request: req}, nil
			case strings.Contains(req.URL.Path, "next"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"responseContext":{"visitorData":"vis-pod"}}`)), Request: req}, nil
			case strings.Contains(req.URL.Host, "music.youtube.com") && strings.Contains(req.URL.Path, "player"):
				var reqBody map[string]any
				_ = json.NewDecoder(req.Body).Decode(&reqBody)
				clientName, _ := navMap(reqBody, "context", "client")["clientName"].(string)
				if clientName == "VISIONOS" {
					body := `{
						"playabilityStatus": {"status": "OK"},
						"playerConfig": {"mediaCommonConfig": {"mediaUstreamerRequestConfig": {"videoPlaybackUstreamerConfig": "cfg"}}},
						"streamingData": {
							"serverAbrStreamingUrl": "https://sabr.invalid/stream",
							"adaptiveFormats": [{"itag": 140, "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "8", "approxDurationMs": "1000", "audioSampleRate": "44100"}]
						}
					}`
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"playabilityStatus":{"status":"UNPLAYABLE"}}`)), Request: req}, nil
			case strings.Contains(req.URL.Host, "sabr.invalid"):
				var body bytes.Buffer
				body.Write(makeSabrFormatInitPart(1))
				body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
				body.Write(makeSabrMediaPart(1, []byte("INIT")))
				body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
				body.Write(makeSabrMediaPart(2, []byte("SEG1")))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body.Bytes())), Request: req}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
			}
		})}

		info, err := c.ResolveStream(t.Context(), Track{ID: "sabr-pod", Podcast: true})
		if err != nil {
			t.Fatalf("ResolveStream: %v", err)
		}
		if info.Sabr != nil {
			defer info.Sabr.Close()
		}
		if info.Format != "file" || info.Sabr == nil {
			t.Fatalf("got stream = %+v, want SABR stream", info)
		}
	})
}

func TestResolveMusicRemainsSabrFirst(t *testing.T) {
	var androidCalls atomic.Int32
	c := NewClient()
	c.SetSabr(podcastPOTFunc(func(context.Context, string) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte("tok")), nil
	}), t.TempDir())
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player"):
			androidCalls.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"playabilityStatus":{"status":"OK"}}`)), Request: req}, nil
		case strings.Contains(req.URL.Path, "next"):
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"responseContext":{"visitorData":"vis-music"}}`)), Request: req}, nil
		case strings.Contains(req.URL.Host, "music.youtube.com") && strings.Contains(req.URL.Path, "player"):
			body := `{
				"playabilityStatus": {"status": "OK"},
				"playerConfig": {"mediaCommonConfig": {"mediaUstreamerRequestConfig": {"videoPlaybackUstreamerConfig": "cfg"}}},
				"streamingData": {
					"serverAbrStreamingUrl": "https://sabr.invalid/stream",
					"adaptiveFormats": [{"itag": 140, "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "8", "approxDurationMs": "1000", "audioSampleRate": "44100"}]
				}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		case strings.Contains(req.URL.Host, "sabr.invalid"):
			var body bytes.Buffer
			body.Write(makeSabrFormatInitPart(1))
			body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
			body.Write(makeSabrMediaPart(1, []byte("INIT")))
			body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
			body.Write(makeSabrMediaPart(2, []byte("SEG1")))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body.Bytes())), Request: req}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		}
	})}

	info, err := c.ResolveStream(t.Context(), Track{ID: "song-1", Podcast: false})
	if err != nil {
		t.Fatalf("ResolveStream: %v", err)
	}
	if info.Sabr != nil {
		defer info.Sabr.Close()
	}
	if info.Format != "file" || info.Sabr == nil {
		t.Fatalf("got stream = %+v, want SABR stream", info)
	}
	if androidCalls.Load() != 0 {
		t.Fatalf("ANDROID player was called %d times for normal song when SABR succeeded", androidCalls.Load())
	}
}

func TestResolveMusicAndroidFallbackPrefersAdaptiveAACLC(t *testing.T) {
	c := NewClient() // SABR not configured -> falls back to ANDROID player
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "www.youtube.com") && strings.Contains(req.URL.Path, "player") {
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {
					"formats": [{"itag": 18, "url": "https://rr.googlevideo.com/18.mp4", "mimeType": "video/mp4"}],
					"adaptiveFormats": [
						{"itag": 139, "url": "https://rr.googlevideo.com/139.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""},
						{"itag": 140, "url": "https://rr.googlevideo.com/140.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "1234567", "audioSampleRate": "44100"}
					]
				}
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
	})}

	info, err := c.ResolveStream(t.Context(), Track{ID: "song-fb", Podcast: false})
	if err != nil {
		t.Fatalf("ResolveStream: %v", err)
	}
	if info.Format != "mp4" || info.DirectURL != "https://rr.googlevideo.com/140.m4a" || info.Fallback {
		t.Fatalf("got stream = %+v, want adaptive itag 140 with Fallback=false", info)
	}
}
