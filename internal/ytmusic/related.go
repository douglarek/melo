package ytmusic

import (
	"context"
	"encoding/json"
	"strings"
)

// GetRelated loads the "你可能还喜欢" and "推荐的播放列表" shelves for a track.
func (c *Client) GetRelated(ctx context.Context, videoID string) ([]Section, error) {
	data, err := c.post(ctx, "next", map[string]any{"videoId": videoID}, nil)
	if err != nil {
		return nil, err
	}
	var nextRoot map[string]any
	if err := json.Unmarshal(data, &nextRoot); err != nil {
		return nil, err
	}

	relatedID := relatedBrowseID(nextRoot)
	if relatedID == "" {
		return nil, nil
	}

	data, err = c.post(ctx, "browse", map[string]any{"browseId": relatedID}, nil)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return parseRelatedSections(root), nil
}

func relatedBrowseID(root map[string]any) string {
	contents, _ := root["contents"].(map[string]any)
	scmr, _ := contents["singleColumnMusicWatchNextResultsRenderer"].(map[string]any)
	tabbed, _ := scmr["tabbedRenderer"].(map[string]any)
	watchNext, _ := tabbed["watchNextTabbedResultsRenderer"].(map[string]any)
	tabs, _ := watchNext["tabs"].([]any)

	for _, raw := range tabs {
		tab, _ := raw.(map[string]any)
		tr, _ := tab["tabRenderer"].(map[string]any)
		if tr == nil {
			continue
		}
		endpoint, _ := tr["endpoint"].(map[string]any)
		be, _ := endpoint["browseEndpoint"].(map[string]any)
		id, _ := be["browseId"].(string)
		pageType := ""
		configs, _ := be["browseEndpointContextSupportedConfigs"].(map[string]any)
		musicConfig, _ := configs["browseEndpointContextMusicConfig"].(map[string]any)
		pageType, _ = musicConfig["pageType"].(string)
		title, _ := tr["title"].(string)

		if strings.HasPrefix(id, "MPTR") || pageType == "MUSIC_PAGE_TYPE_TRACK_RELATED" ||
			strings.Contains(title, "相关") || strings.Contains(strings.ToLower(title), "related") {
			return id
		}
	}
	return ""
}

func parseRelatedSections(root map[string]any) []Section {
	var sections []Section
	for _, shelf := range findCarouselShelves(root) {
		header, _ := shelf["header"].(map[string]any)["musicCarouselShelfBasicHeaderRenderer"].(map[string]any)
		title := RunsToString(header["title"])
		rawItems, _ := shelf["contents"].([]any)

		var items []CardItem
		for _, raw := range rawItems {
			im, _ := raw.(map[string]any)
			if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
				track := parseTrackFromListItem(r)
				if track.ID == "" {
					continue
				}
				items = append(items, CardItem{
					ID:        track.ID,
					Title:     track.Title,
					Subtitle:  track.ArtistNames(),
					Thumbnail: track.Thumbnail,
					Type:      "track",
					Track:     &track,
				})
				continue
			}
			// A podcast's related page lists its episodes as rows, not cards.
			if card, ok := cardFromItem(im); ok {
				items = append(items, card)
			}
		}
		if len(items) > 0 {
			sections = append(sections, Section{Title: title, Items: items})
		}
	}
	return sections
}

func findCarouselShelves(v any) []map[string]any {
	var shelves []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch m := x.(type) {
		case map[string]any:
			if shelf, ok := m["musicCarouselShelfRenderer"].(map[string]any); ok {
				shelves = append(shelves, shelf)
				return
			}
			for _, child := range m {
				walk(child)
			}
		case []any:
			for _, child := range m {
				walk(child)
			}
		}
	}
	walk(v)
	return shelves
}
