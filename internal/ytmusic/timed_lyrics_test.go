package ytmusic

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const timedLyricsFixture = `{
  "contents": {
    "elementRenderer": {
      "newElement": {
        "type": {
          "componentType": {
            "model": {
              "timedLyricsModel": {
                "lyricsData": {
                  "timedLyricsData": [
                    {
                      "lyricLine": "第二行",
                      "cueRange": {"startTimeMilliseconds": "16370", "endTimeMilliseconds": "30000"}
                    },
                    {
                      "lyricLine": "第一行",
                      "cueRange": {"startTimeMilliseconds": "1590", "endTimeMilliseconds": "16370"}
                    }
                  ]
                }
              }
            }
          }
        }
      }
    }
  }
}`

func TestParseTimedLyrics(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal([]byte(timedLyricsFixture), &root); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	lyrics, err := parseTimedLyrics(root)
	if err != nil {
		t.Fatalf("parse timed lyrics: %v", err)
	}
	if !lyrics.Synced || len(lyrics.Lines) != 2 {
		t.Fatalf("synced=%v lines=%d, want 2 synced lines", lyrics.Synced, len(lyrics.Lines))
	}
	if lyrics.Lines[0].TimeMs != 1590 || lyrics.Lines[0].Text != "第一行" {
		t.Fatalf("first line = %+v", lyrics.Lines[0])
	}
	if lyrics.Lines[1].TimeMs != 16370 || lyrics.Lines[1].Text != "第二行" {
		t.Fatalf("second line = %+v", lyrics.Lines[1])
	}
}

func TestParseTimedLyricsMissing(t *testing.T) {
	if _, err := parseTimedLyrics(map[string]any{}); err == nil {
		t.Fatal("expected an error without timedLyricsData")
	}
}

func TestGetTimedLyricsUsesAndroidMusicClient(t *testing.T) {
	c := NewClient()
	var gotClient, gotUserAgent string
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotUserAgent = req.Header.Get("User-Agent")

		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		context, _ := payload["context"].(map[string]any)
		client, _ := context["client"].(map[string]any)
		gotClient, _ = client["clientName"].(string)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(timedLyricsFixture)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	lyrics, err := c.GetTimedLyrics(t.Context(), "MPLYt_test")
	if err != nil {
		t.Fatalf("get timed lyrics: %v", err)
	}
	if gotClient != androidMusicClientName {
		t.Fatalf("client = %q, want %q", gotClient, androidMusicClientName)
	}
	if !strings.Contains(gotUserAgent, "android.apps.youtube.music") {
		t.Fatalf("user agent = %q, want Android Music UA", gotUserAgent)
	}
	if len(lyrics.Lines) != 2 || !lyrics.Synced {
		t.Fatalf("lyrics = %+v, want 2 synced lines", lyrics)
	}
}
