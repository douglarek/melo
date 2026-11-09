package ytmusic

import "testing"

func musicNav(videoType string) map[string]any {
	return map[string]any{
		"watchEndpoint": map[string]any{
			"watchEndpointMusicSupportedConfigs": map[string]any{
				"watchEndpointMusicConfig": map[string]any{"musicVideoType": videoType},
			},
		},
	}
}

func TestItemHasMusicVideoAcrossRenderers(t *testing.T) {
	run := func(videoType string) map[string]any {
		return map[string]any{"text": "Title", "navigationEndpoint": musicNav(videoType)}
	}

	tests := []struct {
		name string
		item map[string]any
		want bool
	}{
		{
			name: "item endpoint",
			item: map[string]any{"navigationEndpoint": musicNav("MUSIC_VIDEO_TYPE_OMV")},
			want: true,
		},
		{
			name: "title run video",
			item: map[string]any{"title": map[string]any{"runs": []any{run("MUSIC_VIDEO_TYPE_OMV")}}},
			want: true,
		},
		{
			name: "title run art track",
			item: map[string]any{"title": map[string]any{"runs": []any{run("MUSIC_VIDEO_TYPE_ATV")}}},
			want: false,
		},
		{
			name: "overlay play button",
			item: map[string]any{
				"overlay": map[string]any{
					"musicItemThumbnailOverlayRenderer": map[string]any{
						"content": map[string]any{
							"musicPlayButtonRenderer": map[string]any{
								"playNavigationEndpoint": musicNav("MUSIC_VIDEO_TYPE_UGC"),
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no video type",
			item: map[string]any{"navigationEndpoint": map[string]any{}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := itemHasMusicVideo(tt.item); got != tt.want {
				t.Fatalf("itemHasMusicVideo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseResponsiveCardDetectsVideoFromTitleRun(t *testing.T) {
	responsive := func(videoType string) map[string]any {
		return map[string]any{
			"musicResponsiveListItemRenderer": map[string]any{
				"playlistItemData": map[string]any{"videoId": "video-1"},
				"flexColumns": []any{
					map[string]any{
						"musicResponsiveListItemFlexColumnRenderer": map[string]any{
							"text": map[string]any{"runs": []any{
								map[string]any{"text": "Video Song", "navigationEndpoint": musicNav(videoType)},
							}},
						},
					},
					map[string]any{
						"musicResponsiveListItemFlexColumnRenderer": map[string]any{
							"text": map[string]any{"runs": []any{map[string]any{"text": "Artist"}}},
						},
					},
				},
			},
		}
	}

	for _, tt := range []struct {
		name      string
		videoType string
		want      bool
	}{
		{name: "music video", videoType: "MUSIC_VIDEO_TYPE_OMV", want: true},
		{name: "art track", videoType: "MUSIC_VIDEO_TYPE_ATV", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			items := parseCarouselContents([]any{responsive(tt.videoType)})
			if len(items) != 1 || items[0].Track == nil {
				t.Fatalf("items = %+v", items)
			}
			if got := items[0].Track.Video; got != tt.want {
				t.Fatalf("Track.Video = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestItemHasPodcastEpisode checks that a podcast episode is told from a
// music video by the type its endpoints carry, so that its sound resolves
// from the show's feed rather than after the other clients refuse it.
func TestItemHasPodcastEpisode(t *testing.T) {
	run := func(videoType string) map[string]any {
		return map[string]any{"text": "Title", "navigationEndpoint": musicNav(videoType)}
	}
	tests := []struct {
		name string
		item map[string]any
		want bool
	}{
		{
			name: "overlay play button",
			item: map[string]any{
				"overlay": map[string]any{
					"musicItemThumbnailOverlayRenderer": map[string]any{
						"content": map[string]any{
							"musicPlayButtonRenderer": map[string]any{
								"playNavigationEndpoint": musicNav("MUSIC_VIDEO_TYPE_PODCAST_EPISODE"),
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "title run",
			item: map[string]any{"title": map[string]any{"runs": []any{run("MUSIC_VIDEO_TYPE_PODCAST_EPISODE")}}},
			want: true,
		},
		{
			name: "music video",
			item: map[string]any{"navigationEndpoint": musicNav("MUSIC_VIDEO_TYPE_OMV")},
			want: false,
		},
		{
			name: "no video type",
			item: map[string]any{"navigationEndpoint": map[string]any{}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := itemHasPodcastEpisode(tt.item); got != tt.want {
				t.Fatalf("itemHasPodcastEpisode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPodcastShowArtistsKeepsTheShowsID checks that a row naming its show
// keeps the show's page ID: a name alone would send a click to a search of
// musicians, which finds nothing.
func TestPodcastShowArtistsKeepsTheShowsID(t *testing.T) {
	item := map[string]any{
		"secondTitle": map[string]any{"runs": []any{
			map[string]any{"text": "2023年11月20日"},
			map[string]any{"text": "相声汇", "navigationEndpoint": map[string]any{
				"browseEndpoint": map[string]any{
					"browseId": "MPSPPLshow",
					"browseEndpointContextSupportedConfigs": map[string]any{
						"browseEndpointContextMusicConfig": map[string]any{"pageType": "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE"},
					},
				},
			}},
		}},
	}
	artists := podcastShowArtists(item)
	if len(artists) != 1 || artists[0].ID != "MPSPPLshow" || artists[0].Name != "相声汇" {
		t.Fatalf("show artists = %+v, want 相声汇 with its page", artists)
	}
}

// An episode a search finds names its show the same way, and the show's page
// is what its name opens.
func TestExtractArtistsKeepsTheShowOfAnEpisode(t *testing.T) {
	runs := []any{
		map[string]any{"text": "2023年11月20日"},
		map[string]any{"text": "相声汇", "navigationEndpoint": map[string]any{
			"browseEndpoint": map[string]any{"browseId": "MPSPPLshow"},
		}},
	}
	artists := extractArtistsFromRuns(runs)
	if len(artists) != 2 || artists[1].ID != "MPSPPLshow" || artists[1].Name != "相声汇" {
		t.Fatalf("artists = %+v, want the show with its page", artists)
	}
}
