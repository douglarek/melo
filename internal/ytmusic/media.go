package ytmusic

import "strings"

// podcastEpisodeID returns the video ID of a podcast episode's page, which
// its browse ID carries as MPED<video ID>.
func podcastEpisodeID(browseID string) (string, bool) {
	videoID, ok := strings.CutPrefix(browseID, "MPED")
	if !ok || videoID == "" {
		return "", false
	}
	return videoID, true
}

// IsPodcastShowID reports whether a browse ID names a podcast show, whose page
// lists episodes rather than songs: YouTube Music writes those as
// MPSPPL<show>.
func IsPodcastShowID(browseID string) bool {
	return strings.HasPrefix(browseID, "MPSPPL")
}

// itemHasMusicVideo reports whether any navigation endpoint in an item points
// at a music video. YouTube Music puts musicVideoType on the item endpoint,
// the title runs, or the play overlay depending on the renderer, so all three
// are checked in one place.
func itemHasMusicVideo(item map[string]any) bool {
	for _, nav := range itemNavigationEndpoints(item) {
		if hasMusicVideo(nav) {
			return true
		}
	}
	return false
}

// itemHasPodcastEpisode reports whether any navigation endpoint in an item
// names a podcast episode, whose sound YouTube serves from the show's RSS
// feed rather than its own.
func itemHasPodcastEpisode(item map[string]any) bool {
	for _, nav := range itemNavigationEndpoints(item) {
		if endpointMusicVideoType(nav) == "MUSIC_VIDEO_TYPE_PODCAST_EPISODE" {
			return true
		}
	}
	return false
}

// podcastShowArtists returns the show a row names, which is the artist its
// episode plays under: YouTube Music puts it beside the episode's date, with
// a link to the show's page.
func podcastShowArtists(item map[string]any) []ArtistRef {
	for _, field := range []string{"secondTitle", "subtitle"} {
		wrap, _ := item[field].(map[string]any)
		runs, _ := wrap["runs"].([]any)
		for _, raw := range runs {
			run, _ := raw.(map[string]any)
			nav, _ := run["navigationEndpoint"].(map[string]any)
			be, _ := nav["browseEndpoint"].(map[string]any)
			supported, _ := be["browseEndpointContextSupportedConfigs"].(map[string]any)
			config, _ := supported["browseEndpointContextMusicConfig"].(map[string]any)
			if pageType, _ := config["pageType"].(string); pageType != "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE" {
				continue
			}
			showID, _ := be["browseId"].(string)
			if text, _ := run["text"].(string); text != "" {
				return []ArtistRef{{ID: showID, Name: text}}
			}
		}
	}
	return nil
}

// endpointMusicVideoType returns the musicVideoType of a watch endpoint, or
// "" when it carries none.
func endpointMusicVideoType(nav map[string]any) string {
	if nav == nil {
		return ""
	}
	watch, _ := nav["watchEndpoint"].(map[string]any)
	supported, _ := watch["watchEndpointMusicSupportedConfigs"].(map[string]any)
	config, _ := supported["watchEndpointMusicConfig"].(map[string]any)
	videoType, _ := config["musicVideoType"].(string)
	return videoType
}

// hasMusicVideo reports whether a watch endpoint is a music video rather
// than an audio art track.
func hasMusicVideo(nav map[string]any) bool {
	videoType := endpointMusicVideoType(nav)
	return videoType != "" && videoType != "MUSIC_VIDEO_TYPE_ATV"
}

func itemNavigationEndpoints(item map[string]any) []map[string]any {
	if item == nil {
		return nil
	}

	var navs []map[string]any
	add := func(raw any) {
		if nav, ok := raw.(map[string]any); ok && nav != nil {
			navs = append(navs, nav)
		}
	}
	addRuns := func(raw any) {
		runs, _ := raw.([]any)
		for _, rawRun := range runs {
			run, _ := rawRun.(map[string]any)
			add(run["navigationEndpoint"])
		}
	}

	add(item["navigationEndpoint"])
	add(item["onTap"])

	if overlay, ok := item["overlay"].(map[string]any); ok {
		thumb, _ := overlay["musicItemThumbnailOverlayRenderer"].(map[string]any)
		content, _ := thumb["content"].(map[string]any)
		play, _ := content["musicPlayButtonRenderer"].(map[string]any)
		add(play["playNavigationEndpoint"])
	}

	if title, ok := item["title"].(map[string]any); ok {
		addRuns(title["runs"])
	}
	if flexCols, ok := item["flexColumns"].([]any); ok {
		for _, rawCol := range flexCols {
			col, _ := rawCol.(map[string]any)
			flex, _ := col["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
			text, _ := flex["text"].(map[string]any)
			addRuns(text["runs"])
		}
	}
	return navs
}
