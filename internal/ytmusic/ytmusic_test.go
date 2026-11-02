package ytmusic

import (
	"encoding/json"
	"testing"
)

func TestParseSearchSuggestions(t *testing.T) {
	mockResponse := `{
		"contents": [
			{
				"searchSuggestionsSectionRenderer": {
					"contents": [
						{
							"searchSuggestionRenderer": {
								"suggestion": {
									"runs": [{"text": "周杰伦"}, {"text": " 晴天"}]
								}
							}
						},
						{
							"searchSuggestionRenderer": {
								"suggestion": {
									"runs": [{"text": "周杰伦 搁浅"}]
								}
							}
						}
					]
				}
			}
		]
	}`

	var root map[string]any
	if err := json.Unmarshal([]byte(mockResponse), &root); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	var suggestions []string
	contents, _ := root["contents"].([]any)
	for _, sec := range contents {
		secMap, ok := sec.(map[string]any)
		if !ok {
			continue
		}
		sr, ok := secMap["searchSuggestionsSectionRenderer"].(map[string]any)
		if !ok {
			continue
		}
		items, ok := sr["contents"].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sug, ok := itemMap["searchSuggestionRenderer"].(map[string]any)
			if !ok {
				continue
			}
			suggestion := RunsToString(sug["suggestion"])
			if suggestion != "" {
				suggestions = append(suggestions, suggestion)
			}
		}
	}

	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
	}
	if suggestions[0] != "周杰伦 晴天" {
		t.Errorf("expected '周杰伦 晴天', got %q", suggestions[0])
	}
}

func TestParseDurationMs(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"3:45", 225000},
		{"0:30", 30000},
		{"1:02:15", 3735000},
		{"120", 120000},
		{"歌曲 • 等什么君 • 4:43", 0},
		{"", 0},
	}
	for _, tc := range tests {
		got := ParseDurationMs(tc.input)
		if got != tc.expected {
			t.Errorf("ParseDurationMs(%q) = %d, expected %d", tc.input, got, tc.expected)
		}
	}
}

func TestSearchTypeParam(t *testing.T) {
	if SearchAll.Param() != "" {
		t.Errorf("SearchAll param should be empty")
	}
	if SearchSongs.Param() == "" {
		t.Errorf("SearchSongs param should not be empty")
	}
	if SearchSongs.String() != "歌曲" {
		t.Errorf("SearchSongs string mismatch: %s", SearchSongs.String())
	}
}

func TestParseLyrics(t *testing.T) {
	rawLines := []string{"[00:12.34]第一行歌词", "[00:25.00]第二行歌词", "普通歌词没有时间戳"}
	lyrics := &Lyrics{}
	for _, l := range rawLines {
		m := lrcTimestampReg.FindStringSubmatch(l)
		if len(m) > 0 {
			lyrics.Synced = true
			lyrics.Lines = append(lyrics.Lines, LyricLine{TimeMs: 12340, Text: "第一行歌词"})
		} else {
			lyrics.Lines = append(lyrics.Lines, LyricLine{Text: l})
		}
	}

	if !lyrics.Synced {
		t.Error("lyrics should be marked synced")
	}
	if len(lyrics.Lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lyrics.Lines))
	}
}

func TestParseResponsiveCardMetadata(t *testing.T) {
	cases := []struct {
		name     string
		subtitle string
	}{
		{
			name:     "split runs",
			subtitle: `[{"text":"歌曲"},{"text":" • "},{"text":"等什么君","navigationEndpoint":{"browseEndpoint":{"browseId":"UC123"}}},{"text":" • "},{"text":"4:43"}]`,
		},
		{
			name:     "single run",
			subtitle: `[{"text":"歌曲 • 等什么君 • 4:43"}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{
				"playlistItemData": {"videoId": "chiling"},
				"flexColumns": [
					{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "赤伶"}]}}},
					{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": ` + tc.subtitle + `}}}
				]
			}`
			var r map[string]any
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			item := parseResponsiveAsCard(r)
			if item.Track == nil {
				t.Fatal("expected a track")
			}
			if item.Track.DurationMs != 283000 {
				t.Errorf("duration = %d, want 283000", item.Track.DurationMs)
			}
			if len(item.Track.Artists) != 1 || item.Track.Artists[0].Name != "等什么君" {
				t.Errorf("artists = %+v, want 等什么君", item.Track.Artists)
			}
		})
	}
}

func TestParseTwoRowCardDuration(t *testing.T) {
	raw := `{
		"title": {"runs": [{"text": "晴天"}]},
		"subtitle": {"runs": [{"text": "周杰伦 • 叶惠美 • 4:29"}]},
		"navigationEndpoint": {"watchEndpoint": {"videoId": "v1"}}
	}`
	var r map[string]any
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	item := parseTwoRowItem(r)
	if item.Track == nil {
		t.Fatal("expected a track")
	}
	if item.Track.DurationMs != 269000 {
		t.Errorf("duration = %d, want 269000", item.Track.DurationMs)
	}
}

func TestParseHumanDurationMs(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{" • 21分钟", 1260000},
		{"1小时2分钟", 3720000},
		{"45秒", 45000},
		{"1 hr 2 min", 3720000},
		{"21:00", 1260000},
		{"2026年", 0},
	}
	for _, tc := range tests {
		if got := parseHumanDurationMs(tc.input); got != tc.want {
			t.Errorf("parseHumanDurationMs(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParseMultiRowPodcastTrack(t *testing.T) {
	raw := `{
		"title": {"runs": [{"text": "04 三山五園的權力密碼"}]},
		"subtitle": {"runs": [{"text": "15次观看"}, {"text": " • "}, {"text": "8月9日"}]},
		"playbackProgress": {"musicPlaybackProgressRenderer": {"durationText": {"runs": [{"text": " • "}, {"text": "21分钟"}]}}},
		"onTap": {"watchEndpoint": {"videoId": "A9uCt7l8Qh4"}},
		"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
			{"url": "//example.com/episode.jpg", "width": 400, "height": 225}
		]}}}
	}`
	var r map[string]any
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	track := parseMultiRowTrack(r)
	if track.ID != "A9uCt7l8Qh4" {
		t.Errorf("id = %q, want A9uCt7l8Qh4", track.ID)
	}
	if track.Title != "04 三山五園的權力密碼" {
		t.Errorf("title = %q", track.Title)
	}
	if track.DurationMs != 1260000 {
		t.Errorf("duration = %d, want 1260000", track.DurationMs)
	}
	if track.Thumbnail != "https://example.com/episode.jpg" {
		t.Errorf("thumbnail = %q", track.Thumbnail)
	}
}

func TestPickAndroidAudio(t *testing.T) {
	raw := `{
		"formats": [{"itag": 18, "url": "https://example.com/18.mp4", "mimeType": "video/mp4"}],
		"adaptiveFormats": [
			{"itag": 139, "url": "https://example.com/139.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.5\""},
			{"itag": 140, "url": "https://example.com/140.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\""}
		]
	}`
	var sd map[string]any
	if err := json.Unmarshal([]byte(raw), &sd); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if f := pickAndroidAudio(sd); f == nil || f["itag"].(float64) != 18 {
		t.Fatalf("expected itag 18, got %+v", f)
	}

	delete(sd, "formats")
	if f := pickAndroidAudio(sd); f == nil || f["itag"].(float64) != 140 {
		t.Fatalf("expected itag 140 fallback, got %+v", f)
	}
}

func TestPickAudioPlaylist(t *testing.T) {
	masterM3U8 := `#EXTM3U
#EXT-X-MEDIA:URI="https://manifest.googlevideo.com/api/manifest/hls_playlist/itag/233/playlist.m3u8",TYPE=AUDIO,GROUP-ID="233"
#EXT-X-MEDIA:URI="https://manifest.googlevideo.com/api/manifest/hls_playlist/itag/234/playlist.m3u8",TYPE=AUDIO,GROUP-ID="234"
#EXT-X-STREAM-INF:BANDWIDTH=385245,CODECS="avc1.4D4015,mp4a.40.2"`

	chosen := pickAudioPlaylist(masterM3U8)
	if chosen != "https://manifest.googlevideo.com/api/manifest/hls_playlist/itag/234/playlist.m3u8" {
		t.Errorf("unexpected audio playlist: %s", chosen)
	}
}
