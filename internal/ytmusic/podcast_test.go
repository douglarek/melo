package ytmusic

import (
	"encoding/json"
	"errors"
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
