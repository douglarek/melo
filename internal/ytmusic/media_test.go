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
