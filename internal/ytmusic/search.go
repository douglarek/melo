package ytmusic

import (
	"context"
	"encoding/json"
	"strings"
)

// Search executes a search query with an optional type filter.
func (c *Client) Search(ctx context.Context, query string, filter SearchType) (*SearchResult, error) {
	body := map[string]any{
		"query": query,
	}
	if p := filter.Param(); p != "" {
		body["params"] = p
	}

	data, err := c.post(ctx, "search", body, nil)
	if err != nil {
		return nil, err
	}

	return parseSearchResponse(data)
}

// SearchSuggest retrieves search autocomplete suggestions.
func (c *Client) SearchSuggest(ctx context.Context, query string) ([]string, error) {
	body := map[string]any{
		"input": query,
	}
	data, err := c.post(ctx, "music/get_search_suggestions", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
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
	return suggestions, nil
}

func parseSearchResponse(data []byte) (*SearchResult, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	res := &SearchResult{}

	contents, _ := root["contents"].(map[string]any)
	tabbed, _ := contents["tabbedSearchResultsRenderer"].(map[string]any)
	tabs, _ := tabbed["tabs"].([]any)
	if len(tabs) == 0 {
		return res, nil
	}

	tab0, _ := tabs[0].(map[string]any)
	tabRenderer, _ := tab0["tabRenderer"].(map[string]any)
	tabContent, _ := tabRenderer["content"].(map[string]any)
	sectionList, _ := tabContent["sectionListRenderer"].(map[string]any)
	sections, _ := sectionList["contents"].([]any)

	for _, s := range sections {
		secMap, ok := s.(map[string]any)
		if !ok {
			continue
		}

		// 1. MusicCardShelfRenderer (Top result)
		if card, ok := secMap["musicCardShelfRenderer"].(map[string]any); ok {
			parseCardShelf(card, res)
		}

		// 2. ItemSectionRenderer (Standard result items)
		if itemSec, ok := secMap["itemSectionRenderer"].(map[string]any); ok {
			items, _ := itemSec["contents"].([]any)
			for _, item := range items {
				im, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
					parseResponsiveListItem(r, res)
				} else if shelf, ok := im["musicShelfRenderer"].(map[string]any); ok {
					parseMusicShelf(shelf, res)
				}
			}
		}

		// 3. MusicShelfRenderer (Direct shelf in section)
		if shelf, ok := secMap["musicShelfRenderer"].(map[string]any); ok {
			parseMusicShelf(shelf, res)
		}
	}

	return res, nil
}

func parseCardShelf(card map[string]any, res *SearchResult) {
	titleRuns, _ := card["title"].(map[string]any)["runs"].([]any)
	title := RunsToString(titleRuns)

	subtitleRuns, _ := card["subtitle"].(map[string]any)["runs"].([]any)
	subtitle := RunsToString(subtitleRuns)

	thumbs := ParseThumbnails(card["thumbnail"])
	thumbURL := BestThumbnail(thumbs)

	// Check if card is a track
	var videoID string
	var nav map[string]any
	if len(titleRuns) > 0 {
		if tr0, ok := titleRuns[0].(map[string]any); ok {
			nav, _ = tr0["navigationEndpoint"].(map[string]any)
			we, _ := nav["watchEndpoint"].(map[string]any)
			videoID, _ = we["videoId"].(string)
		}
	}
	if videoID != "" {
		res.Tracks = append(res.Tracks, Track{
			ID:         videoID,
			Title:      title,
			Thumbnail:  thumbURL,
			Thumbnails: thumbs,
			Artists:    []ArtistRef{{Name: subtitle}},
			Video:      hasMusicVideo(nav),
		})
	}
}

func parseMusicShelf(shelf map[string]any, res *SearchResult) {
	items, _ := shelf["contents"].([]any)
	for _, item := range items {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
			parseResponsiveListItem(r, res)
		}
	}
}

func parseResponsiveListItem(r map[string]any, res *SearchResult) {
	flexCols, _ := r["flexColumns"].([]any)
	if len(flexCols) == 0 {
		return
	}

	// Title from col 0
	col0, _ := flexCols[0].(map[string]any)
	col0Flex, _ := col0["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
	col0Runs, _ := col0Flex["text"].(map[string]any)["runs"].([]any)
	title := RunsToString(col0Runs)

	// Subtitle from col 1
	var subtitle string
	var col1Runs []any
	if len(flexCols) > 1 {
		col1, _ := flexCols[1].(map[string]any)
		col1Flex, _ := col1["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		col1Runs, _ = col1Flex["text"].(map[string]any)["runs"].([]any)
		subtitle = RunsToString(col1Runs)
	}

	thumbs := extractThumbnail(r)
	thumbURL := BestThumbnail(thumbs)

	// Video ID
	var videoID string
	if pid, ok := r["playlistItemData"].(map[string]any); ok {
		videoID, _ = pid["videoId"].(string)
	}
	if videoID == "" {
		videoID = extractOverlayVideoID(r)
	}
	if videoID == "" && len(col0Runs) > 0 {
		if tr0, ok := col0Runs[0].(map[string]any); ok {
			nav, _ := tr0["navigationEndpoint"].(map[string]any)
			we, _ := nav["watchEndpoint"].(map[string]any)
			videoID, _ = we["videoId"].(string)
		}
	}

	// Browse ID (Artist, Album, Playlist)
	var browseID string
	var pageType string

	// 1. Check root navigationEndpoint on item
	if nav, ok := r["navigationEndpoint"].(map[string]any); ok && nav != nil {
		if be, ok := nav["browseEndpoint"].(map[string]any); ok && be != nil {
			browseID, _ = be["browseId"].(string)
			if supported, ok := be["browseEndpointContextSupportedConfigs"].(map[string]any); ok {
				if musicConfig, ok := supported["browseEndpointContextMusicConfig"].(map[string]any); ok {
					pageType, _ = musicConfig["pageType"].(string)
				}
			}
		}
	}

	// 2. Fallback to col0Runs[0] navigationEndpoint
	if browseID == "" && len(col0Runs) > 0 {
		if tr0, ok := col0Runs[0].(map[string]any); ok {
			nav, _ := tr0["navigationEndpoint"].(map[string]any)
			if nav != nil {
				be, _ := nav["browseEndpoint"].(map[string]any)
				if be != nil {
					browseID, _ = be["browseId"].(string)
					if supported, ok := be["browseEndpointContextSupportedConfigs"].(map[string]any); ok {
						if musicConfig, ok := supported["browseEndpointContextMusicConfig"].(map[string]any); ok {
							pageType, _ = musicConfig["pageType"].(string)
						}
					}
				}
			}
		}
	}

	// Duration
	durationMs := extractDuration(r, flexCols)

	// Category discernment. Explicit page types take precedence: album
	// results may also carry a play-button video id, which must not turn
	// them into tracks.
	switch {
	case strings.HasPrefix(browseID, "UC") || pageType == "MUSIC_PAGE_TYPE_ARTIST":
		res.Artists = append(res.Artists, ArtistRef{ID: browseID, Name: title, Thumbnail: thumbURL})
	case strings.HasPrefix(browseID, "MPREb_") || pageType == "MUSIC_PAGE_TYPE_ALBUM":
		res.Albums = append(res.Albums, AlbumItem{ID: browseID, Title: title, Artist: subtitle, Thumbnail: thumbURL})
	case strings.HasPrefix(browseID, "VL") || strings.HasPrefix(browseID, "PL") || pageType == "MUSIC_PAGE_TYPE_PLAYLIST":
		res.Playlists = append(res.Playlists, PlaylistItem{ID: browseID, Title: title, Author: subtitle, Thumbnail: thumbURL})
	case videoID != "":
		nav, _ := r["navigationEndpoint"].(map[string]any)
		video := hasMusicVideo(nav)
		if !video {
			for _, raw := range col0Runs {
				run, _ := raw.(map[string]any)
				runNav, _ := run["navigationEndpoint"].(map[string]any)
				if hasMusicVideo(runNav) {
					video = true
					break
				}
			}
		}
		res.Tracks = append(res.Tracks, Track{
			ID:         videoID,
			Title:      title,
			Artists:    extractArtistsFromRuns(col1Runs),
			DurationMs: durationMs,
			Thumbnail:  thumbURL,
			Thumbnails: thumbs,
			Video:      video,
		})
	case strings.Contains(subtitle, "音乐人") || strings.Contains(subtitle, "Artist"):
		res.Artists = append(res.Artists, ArtistRef{ID: browseID, Name: title, Thumbnail: thumbURL})
	case strings.Contains(subtitle, "专辑") || strings.Contains(subtitle, "EP") || strings.Contains(subtitle, "Single"):
		res.Albums = append(res.Albums, AlbumItem{ID: browseID, Title: title, Artist: subtitle, Thumbnail: thumbURL})
	case strings.Contains(subtitle, "歌单") || strings.Contains(subtitle, "播放列表"):
		res.Playlists = append(res.Playlists, PlaylistItem{ID: browseID, Title: title, Author: subtitle, Thumbnail: thumbURL})
	}
}

func extractThumbnail(r map[string]any) []Thumbnail {
	if r == nil {
		return nil
	}
	if thumbs := ParseThumbnails(r["thumbnailRenderer"]); len(thumbs) > 0 {
		return thumbs
	}
	if thumbs := ParseThumbnails(r["thumbnail"]); len(thumbs) > 0 {
		return thumbs
	}
	return ParseThumbnails(r)
}

func extractOverlayVideoID(r map[string]any) string {
	overlay, ok := r["overlay"].(map[string]any)
	if !ok {
		return ""
	}
	thumbOverlay, ok := overlay["musicItemThumbnailOverlayRenderer"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := thumbOverlay["content"].(map[string]any)
	if !ok {
		return ""
	}
	btn, ok := content["musicPlayButtonRenderer"].(map[string]any)
	if !ok {
		return ""
	}
	playNav, ok := btn["playNavigationEndpoint"].(map[string]any)
	if !ok {
		return ""
	}
	watch, ok := playNav["watchEndpoint"].(map[string]any)
	if !ok {
		return ""
	}
	vID, _ := watch["videoId"].(string)
	return vID
}

// extractDuration extracts duration in milliseconds from flexColumns, fixedColumns, or lengthText.
func extractDuration(r map[string]any, flexCols []any) int64 {
	// 1. Check fixedColumns
	if fixedCols, ok := r["fixedColumns"].([]any); ok && len(fixedCols) > 0 {
		for _, fc := range fixedCols {
			fcMap, _ := fc.(map[string]any)
			fcFlex, _ := fcMap["musicResponsiveListItemFixedColumnRenderer"].(map[string]any)
			text := RunsToString(fcFlex["text"])
			if strings.Contains(text, ":") {
				if d := ParseDurationMs(text); d > 0 {
					return d
				}
			}
		}
	}

	// 2. Check lengthText
	if lt, ok := r["lengthText"].(map[string]any); ok {
		text := RunsToString(lt)
		if strings.Contains(text, ":") {
			if d := ParseDurationMs(text); d > 0 {
				return d
			}
		}
	}

	// 3. Use the last non-navigable run of the subtitle as a duration
	// candidate.
	if len(flexCols) > 0 {
		colMap, _ := flexCols[len(flexCols)-1].(map[string]any)
		colFlex, _ := colMap["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		text, _ := colFlex["text"].(map[string]any)
		runs, _ := text["runs"].([]any)
		if len(runs) > 0 {
			last, _ := runs[len(runs)-1].(map[string]any)
			if _, navigable := last["navigationEndpoint"]; !navigable {
				raw, _ := last["text"].(string)
				parts := strings.Split(raw, "•")
				candidate := strings.TrimSpace(parts[len(parts)-1])
				if d := ParseDurationMs(candidate); d > 0 {
					return d
				}
			}
		}
	}

	return 0
}

func extractArtistsFromRuns(runs []any) []ArtistRef {
	var artists []ArtistRef
	for _, r := range runs {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		text, _ := m["text"].(string)
		text = strings.TrimSpace(text)
		if text == "" || text == "•" || text == "," || text == "&" || strings.HasPrefix(text, "歌曲") || strings.HasPrefix(text, "视频") {
			continue
		}
		nav, _ := m["navigationEndpoint"].(map[string]any)
		be, _ := nav["browseEndpoint"].(map[string]any)
		browseID, _ := be["browseId"].(string)
		if strings.HasPrefix(browseID, "UC") {
			artists = append(artists, ArtistRef{
				ID:   browseID,
				Name: text,
			})
		} else if len(artists) == 0 && text != "" && !strings.Contains(text, "次") {
			// Fallback artist name
			artists = append(artists, ArtistRef{Name: text})
		}
	}
	return artists
}
