package ytmusic

import (
	"encoding/json"
	"testing"
)

func TestRelatedBrowseID(t *testing.T) {
	raw := `{
		"contents": {"singleColumnMusicWatchNextResultsRenderer": {"tabbedRenderer": {"watchNextTabbedResultsRenderer": {"tabs": [
			{"tabRenderer": {"title": "接下来播放"}},
			{"tabRenderer": {"title": "相关内容", "endpoint": {"browseEndpoint": {"browseId": "MPTRabc123"}}}}
		]}}}}
	}`
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if got := relatedBrowseID(root); got != "MPTRabc123" {
		t.Fatalf("relatedBrowseID = %q, want MPTRabc123", got)
	}
}

func TestParseRelatedSections(t *testing.T) {
	raw := `{
		"contents": {"sectionListRenderer": {"contents": [
			{"musicCarouselShelfRenderer": {
				"header": {"musicCarouselShelfBasicHeaderRenderer": {"title": {"runs": [{"text": "你可能还喜欢"}]}}},
				"contents": [
					{"musicResponsiveListItemRenderer": {
						"flexColumns": [
							{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "让一切随风"}]}}},
							{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "钟镇涛"}]}}},
							{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "听涛"}]}}}
						],
						"playlistItemData": {"videoId": "17r5yGsOkj4"}
					}}
				]
			}},
			{"musicCarouselShelfRenderer": {
				"header": {"musicCarouselShelfBasicHeaderRenderer": {"title": {"runs": [{"text": "推荐的播放列表"}]}}},
				"contents": [
					{"musicTwoRowItemRenderer": {
						"title": {"runs": [{"text": "经典"}]},
						"subtitle": {"runs": [{"text": "播放列表"}, {"text": " • "}, {"text": "Zuan Xu"}]},
						"navigationEndpoint": {"browseEndpoint": {"browseId": "VLPLXgpp9zICVTGBOeFm6FXajdagIcLJ8Hr-"}},
						"thumbnailRenderer": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://example.com/cover.jpg", "width": 192, "height": 192}]}}}
					}}
				]
			}}
		]}}
	}`
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	sections := parseRelatedSections(root)
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(sections))
	}
	if sections[0].Title != "你可能还喜欢" || len(sections[0].Items) != 1 {
		t.Fatalf("track shelf = %+v", sections[0])
	}
	track := sections[0].Items[0]
	if track.Type != "track" || track.ID != "17r5yGsOkj4" || track.Track == nil {
		t.Fatalf("track card = %+v", track)
	}
	if sections[1].Title != "推荐的播放列表" || len(sections[1].Items) != 1 {
		t.Fatalf("playlist shelf = %+v", sections[1])
	}
	playlist := sections[1].Items[0]
	if playlist.Type != "playlist" || playlist.ID != "VLPLXgpp9zICVTGBOeFm6FXajdagIcLJ8Hr-" {
		t.Fatalf("playlist card = %+v", playlist)
	}
}
