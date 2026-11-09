package ytmusic

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// Podcast episodes are RSS hosted, and YouTube Music plays them from the
// show's own feed rather than its CDN: the web player answers with the
// episode's enclosure as an itag 15 format. Only the signed-in web client
// gets that answer, which is also why the other clients call the episode
// unavailable.
func (c *Client) resolvePodcast(ctx context.Context, videoID string) (*StreamInfo, error) {
	data, err := c.post(ctx, "player", map[string]any{
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}, nil)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return podcastStream(videoID, resp)
}

// podcastStream returns the enclosure a player response names, which is a
// direct URL on the show's host rather than a YouTube one.
func podcastStream(videoID string, resp map[string]any) (*StreamInfo, error) {
	status, _ := resp["playabilityStatus"].(map[string]any)["status"].(string)
	if status != "OK" {
		return nil, ErrUnavailable
	}
	sd, _ := resp["streamingData"].(map[string]any)
	if sd == nil {
		return nil, ErrUnavailable
	}
	formats, _ := sd["formats"].([]any)
	if len(formats) == 0 {
		formats, _ = sd["adaptiveFormats"].([]any)
	}
	for _, raw := range formats {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		u, _ := f["url"].(string)
		mime, _ := f["mimeType"].(string)
		if u == "" || !strings.HasPrefix(mime, "audio/") || !isEnclosureURL(u) {
			continue
		}
		info := &StreamInfo{
			VideoID:   videoID,
			DirectURL: u,
			Format:    "mp3",
		}
		if strings.Contains(mime, "mp4") || strings.Contains(mime, "m4a") {
			info.Format = "mp4"
		}
		if dur, ok := f["approxDurationMs"].(string); ok {
			info.DurationMs, _ = strconv.ParseInt(dur, 10, 64)
		}
		if cl, ok := f["contentLength"].(string); ok {
			info.ContentLength, _ = strconv.ParseInt(cl, 10, 64)
		}
		return info, nil
	}
	return nil, ErrUnavailable
}

// isEnclosureURL reports whether the URL is the show's own file rather than
// a YouTube one, which is what tells a podcast episode's answer from a
// track's.
func isEnclosureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return !strings.HasSuffix(host, "googlevideo.com") && !strings.HasSuffix(host, "youtube.com") && !strings.HasSuffix(host, "ytimg.com")
}
