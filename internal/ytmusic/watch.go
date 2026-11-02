package ytmusic

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	mediaURIRegex   = regexp.MustCompile(`URI="([^"]+)"`)
	extInfRegex     = regexp.MustCompile(`#EXTINF:([0-9.]+),`)
	lrcTimestampReg = regexp.MustCompile(`\[(\d{1,2}):(\d{2})(?:\.(\d{1,3}))?\]`)
)

// GetNext retrieves the queue / radio continuation and lyrics browse ID.
func (c *Client) GetNext(ctx context.Context, videoID string, playlistID string) ([]Track, string, error) {
	body := map[string]any{
		"videoId": videoID,
	}
	if playlistID != "" {
		body["playlistId"] = playlistID
	}

	data, err := c.post(ctx, "next", body, nil)
	if err != nil {
		return nil, "", err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, "", err
	}

	var nextTracks []Track
	var lyricsBrowseID string

	contents, _ := root["contents"].(map[string]any)
	if contents == nil {
		return nil, "", nil
	}
	scmr, _ := contents["singleColumnMusicWatchNextResultsRenderer"].(map[string]any)
	if scmr == nil {
		return nil, "", nil
	}
	tabbed, _ := scmr["tabbedRenderer"].(map[string]any)
	if tabbed == nil {
		return nil, "", nil
	}
	watchNext, _ := tabbed["watchNextTabbedResultsRenderer"].(map[string]any)
	if watchNext == nil {
		return nil, "", nil
	}
	tabs, _ := watchNext["tabs"].([]any)

	for _, t := range tabs {
		tMap, ok := t.(map[string]any)
		if !ok {
			continue
		}
		tr, _ := tMap["tabRenderer"].(map[string]any)
		if tr == nil {
			continue
		}
		title, _ := tr["title"].(string)

		var bid string
		if ep, ok := tr["endpoint"].(map[string]any); ok {
			if be, ok := ep["browseEndpoint"].(map[string]any); ok {
				bid, _ = be["browseId"].(string)
			}
		}

		if strings.Contains(title, "歌词") || strings.Contains(strings.ToLower(title), "lyric") || strings.HasPrefix(bid, "MPLY") {
			lyricsBrowseID = bid
		}

		// Parse Up Next tab contents
		if trContent, ok := tr["content"].(map[string]any); ok {
			if mq, ok := trContent["musicQueueRenderer"].(map[string]any); ok {
				if mqContent, ok := mq["content"].(map[string]any); ok {
					if ppr, ok := mqContent["playlistPanelRenderer"].(map[string]any); ok {
						contentSec, _ := ppr["contents"].([]any)
						for _, item := range contentSec {
							im, ok := item.(map[string]any)
							if !ok {
								continue
							}
							r, _ := im["playlistPanelVideoRenderer"].(map[string]any)
							if r == nil {
								continue
							}
							vID, _ := r["videoId"].(string)
							tTitle := RunsToString(r["title"])
							artRuns, _ := r["shortBylineText"].(map[string]any)["runs"].([]any)
							durText := RunsToString(r["lengthText"])
							thumbs := ParseThumbnails(r["thumbnail"])

							if vID != "" {
								nextTracks = append(nextTracks, Track{
									ID:         vID,
									Title:      tTitle,
									Artists:    extractArtistsFromRuns(artRuns),
									DurationMs: ParseDurationMs(durText),
									Thumbnail:  BestThumbnail(thumbs),
									Thumbnails: thumbs,
								})
							}
						}
					}
				}
			}
		}
	}

	return nextTracks, lyricsBrowseID, nil
}

// pickAndroidAudio prefers the muxed itag 18 MP4, which is a complete
// faststart file. Adaptive itag 140 is audio-only AAC-LC but may be a
// fragmented MP4 whose URL is range-limited.
func pickAndroidAudio(sd map[string]any) map[string]any {
	formats, _ := sd["formats"].([]any)
	for _, raw := range formats {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		itag, _ := f["itag"].(float64)
		if itag != 18 {
			continue
		}
		if u, _ := f["url"].(string); u != "" {
			return f
		}
	}

	adaptive, _ := sd["adaptiveFormats"].([]any)
	for _, raw := range adaptive {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		mime, _ := f["mimeType"].(string)
		if !strings.Contains(mime, "audio/mp4") || !strings.Contains(mime, "mp4a.40.2") {
			continue
		}
		if u, _ := f["url"].(string); u != "" {
			return f
		}
	}
	return nil
}

// GetLyrics fetches and parses the lyrics for a song.
func (c *Client) GetLyrics(ctx context.Context, lyricsBrowseID string, durationMs ...int64) (*Lyrics, error) {
	if lyricsBrowseID == "" {
		return nil, errors.New("no lyrics available")
	}

	body := map[string]any{
		"browseId": lyricsBrowseID,
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	contents, _ := root["contents"].(map[string]any)
	if contents == nil {
		return nil, errors.New("empty lyrics response")
	}
	slr, _ := contents["sectionListRenderer"].(map[string]any)
	if slr == nil {
		return nil, errors.New("empty lyrics section")
	}
	secList, _ := slr["contents"].([]any)
	if len(secList) == 0 {
		return nil, errors.New("empty lyrics section")
	}
	firstSec, _ := secList[0].(map[string]any)
	if firstSec == nil {
		return nil, errors.New("empty lyrics item")
	}
	shelf, _ := firstSec["musicDescriptionShelfRenderer"].(map[string]any)
	if shelf == nil {
		return nil, errors.New("lyrics shelf not found")
	}

	fullText := RunsToString(shelf["description"])
	if fullText == "" {
		return nil, errors.New("no lyrics text found")
	}

	dur := int64(0)
	if len(durationMs) > 0 {
		dur = durationMs[0]
	}

	return parseLRC(fullText, dur)
}

// GetTimedLyrics retrieves timed lyrics through the Android Music client.
func (c *Client) GetTimedLyrics(ctx context.Context, browseID string) (*Lyrics, error) {
	if browseID == "" {
		return nil, errors.New("no lyrics available")
	}

	data, err := c.postWithClient(ctx, "browse", map[string]any{
		"browseId": browseID,
	}, nil, map[string]any{
		"clientName":        androidMusicClientName,
		"clientVersion":     androidMusicClientVersion,
		"androidSdkVersion": 30,
		"osName":            "Android",
		"osVersion":         "11",
	}, androidMusicUserAgent)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return parseTimedLyrics(root)
}

func parseTimedLyrics(root map[string]any) (*Lyrics, error) {
	raw := findTimedLyricsData(root)
	if len(raw) == 0 {
		return nil, errors.New("timed lyrics not found")
	}

	lines := make([]LyricLine, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text, _ := entry["lyricLine"].(string)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		cue, _ := entry["cueRange"].(map[string]any)
		lines = append(lines, LyricLine{
			TimeMs: lyricTimeMilliseconds(cue["startTimeMilliseconds"]),
			Text:   text,
		})
	}
	if len(lines) == 0 {
		return nil, errors.New("timed lyrics not found")
	}

	slices.SortStableFunc(lines, func(a, b LyricLine) int {
		return cmp.Compare(a.TimeMs, b.TimeMs)
	})
	return &Lyrics{Lines: lines, Synced: true}, nil
}

func findTimedLyricsData(value any) []any {
	switch node := value.(type) {
	case map[string]any:
		if raw, ok := node["timedLyricsData"].([]any); ok {
			return raw
		}
		for _, child := range node {
			if raw := findTimedLyricsData(child); raw != nil {
				return raw
			}
		}
	case []any:
		for _, child := range node {
			if raw := findTimedLyricsData(child); raw != nil {
				return raw
			}
		}
	}
	return nil
}

func lyricTimeMilliseconds(value any) int64 {
	switch v := value.(type) {
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return n
		}
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return int64(f)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

var cleanTitleReg = regexp.MustCompile(`(?i)\s*[\(\[](official|video|audio|mv|hd|hq|live|remastered)[^\)\]]*[\)\]]`)

func cleanTrackTitle(title string) string {
	cleaned := cleanTitleReg.ReplaceAllString(title, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return title
	}
	return cleaned
}

// FetchLrcLibLyrics queries lrclib.net for synchronized LRC lyrics.
func FetchLrcLibLyrics(ctx context.Context, title, artist string, durationSec int) (*Lyrics, error) {
	if title == "" {
		return nil, errors.New("empty title")
	}
	params := url.Values{}
	params.Set("track_name", cleanTrackTitle(title))
	if artist != "" {
		params.Set("artist_name", artist)
	}
	if durationSec > 0 {
		params.Set("duration", strconv.Itoa(durationSec))
	}

	reqURL := "https://lrclib.net/api/get?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "melo/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib HTTP %d", resp.StatusCode)
	}

	var res struct {
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	raw := res.SyncedLyrics
	if raw == "" {
		raw = res.PlainLyrics
	}
	if raw == "" {
		return nil, errors.New("no lyrics in lrclib response")
	}

	return parseLRC(raw, int64(durationSec)*1000)
}

func parseLRC(raw string, _ int64) (*Lyrics, error) {
	lyrics := &Lyrics{Synced: false}
	lines := strings.Split(raw, "\n")
	hasLRC := false

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		matches := lrcTimestampReg.FindAllStringSubmatch(trimmed, -1)
		if len(matches) > 0 {
			hasLRC = true
			lyrics.Synced = true
			cleanLine := strings.TrimSpace(lrcTimestampReg.ReplaceAllString(trimmed, ""))
			if cleanLine == "" {
				continue
			}
			for _, m := range matches {
				min, _ := strconv.ParseInt(m[1], 10, 64)
				sec, _ := strconv.ParseInt(m[2], 10, 64)
				var ms int64
				if len(m) > 3 && m[3] != "" {
					ms, _ = strconv.ParseInt(m[3], 10, 64)
					switch len(m[3]) {
					case 1:
						ms *= 100
					case 2:
						ms *= 10
					}
				}
				tMs := (min*60+sec)*1000 + ms
				lyrics.Lines = append(lyrics.Lines, LyricLine{TimeMs: tMs, Text: cleanLine})
			}
		} else if !hasLRC {
			lyrics.Lines = append(lyrics.Lines, LyricLine{Text: trimmed})
		}
	}

	if hasLRC {
		slices.SortStableFunc(lyrics.Lines, func(a, b LyricLine) int {
			return cmp.Compare(a.TimeMs, b.TimeMs)
		})
	}

	// Plain lyrics keep Synced false and carry no timestamps.
	if len(lyrics.Lines) == 0 {
		return nil, errors.New("empty lyrics")
	}
	return lyrics, nil
}

// ResolveStream resolves playable audio for videoID (direct MP4 or HLS).
func (c *Client) ResolveStream(ctx context.Context, videoID string) (*StreamInfo, error) {
	// 1. First attempt: ANDROID client (reliable itag 18 stream)
	androidPayload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":        "ANDROID",
				"clientVersion":     "20.10.38",
				"androidSdkVersion": 30,
				"userAgent":         "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip",
				"osName":            "Android",
				"osVersion":         "11",
				"hl":                "en",
				"gl":                "US",
			},
		},
		"videoId": videoID,
	}

	b, err := json.Marshal(androidPayload)
	if err == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, youtubeBaseURL+"player?alt=json", bytes.NewReader(b))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip")
			req.Header.Set("X-YouTube-Client-Name", "3")
			req.Header.Set("X-YouTube-Client-Version", "20.10.38")
			c.setPlayerAuth(req)

			resp, err := c.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var pResp map[string]any
					if json.NewDecoder(resp.Body).Decode(&pResp) == nil {
						sd, _ := pResp["streamingData"].(map[string]any)
						if sd != nil {
							if f := pickAndroidAudio(sd); f != nil {
								u, _ := f["url"].(string)
								durMs := int64(0)
								if dStr, ok := f["approxDurationMs"].(string); ok {
									durMs, _ = strconv.ParseInt(dStr, 10, 64)
								}
								sampleRate := 44100
								if sr, ok := f["audioSampleRate"].(string); ok {
									if v, err := strconv.Atoi(sr); err == nil && v > 0 {
										sampleRate = v
									}
								}
								channels := 2
								if ch, ok := f["audioChannels"].(float64); ok && ch > 0 {
									channels = int(ch)
								}
								contentLength := int64(0)
								if cl, ok := f["contentLength"].(string); ok {
									contentLength, _ = strconv.ParseInt(cl, 10, 64)
								}
								return &StreamInfo{
									VideoID:       videoID,
									DirectURL:     u,
									Format:        "mp4",
									SampleRate:    sampleRate,
									Channels:      channels,
									DurationMs:    durMs,
									ContentLength: contentLength,
									HEAAC:         strings.Contains(u, "heaudio"),
								}, nil
							}
						}
					}
				}
			}
		}
	}

	return c.ResolveHLS(ctx, videoID)
}

// setPlayerAuth attaches the signed-in session to a player request.
func (c *Client) setPlayerAuth(req *http.Request) {
	c.mu.RLock()
	cookie := c.cookie
	c.mu.RUnlock()
	if cookie == "" {
		return
	}
	req.Header.Set("Cookie", cookie)
	if sapisid := ExtractSAPISID(cookie); sapisid != "" {
		req.Header.Set("Authorization", GenerateSAPISIDHash(sapisid, "https://www.youtube.com"))
	}
}

// ResolveHLS resolves an audio-only HLS stream through the iOS client.
func (c *Client) ResolveHLS(ctx context.Context, videoID string) (*StreamInfo, error) {
	reqPayload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "IOS",
				"clientVersion": "21.26.4",
				"deviceMake":    "Apple",
				"deviceModel":   "iPhone16,2",
				"userAgent":     "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)",
				"osName":        "iPhone",
				"osVersion":     "18.3.2.22D82",
				"hl":            "en",
				"gl":            "US",
			},
		},
		"videoId": videoID,
	}

	b, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	playerURL := youtubeBaseURL + "player?alt=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, playerURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)")
	req.Header.Set("X-YouTube-Client-Name", "5")
	req.Header.Set("X-YouTube-Client-Version", "21.26.4")
	c.setPlayerAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("player request HTTP %d", resp.StatusCode)
	}

	var pResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pResp); err != nil {
		return nil, err
	}

	streamingData, _ := pResp["streamingData"].(map[string]any)
	hlsManifestURL, _ := streamingData["hlsManifestUrl"].(string)
	if hlsManifestURL == "" {
		return nil, errors.New("no hlsManifestUrl returned")
	}

	// Fetch master HLS playlist
	masterM3U8, err := c.fetchText(ctx, hlsManifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch hls manifest: %w", err)
	}

	// Find the best audio playlist URL (prefer itag 234 / 140 AAC-LC)
	audioPlaylistURL := pickAudioPlaylist(masterM3U8)
	if audioPlaylistURL == "" {
		return nil, errors.New("no audio playlist found in master HLS manifest")
	}

	// Fetch media playlist containing segments
	mediaM3U8, err := c.fetchText(ctx, audioPlaylistURL)
	if err != nil {
		return nil, fmt.Errorf("fetch media playlist: %w", err)
	}

	segments, totalDurMs := parseMediaSegments(mediaM3U8)
	if len(segments) == 0 {
		return nil, errors.New("no segments in media playlist")
	}

	return &StreamInfo{
		VideoID:     videoID,
		SampleRate:  44100,
		Channels:    2,
		DurationMs:  totalDurMs,
		HLSPlaylist: audioPlaylistURL,
		Segments:    segments,
	}, nil
}

func (c *Client) fetchText(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func pickAudioPlaylist(masterM3U8 string) string {
	lines := strings.Split(masterM3U8, "\n")
	var candidates []string
	var preferred string

	for _, l := range lines {
		if strings.HasPrefix(l, "#EXT-X-MEDIA:") && strings.Contains(l, "TYPE=AUDIO") {
			m := mediaURIRegex.FindStringSubmatch(l)
			if len(m) > 1 {
				uri := m[1]
				candidates = append(candidates, uri)
				// itag 234 / itag 140 is AAC-LC
				if strings.Contains(uri, "itag/234") || strings.Contains(uri, "itag/140") {
					preferred = uri
				}
			}
		}
	}

	if preferred != "" {
		return preferred
	}
	if len(candidates) > 0 {
		return candidates[len(candidates)-1]
	}
	return ""
}

func parseMediaSegments(mediaM3U8 string) ([]StreamSegment, int64) {
	lines := strings.Split(mediaM3U8, "\n")
	var segments []StreamSegment
	var curDurMs int64
	var totalDurMs int64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			m := extInfRegex.FindStringSubmatch(line)
			if len(m) > 1 {
				sec, _ := strconv.ParseFloat(m[1], 64)
				curDurMs = int64(sec * 1000)
			}
		} else if strings.HasPrefix(line, "http") {
			segments = append(segments, StreamSegment{
				URI:        line,
				DurationMs: curDurMs,
			})
			totalDurMs += curDurMs
			curDurMs = 0
		}
	}

	return segments, totalDurMs
}
