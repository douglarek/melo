package ytmusic

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
