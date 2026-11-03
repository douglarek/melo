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
	lyrics, err := parseLRC("[00:12.34]第一行歌词\n[00:25.00]第二行歌词", 0)
	if err != nil {
		t.Fatalf("parse lyrics: %v", err)
	}
	if !lyrics.Synced || len(lyrics.Lines) != 2 {
		t.Fatalf("synced=%v lines=%d, want 2 synced lines", lyrics.Synced, len(lyrics.Lines))
	}
	if lyrics.Lines[0].TimeMs != 12340 || lyrics.Lines[0].Text != "第一行歌词" {
		t.Fatalf("first line = %+v", lyrics.Lines[0])
	}
	if lyrics.Lines[1].TimeMs != 25000 || lyrics.Lines[1].Text != "第二行歌词" {
		t.Fatalf("second line = %+v", lyrics.Lines[1])
	}
}

func TestParseLRCPlainLyricsNotSynced(t *testing.T) {
	lyrics, err := parseLRC("第一行\n第二行", 123000)
	if err != nil {
		t.Fatalf("parse lyrics: %v", err)
	}
	if lyrics.Synced {
		t.Fatal("plain lyrics must not be marked synced")
	}
	for i, line := range lyrics.Lines {
		if line.TimeMs != 0 {
			t.Fatalf("line %d time = %d, want 0 for plain lyrics", i, line.TimeMs)
		}
	}
}

func TestParseLRCResolvesSingleDecimal(t *testing.T) {
	lyrics, err := parseLRC("[00:01.5]hello", 0)
	if err != nil {
		t.Fatalf("parse lyrics: %v", err)
	}
	if len(lyrics.Lines) != 1 || lyrics.Lines[0].TimeMs != 1500 {
		t.Fatalf("line = %+v, want 1500ms", lyrics.Lines)
	}
}

func TestParseLRCMultipleTimestamps(t *testing.T) {
	lyrics, err := parseLRC("[00:20.50][00:10.00]same\n[00:15.00]middle", 0)
	if err != nil {
		t.Fatalf("parse lyrics: %v", err)
	}
	want := []int64{10000, 15000, 20500}
	if len(lyrics.Lines) != len(want) {
		t.Fatalf("lines = %d, want %d", len(lyrics.Lines), len(want))
	}
	for i, ms := range want {
		if lyrics.Lines[i].TimeMs != ms || lyrics.Lines[i].Text == "" {
			t.Fatalf("line %d = %+v, want %dms", i, lyrics.Lines[i], ms)
		}
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

func TestStreamsVideo(t *testing.T) {
	if !streamsVideo(map[string]any{"mimeType": `video/mp4; codecs="avc1.42001E, mp4a.40.2"`}) {
		t.Error("a progressive format was not found to carry a picture")
	}
	if streamsVideo(map[string]any{"mimeType": `audio/mp4; codecs="mp4a.40.2"`}) {
		t.Error("an audio format was found to carry a picture")
	}
}

func TestPickVideoURL(t *testing.T) {
	raw := `{
		"formats": [{"itag": 18, "url": "https://example.com/18.mp4", "mimeType": "video/mp4; codecs=\"avc1.42001E, mp4a.40.2\""}],
		"adaptiveFormats": [
			{"itag": 137, "url": "https://example.com/137.mp4", "mimeType": "video/mp4; codecs=\"avc1.640028\""},
			{"itag": 136, "url": "https://example.com/136.mp4", "mimeType": "video/mp4; codecs=\"avc1.4D401F\""},
			{"itag": 140, "url": "https://example.com/140.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\""}
		]
	}`
	var sd map[string]any
	if err := json.Unmarshal([]byte(raw), &sd); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if u, format := pickVideoURL(sd); u != "https://example.com/18.mp4" || format != "mp4" {
		t.Errorf("progressive pick = %q, %q, want the muxed format", u, format)
	}

	delete(sd, "formats")
	if u, _ := pickVideoURL(sd); u != "https://example.com/136.mp4" {
		t.Errorf("adaptive pick = %q, want the 720p H.264 format", u)
	}

	// Without H.264, the best of what is there, in its own container.
	var webm map[string]any
	raw = `{"adaptiveFormats": [
		{"itag": 247, "url": "https://example.com/247.webm", "mimeType": "video/webm; codecs=\"vp9\""},
		{"itag": 400, "url": "https://example.com/400.mp4", "mimeType": "video/mp4; codecs=\"av01.0.05M.08\""}
	]}`
	if err := json.Unmarshal([]byte(raw), &webm); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if u, format := pickVideoURL(webm); u != "https://example.com/400.mp4" || format != "mp4" {
		t.Errorf("av1 pick = %q, %q, want the AV1 format over VP9", u, format)
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
