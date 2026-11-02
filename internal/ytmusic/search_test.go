package ytmusic

import "testing"

func flexColumn(text string) map[string]any {
	return map[string]any{
		"musicResponsiveListItemFlexColumnRenderer": map[string]any{
			"text": map[string]any{"runs": []any{map[string]any{"text": text}}},
		},
	}
}

func TestSearchAlbumResultWithPlayVideoIDIsNotATrack(t *testing.T) {
	r := map[string]any{
		"flexColumns": []any{
			flexColumn("平凡的一天"),
			flexColumn("专辑，毛不易"),
		},
		"playlistItemData": map[string]any{"videoId": "first-track-video"},
		"navigationEndpoint": map[string]any{
			"browseEndpoint": map[string]any{
				"browseId": "MPREb_album",
				"browseEndpointContextSupportedConfigs": map[string]any{
					"browseEndpointContextMusicConfig": map[string]any{
						"pageType": "MUSIC_PAGE_TYPE_ALBUM",
					},
				},
			},
		},
	}

	res := &SearchResult{}
	parseResponsiveListItem(r, res)
	if len(res.Albums) != 1 || len(res.Tracks) != 0 {
		t.Fatalf("albums=%d tracks=%d, want 1 album and 0 tracks", len(res.Albums), len(res.Tracks))
	}
	if res.Albums[0].ID != "MPREb_album" || res.Albums[0].Title != "平凡的一天" {
		t.Fatalf("album = %+v", res.Albums[0])
	}
}

func TestSearchTrackWithoutBrowsePageStaysATrack(t *testing.T) {
	r := map[string]any{
		"flexColumns": []any{
			flexColumn("晴天"),
			flexColumn("歌曲 • 周杰伦"),
		},
		"playlistItemData": map[string]any{"videoId": "track-video"},
	}
	res := &SearchResult{}
	parseResponsiveListItem(r, res)
	if len(res.Tracks) != 1 || len(res.Albums) != 0 {
		t.Fatalf("tracks=%d albums=%d, want 1 track and 0 albums", len(res.Tracks), len(res.Albums))
	}
}
