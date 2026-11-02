package ytmusic

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// BrowseHome retrieves home recommendations and carousels.
func (c *Client) BrowseHome(ctx context.Context) ([]Section, error) {
	return c.browseSections(ctx, "FEmusic_home")
}

// BrowseExplore retrieves new releases, moods and genres.
func (c *Client) BrowseExplore(ctx context.Context) ([]Section, error) {
	return c.browseSections(ctx, "FEmusic_explore")
}

// BrowseCharts retrieves the charts for the region YouTube Music chooses.
func (c *Client) BrowseCharts(ctx context.Context) ([]Section, error) {
	page, err := c.BrowseChartsPage(ctx, "")
	if err != nil {
		return nil, err
	}
	return page.Sections, nil
}

// BrowseChartsPage retrieves the charts for country and the country menu.
// An empty country keeps the region YouTube Music chooses.
func (c *Client) BrowseChartsPage(ctx context.Context, country string) (*ChartsPage, error) {
	body := map[string]any{
		"browseId": "FEmusic_charts",
	}
	if country != "" {
		body["formData"] = map[string]any{"selectedValues": []string{country}}
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	page := &ChartsPage{Sections: parseSections(root)}
	page.Countries, page.Selected = parseChartCountries(root)
	return page, nil
}

// BrowseCategory retrieves a mood or genre page, such as one opened from the
// moods and genres grid on Explore.
func (c *Client) BrowseCategory(ctx context.Context, browseID, params string) (*Category, error) {
	body := map[string]any{
		"browseId": browseID,
	}
	if params != "" {
		body["params"] = params
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	cat := &Category{ID: browseID}
	if header, ok := root["header"].(map[string]any)["musicHeaderRenderer"].(map[string]any); ok {
		cat.Title = RunsToString(header["title"])
	}
	cat.Sections = parseSections(root)
	return cat, nil
}

func (c *Client) browseSections(ctx context.Context, browseID string) ([]Section, error) {
	body := map[string]any{
		"browseId": browseID,
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	return parseSections(root), nil
}

func parseChartCountries(root map[string]any) ([]ChartCountry, string) {
	contents, _ := root["contents"].(map[string]any)
	single, _ := contents["singleColumnBrowseResultsRenderer"].(map[string]any)
	tabs, _ := single["tabs"].([]any)
	if len(tabs) == 0 {
		return nil, ""
	}
	tab0, _ := tabs[0].(map[string]any)
	sectionList, _ := tab0["tabRenderer"].(map[string]any)["content"].(map[string]any)["sectionListRenderer"].(map[string]any)
	rawSections, _ := sectionList["contents"].([]any)
	if len(rawSections) == 0 {
		return nil, ""
	}

	shelf, _ := rawSections[0].(map[string]any)["musicShelfRenderer"].(map[string]any)
	subheaders, _ := shelf["subheaders"].([]any)
	if len(subheaders) == 0 {
		return nil, ""
	}
	startItems, _ := subheaders[0].(map[string]any)["musicSideAlignedItemRenderer"].(map[string]any)["startItems"].([]any)
	if len(startItems) == 0 {
		return nil, ""
	}
	button, _ := startItems[0].(map[string]any)["musicSortFilterButtonRenderer"].(map[string]any)
	menu, _ := button["menu"].(map[string]any)["musicMultiSelectMenuRenderer"].(map[string]any)
	options, _ := menu["options"].([]any)

	codes, selected := chartCountryTokens(root)
	selectedCode := ""
	countries := make([]ChartCountry, 0, len(options))
	for _, raw := range options {
		option, _ := raw.(map[string]any)["musicMultiSelectMenuItemRenderer"].(map[string]any)
		if option == nil {
			continue
		}
		name := RunsToString(option["title"])
		if name == "" {
			continue
		}
		entityKey, _ := option["formItemEntityKey"].(string)
		code := codes[entityKey]
		if _, has := option["selectedCommand"]; !has {
			selectedCode = code
		}
		countries = append(countries, ChartCountry{Code: code, Name: name})
	}
	if selected == "" {
		selected = selectedCode
	}
	return countries, selected
}

func chartCountryTokens(root map[string]any) (map[string]string, string) {
	updates, _ := root["frameworkUpdates"].(map[string]any)
	batch, _ := updates["entityBatchUpdate"].(map[string]any)
	mutations, _ := batch["mutations"].([]any)
	codes := map[string]string{}
	selected := ""
	for _, raw := range mutations {
		mutation, _ := raw.(map[string]any)
		choice, _ := mutation["payload"].(map[string]any)["musicFormBooleanChoice"].(map[string]any)
		code, _ := choice["opaqueToken"].(string)
		if code == "" {
			continue
		}
		entityKey, _ := mutation["entityKey"].(string)
		if entityKey != "" {
			codes[entityKey] = code
		}
		if on, _ := choice["selected"].(bool); on {
			selected = code
		}
	}
	return codes, selected
}

func parseSections(root map[string]any) []Section {
	var sections []Section
	contents, _ := root["contents"].(map[string]any)
	single, _ := contents["singleColumnBrowseResultsRenderer"].(map[string]any)
	tabs, _ := single["tabs"].([]any)
	if len(tabs) == 0 {
		return sections
	}

	tab0, _ := tabs[0].(map[string]any)
	sectionList, _ := tab0["tabRenderer"].(map[string]any)["content"].(map[string]any)["sectionListRenderer"].(map[string]any)
	rawSections, _ := sectionList["contents"].([]any)

	for _, rs := range rawSections {
		sm, ok := rs.(map[string]any)
		if !ok {
			continue
		}
		car, ok := sm["musicCarouselShelfRenderer"].(map[string]any)
		if !ok {
			// Might be musicShelfRenderer
			if shelf, ok := sm["musicShelfRenderer"].(map[string]any); ok {
				title := RunsToString(shelf["title"])
				cList, _ := shelf["contents"].([]any)
				items := parseShelfItems(cList)
				if len(items) > 0 {
					sections = append(sections, Section{Title: title, Items: items})
				}
			}
			continue
		}

		header, _ := car["header"].(map[string]any)["musicCarouselShelfBasicHeaderRenderer"].(map[string]any)
		title := RunsToString(header["title"])
		cList, _ := car["contents"].([]any)
		items := parseCarouselContents(cList)
		if len(items) > 0 {
			layout := ""
			if items[0].Type == "mood" {
				layout = "moods"
			}
			sections = append(sections, Section{
				Title:  title,
				Items:  items,
				Layout: layout,
			})
		}
	}

	return sections
}

func parseCarouselContents(rawItems []any) []CardItem {
	var items []CardItem
	for _, item := range rawItems {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := im["musicTwoRowItemRenderer"].(map[string]any); ok {
			ci := parseTwoRowItem(t)
			if ci.ID != "" || ci.Title != "" {
				items = append(items, ci)
			}
		} else if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
			ci := parseResponsiveAsCard(r)
			if ci.ID != "" || ci.Title != "" {
				items = append(items, ci)
			}
		} else if n, ok := im["musicNavigationButtonRenderer"].(map[string]any); ok {
			ci := parseNavigationButton(n)
			if ci.ID != "" || ci.Title != "" {
				items = append(items, ci)
			}
		}
	}
	return items
}

func parseNavigationButton(n map[string]any) CardItem {
	title := RunsToString(n["buttonText"])
	click, _ := n["clickCommand"].(map[string]any)
	endpoint, _ := click["browseEndpoint"].(map[string]any)
	id, _ := endpoint["browseId"].(string)
	params, _ := endpoint["params"].(string)

	var color uint32
	if solid, ok := n["solid"].(map[string]any); ok {
		if v, ok := solid["leftStripeColor"].(float64); ok {
			color = uint32(v)
		}
	}

	return CardItem{
		ID:     id,
		Title:  title,
		Type:   "mood",
		Params: params,
		Color:  color,
	}
}

func parseShelfItems(rawItems []any) []CardItem {
	var items []CardItem
	for _, item := range rawItems {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
			ci := parseResponsiveAsCard(r)
			if ci.ID != "" || ci.Title != "" {
				items = append(items, ci)
			}
		}
	}
	return items
}

func parseTwoRowItem(t map[string]any) CardItem {
	title := RunsToString(t["title"])
	subtitle := RunsToString(t["subtitle"])
	thumbs := extractThumbnail(t)
	thumbURL := BestThumbnail(thumbs)

	nav, _ := t["navigationEndpoint"].(map[string]any)
	be, _ := nav["browseEndpoint"].(map[string]any)
	browseID, _ := be["browseId"].(string)
	pageType := ""
	if supported, ok := be["browseEndpointContextSupportedConfigs"].(map[string]any); ok {
		if musicConfig, ok := supported["browseEndpointContextMusicConfig"].(map[string]any); ok {
			pageType, _ = musicConfig["pageType"].(string)
		}
	}

	we, _ := nav["watchEndpoint"].(map[string]any)
	videoID, _ := we["videoId"].(string)

	itemType := "playlist"
	id := browseID
	switch {
	case strings.HasPrefix(browseID, "UC") || pageType == "MUSIC_PAGE_TYPE_ARTIST":
		itemType = "artist"
	case strings.HasPrefix(browseID, "MPREb_") || pageType == "MUSIC_PAGE_TYPE_ALBUM":
		itemType = "album"
	case strings.HasPrefix(browseID, "VL") || strings.HasPrefix(browseID, "PL"):
		itemType = "playlist"
	case videoID != "":
		itemType = "track"
		id = videoID
	}

	var track *Track
	if itemType == "track" && videoID != "" {
		track = &Track{
			ID:         videoID,
			Title:      title,
			DurationMs: durationFromSubtitle(subtitle),
			Thumbnail:  thumbURL,
			Thumbnails: thumbs,
			Artists:    []ArtistRef{{Name: subtitle}},
			Video:      hasMusicVideo(nav),
		}
	}

	return CardItem{
		ID:        id,
		Title:     title,
		Subtitle:  subtitle,
		Thumbnail: thumbURL,
		Type:      itemType,
		Track:     track,
	}
}

// durationFromSubtitle finds a "4:43" segment in a "Artist • Album • 4:43"
// subtitle. Bare numbers are ignored so years are not mistaken for durations.
func durationFromSubtitle(subtitle string) int64 {
	for _, part := range strings.Split(subtitle, "•") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, ":") {
			if d := ParseDurationMs(part); d > 0 {
				return d
			}
		}
	}
	return 0
}

func parseResponsiveAsCard(r map[string]any) CardItem {
	flexCols, _ := r["flexColumns"].([]any)
	if len(flexCols) == 0 {
		return CardItem{}
	}
	col0, _ := flexCols[0].(map[string]any)
	col0Flex, _ := col0["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
	title := RunsToString(col0Flex["text"])

	var subtitle string
	var artists []ArtistRef
	if len(flexCols) > 1 {
		col1, _ := flexCols[1].(map[string]any)
		col1Flex, _ := col1["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		text, _ := col1Flex["text"].(map[string]any)
		runs, _ := text["runs"].([]any)
		subtitle = RunsToString(text)
		artists = extractArtistsFromRuns(runs)
		if len(artists) == 0 {
			artists = artistsFromSubtitle(subtitle)
		}
	}

	thumbs := extractThumbnail(r)
	thumbURL := BestThumbnail(thumbs)

	var videoID string
	if pid, ok := r["playlistItemData"].(map[string]any); ok {
		videoID, _ = pid["videoId"].(string)
	}
	if videoID == "" {
		videoID = extractOverlayVideoID(r)
	}

	var browseID, pageType string
	var watchNav map[string]any
	if nav, ok := r["navigationEndpoint"].(map[string]any); ok && nav != nil {
		watchNav = nav
		if be, ok := nav["browseEndpoint"].(map[string]any); ok && be != nil {
			browseID, _ = be["browseId"].(string)
			if supported, ok := be["browseEndpointContextSupportedConfigs"].(map[string]any); ok {
				if musicConfig, ok := supported["browseEndpointContextMusicConfig"].(map[string]any); ok {
					pageType, _ = musicConfig["pageType"].(string)
				}
			}
		}
	}
	if browseID == "" && len(flexCols) > 0 {
		col0, _ := flexCols[0].(map[string]any)
		col0Flex, _ := col0["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		if runs, ok := col0Flex["text"].(map[string]any)["runs"].([]any); ok && len(runs) > 0 {
			if run0, ok := runs[0].(map[string]any); ok {
				nav, _ := run0["navigationEndpoint"].(map[string]any)
				if nav != nil {
					watchNav = nav
				}
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

	itemType := "playlist"
	id := browseID
	switch {
	case strings.HasPrefix(browseID, "UC") || pageType == "MUSIC_PAGE_TYPE_ARTIST":
		itemType = "artist"
	case strings.HasPrefix(browseID, "MPREb_") || pageType == "MUSIC_PAGE_TYPE_ALBUM":
		itemType = "album"
	case strings.HasPrefix(browseID, "VL") || strings.HasPrefix(browseID, "PL") || pageType == "MUSIC_PAGE_TYPE_PLAYLIST":
		itemType = "playlist"
	case videoID != "":
		itemType = "track"
		id = videoID
	}

	var track *Track
	if itemType == "track" && videoID != "" {
		track = &Track{
			ID:         videoID,
			Title:      title,
			DurationMs: extractDuration(r, flexCols),
			Thumbnail:  thumbURL,
			Thumbnails: thumbs,
			Artists:    artists,
			Video:      hasMusicVideo(watchNav),
		}
	}

	return CardItem{
		ID:        id,
		Title:     title,
		Subtitle:  subtitle,
		Thumbnail: thumbURL,
		Type:      itemType,
		Track:     track,
	}
}

// artistsFromSubtitle extracts artist names from a plain "Song • Artist • 3:45"
// subtitle when the renderer gives no run-level navigation endpoints.
func artistsFromSubtitle(subtitle string) []ArtistRef {
	var artists []ArtistRef
	for _, part := range strings.Split(subtitle, "•") {
		part = strings.TrimSpace(part)
		if part == "" || part == "歌曲" || part == "视频" || strings.Contains(part, ":") {
			continue
		}
		artists = append(artists, ArtistRef{Name: part})
	}
	return artists
}

// BrowsePlaylist loads playlist metadata and its track listing.
func (c *Client) BrowsePlaylist(ctx context.Context, playlistID string) (*Playlist, error) {
	browseID := playlistID
	if !strings.HasPrefix(browseID, "VL") && strings.HasPrefix(browseID, "PL") {
		browseID = "VL" + browseID
	}
	body := map[string]any{
		"browseId": browseID,
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	pl := &Playlist{
		ID: playlistID,
	}

	contents, _ := root["contents"].(map[string]any)

	// Check header
	headerObj := root["header"]
	if headerObj == nil {
		// Might be in twoColumnBrowseResultsRenderer
		tc, _ := contents["twoColumnBrowseResultsRenderer"].(map[string]any)
		if tc != nil {
			tabs, _ := tc["tabs"].([]any)
			if len(tabs) > 0 {
				tab0, _ := tabs[0].(map[string]any)
				if tr, ok := tab0["tabRenderer"].(map[string]any); ok {
					if content, ok := tr["content"].(map[string]any); ok {
						if sl, ok := content["sectionListRenderer"].(map[string]any); ok {
							secContent, _ := sl["contents"].([]any)
							if len(secContent) > 0 {
								if sc0, ok := secContent[0].(map[string]any); ok {
									headerObj = sc0["musicResponsiveHeaderRenderer"]
								}
							}
						}
					}
				}
			}
		}
	}

	parsePlaylistHeader(headerObj, pl)

	// Tracks can be in secondaryContents or singleColumnBrowseResultsRenderer
	var rawTracks []any
	var continuation string
	if tc, ok := contents["twoColumnBrowseResultsRenderer"].(map[string]any); ok {
		sec, _ := tc["secondaryContents"].(map[string]any)["sectionListRenderer"].(map[string]any)["contents"].([]any)
		rawTracks, continuation = findShelfContents(sec)
	} else if sc, ok := contents["singleColumnBrowseResultsRenderer"].(map[string]any); ok {
		tabs, _ := sc["tabs"].([]any)
		if len(tabs) > 0 {
			tab0, _ := tabs[0].(map[string]any)
			sections, _ := tab0["tabRenderer"].(map[string]any)["content"].(map[string]any)["sectionListRenderer"].(map[string]any)["contents"].([]any)
			rawTracks, continuation = findShelfContents(sections)
		}
	}

	for _, item := range rawTracks {
		appendPlaylistItem(pl, item)
	}
	pl.Continuation = continuation
	if pl.TrackCount == 0 {
		pl.TrackCount = len(pl.Tracks)
	}

	return pl, nil
}

// PlaylistTracks loads one more page of a playlist from its continuation
// token. It returns the tracks and the token for the page after them.
func (c *Client) PlaylistTracks(ctx context.Context, continuation string) ([]Track, string, error) {
	data, err := c.post(ctx, "browse", map[string]any{"continuation": continuation}, nil)
	if err != nil {
		return nil, "", err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, "", err
	}
	items, next := continuationItems(root)
	pl := &Playlist{}
	for _, item := range items {
		appendPlaylistItem(pl, item)
	}
	return pl.Tracks, next, nil
}

func appendPlaylistItem(pl *Playlist, item any) {
	im, ok := item.(map[string]any)
	if !ok {
		return
	}
	if r, ok := im["musicResponsiveListItemRenderer"].(map[string]any); ok {
		track := parseTrackFromListItem(r)
		if track.ID != "" {
			pl.Tracks = append(pl.Tracks, track)
		}
		return
	}
	if r, ok := im["musicMultiRowListItemRenderer"].(map[string]any); ok {
		track := parseMultiRowTrack(r)
		if track.ID != "" {
			if len(track.Artists) == 0 && pl.Title != "" {
				track.Artists = []ArtistRef{{Name: pl.Title}}
			}
			pl.Tracks = append(pl.Tracks, track)
		}
	}
}

// continuationItems extracts playlist items and the next page token from a
// browse continuation response.
func continuationItems(root map[string]any) ([]any, string) {
	cc, _ := root["continuationContents"].(map[string]any)
	if cc != nil {
		for _, key := range []string{"musicShelfContinuation", "musicPlaylistShelfContinuation"} {
			shelf, ok := cc[key].(map[string]any)
			if !ok {
				continue
			}
			contents, _ := shelf["contents"].([]any)
			token := firstContinuation(shelf["continuations"])
			if token == "" {
				token = continuationItemToken(contents)
			}
			return contents, token
		}
	}

	// Liked Music and other owned playlists append their next page through
	// appendContinuationItemsAction.
	actions, _ := root["onResponseReceivedActions"].([]any)
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		appendAction, _ := action["appendContinuationItemsAction"].(map[string]any)
		if appendAction == nil {
			continue
		}
		items, _ := appendAction["continuationItems"].([]any)
		token := firstContinuation(appendAction["continuations"])
		if token == "" {
			token = continuationItemToken(items)
		}
		return items, token
	}

	return nil, ""
}

// continuationItemToken reads the next page token from the continuation item
// that large shelves append after their visible rows.
func continuationItemToken(items []any) string {
	for _, item := range items {
		im, _ := item.(map[string]any)
		ci, _ := im["continuationItemRenderer"].(map[string]any)
		endpoint, _ := ci["continuationEndpoint"].(map[string]any)
		command, _ := endpoint["continuationCommand"].(map[string]any)
		if token, _ := command["token"].(string); token != "" {
			return token
		}
	}
	return ""
}

func firstContinuation(raw any) string {
	items, _ := raw.([]any)
	for _, item := range items {
		im, _ := item.(map[string]any)
		if next, ok := im["nextContinuationData"].(map[string]any); ok {
			if token, ok := next["continuation"].(string); ok {
				return token
			}
		}
	}
	return ""
}

// parseMultiRowTrack parses podcast episode rows, which carry their video ID
// on the tap endpoint and their length as localized text such as "21分钟".
func parseMultiRowTrack(r map[string]any) Track {
	title := RunsToString(r["title"])

	var videoID string
	if tap, ok := r["onTap"].(map[string]any); ok {
		if we, ok := tap["watchEndpoint"].(map[string]any); ok {
			videoID, _ = we["videoId"].(string)
		}
	}
	if videoID == "" {
		videoID = extractOverlayVideoID(r)
	}

	durationMs := int64(0)
	if pp, ok := r["playbackProgress"].(map[string]any); ok {
		if m, ok := pp["musicPlaybackProgressRenderer"].(map[string]any); ok {
			durationMs = parseHumanDurationMs(RunsToString(m["durationText"]))
		}
	}
	if durationMs == 0 {
		durationMs = extractDuration(r, nil)
	}

	thumbs := extractThumbnail(r)
	return Track{
		ID:         videoID,
		Title:      title,
		DurationMs: durationMs,
		Thumbnail:  BestThumbnail(thumbs),
		Thumbnails: thumbs,
	}
}

var humanDurationReg = regexp.MustCompile(`(?i)(\d+)\s*(小时|小時|hours?|hrs?|分钟|分鐘|minutes?|mins?|时|時|分|秒|seconds?|secs?)`)

// parseHumanDurationMs parses localized lengths such as "21分钟",
// "1小时2分钟" or "1 hr 2 min".
func parseHumanDurationMs(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	if strings.Contains(text, ":") {
		if d := ParseDurationMs(text); d > 0 {
			return d
		}
	}

	var total int64
	for _, m := range humanDurationReg.FindAllStringSubmatch(text, -1) {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		unit := strings.ToLower(m[2])
		switch {
		case strings.Contains(unit, "小时") || strings.Contains(unit, "小時") || unit == "时" || unit == "時" || strings.HasPrefix(unit, "hour") || strings.HasPrefix(unit, "hr"):
			total += n * 3600 * 1000
		case strings.Contains(unit, "分钟") || strings.Contains(unit, "分鐘") || unit == "分" || strings.HasPrefix(unit, "min"):
			total += n * 60 * 1000
		case strings.Contains(unit, "秒") || strings.HasPrefix(unit, "sec"):
			total += n * 1000
		}
	}
	return total
}

func parsePlaylistHeader(raw any, pl *Playlist) {
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}
	title := RunsToString(m["title"])
	if title != "" {
		pl.Title = title
	}
	desc := RunsToString(m["description"])
	if desc != "" {
		pl.Description = desc
	}
	author := RunsToString(m["straplineTextOne"])
	if author == "" {
		author = RunsToString(m["subtitle"])
	}
	pl.Author = author

	if count := playlistTrackCount(m); count > 0 {
		pl.TrackCount = count
	}

	thumbs := extractThumbnail(m)
	pl.Thumbnail = BestThumbnail(thumbs)
}

var playlistCountRE = regexp.MustCompile(`(?i)([0-9][0-9,]*)\s*(?:首|songs?|tracks?)`)

func playlistTrackCount(header map[string]any) int {
	for _, key := range []string{"secondSubtitle", "subtitle"} {
		text := RunsToString(header[key])
		match := playlistCountRE.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		n, err := strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
		if err == nil {
			return n
		}
	}
	return 0
}

func parseTrackFromListItem(r map[string]any) Track {
	flexCols, _ := r["flexColumns"].([]any)
	if len(flexCols) == 0 {
		return Track{}
	}

	col0, _ := flexCols[0].(map[string]any)
	col0Flex, _ := col0["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
	title := RunsToString(col0Flex["text"])

	var artists []ArtistRef
	var album AlbumRef
	if len(flexCols) > 1 {
		col1, _ := flexCols[1].(map[string]any)
		col1Flex, _ := col1["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		col1Runs, _ := col1Flex["text"].(map[string]any)["runs"].([]any)
		artists = extractArtistsFromRuns(col1Runs)
	}
	if len(flexCols) > 2 {
		col2, _ := flexCols[2].(map[string]any)
		col2Flex, _ := col2["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		alName := RunsToString(col2Flex["text"])
		if alName != "" && !strings.Contains(alName, ":") {
			album.Name = alName
		}
	}

	durationMs := extractDuration(r, flexCols)

	var videoID string
	if pid, ok := r["playlistItemData"].(map[string]any); ok {
		videoID, _ = pid["videoId"].(string)
	}
	if videoID == "" {
		videoID = extractOverlayVideoID(r)
	}

	thumbs := extractThumbnail(r)
	nav, _ := r["navigationEndpoint"].(map[string]any)

	return Track{
		ID:         videoID,
		Title:      title,
		Artists:    artists,
		Album:      album,
		DurationMs: durationMs,
		Thumbnail:  BestThumbnail(thumbs),
		Thumbnails: thumbs,
		Video:      hasMusicVideo(nav),
	}
}

// BrowseAlbum loads album metadata and track listing.
func (c *Client) BrowseAlbum(ctx context.Context, albumID string) (*Album, error) {
	body := map[string]any{
		"browseId": albumID,
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	return parseAlbumResponse(data, albumID)
}

func parseAlbumResponse(data []byte, albumID string) (*Album, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	al := &Album{
		ID: albumID,
	}

	contents, _ := root["contents"].(map[string]any)
	tc, _ := contents["twoColumnBrowseResultsRenderer"].(map[string]any)
	if tc != nil {
		tabs, _ := tc["tabs"].([]any)
		if len(tabs) > 0 {
			tab0, _ := tabs[0].(map[string]any)
			if tr, ok := tab0["tabRenderer"].(map[string]any); ok {
				if content, ok := tr["content"].(map[string]any); ok {
					if sl, ok := content["sectionListRenderer"].(map[string]any); ok {
						sec, _ := sl["contents"].([]any)
						if len(sec) > 0 {
							if sc0, ok := sec[0].(map[string]any); ok {
								header, _ := sc0["musicResponsiveHeaderRenderer"].(map[string]any)
								if header != nil {
									al.Title = RunsToString(header["title"])
									al.Description = RunsToString(header["description"])
									al.Artists = albumArtistsFromHeader(header)
									thumbs := extractThumbnail(header)
									al.Thumbnail = BestThumbnail(thumbs)
									subtitle := RunsToString(header["subtitle"])
									subParts := strings.Split(subtitle, "•")
									if len(subParts) > 1 {
										al.Year = strings.TrimSpace(subParts[len(subParts)-1])
									}
								}
							}
						}
					}
				}
			}
		}
	}

	secList, _ := tc["secondaryContents"].(map[string]any)["sectionListRenderer"].(map[string]any)["contents"].([]any)
	for _, s := range secList {
		shelf, _ := s.(map[string]any)["musicShelfRenderer"].(map[string]any)
		if shelf == nil {
			shelf, _ = s.(map[string]any)["musicPlaylistShelfRenderer"].(map[string]any)
		}
		if shelf == nil {
			continue
		}
		rawTracks, _ := shelf["contents"].([]any)
		for _, item := range rawTracks {
			r, _ := item.(map[string]any)["musicResponsiveListItemRenderer"].(map[string]any)
			if r != nil {
				track := parseTrackFromListItem(r)
				if track.ID != "" {
					track.Album = AlbumRef{ID: albumID, Name: al.Title}
					if len(track.Artists) == 0 {
						track.Artists = al.Artists
					}
					if track.Thumbnail == "" {
						track.Thumbnail = al.Thumbnail
					}
					al.Tracks = append(al.Tracks, track)
				}
			}
		}
	}

	return al, nil
}

// albumArtistsFromHeader reads the artist runs of an album header. A single's
// strapline may hold only the artist, while an album's may start with the
// "Album" or "单曲" label.
func albumArtistsFromHeader(header map[string]any) []ArtistRef {
	strapline, _ := header["straplineTextOne"].(map[string]any)
	runs, _ := strapline["runs"].([]any)
	var artists []ArtistRef
	for _, raw := range runs {
		run, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := run["text"].(string)
		text = strings.TrimSpace(text)
		if text == "" || albumHeaderLabel(text) {
			continue
		}
		nav, _ := run["navigationEndpoint"].(map[string]any)
		be, _ := nav["browseEndpoint"].(map[string]any)
		browseID, _ := be["browseId"].(string)
		artists = append(artists, ArtistRef{ID: browseID, Name: text})
	}
	return artists
}

func albumHeaderLabel(text string) bool {
	switch text {
	case "•", ",", "&", "专辑", "单曲", "EP", "Album", "Single", "歌曲", "视频":
		return true
	}
	if strings.HasSuffix(text, "年") {
		if _, err := strconv.Atoi(strings.TrimSuffix(text, "年")); err == nil {
			return true
		}
	}
	return false
}

// BrowseArtist loads artist profile, top tracks, albums, and singles.
func (c *Client) BrowseArtist(ctx context.Context, artistID string) (*Artist, error) {
	body := map[string]any{
		"browseId": artistID,
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	art := &Artist{
		ID: artistID,
	}

	header, _ := root["header"].(map[string]any)["musicImmersiveHeaderRenderer"].(map[string]any)
	if header != nil {
		art.Name = RunsToString(header["title"])
		art.Description = RunsToString(header["description"])
		if subBtn, ok := header["subscriptionButton"].(map[string]any); ok {
			if sbr, ok := subBtn["subscribeButtonRenderer"].(map[string]any); ok {
				art.Subscribers = RunsToString(sbr["subscriberCountText"])
			}
		}
		thumbs := extractThumbnail(header)
		art.Thumbnail = BestThumbnail(thumbs)
	}

	contents, _ := root["contents"].(map[string]any)["singleColumnBrowseResultsRenderer"].(map[string]any)["tabs"].([]any)
	if len(contents) > 0 {
		sections, _ := contents[0].(map[string]any)["tabRenderer"].(map[string]any)["content"].(map[string]any)["sectionListRenderer"].(map[string]any)["contents"].([]any)
		for _, s := range sections {
			sm, ok := s.(map[string]any)
			if !ok {
				continue
			}
			// MusicShelfRenderer (Songs)
			if shelf, ok := sm["musicShelfRenderer"].(map[string]any); ok {
				title := RunsToString(shelf["title"].(map[string]any)["runs"])
				if strings.Contains(title, "歌曲") || strings.Contains(title, "Songs") || strings.Contains(title, "Top") {
					items, _ := shelf["contents"].([]any)
					for _, item := range items {
						r, _ := item.(map[string]any)["musicResponsiveListItemRenderer"].(map[string]any)
						if r != nil {
							track := parseTrackFromListItem(r)
							if track.ID != "" {
								art.TopTracks = append(art.TopTracks, track)
							}
						}
					}
				}
			}
			// MusicCarouselShelfRenderer (Albums, Singles)
			if car, ok := sm["musicCarouselShelfRenderer"].(map[string]any); ok {
				header, _ := car["header"].(map[string]any)["musicCarouselShelfBasicHeaderRenderer"].(map[string]any)
				title := RunsToString(header["title"].(map[string]any)["runs"])
				items, _ := car["contents"].([]any)
				for _, item := range items {
					twoRow, _ := item.(map[string]any)["musicTwoRowItemRenderer"].(map[string]any)
					if twoRow == nil {
						continue
					}
					ci := parseTwoRowItem(twoRow)
					ai := AlbumItem{
						ID:        ci.ID,
						Title:     ci.Title,
						Artist:    art.Name,
						Thumbnail: ci.Thumbnail,
						Year:      ci.Subtitle,
						Type:      ci.Type,
						Track:     ci.Track,
					}
					switch ai.Type {
					case "album", "artist", "playlist", "track":
					default:
						if strings.Contains(title, "专辑") || strings.Contains(title, "Albums") {
							ai.Type = "Album"
						} else {
							ai.Type = "Single"
						}
					}
					if strings.Contains(title, "专辑") || strings.Contains(title, "Albums") {
						art.Albums = append(art.Albums, ai)
					} else {
						art.Singles = append(art.Singles, ai)
					}
				}
			}
		}
	}

	return art, nil
}

func findShelfContents(sections []any) ([]any, string) {
	for _, s := range sections {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if shelf, ok := sm["musicPlaylistShelfRenderer"].(map[string]any); ok {
			if c, token := shelfPage(shelf); c != nil {
				return c, token
			}
		}
		if shelf, ok := sm["musicShelfRenderer"].(map[string]any); ok {
			if c, token := shelfPage(shelf); c != nil {
				return c, token
			}
		}
		if isr, ok := sm["itemSectionRenderer"].(map[string]any); ok {
			if isrContents, ok := isr["contents"].([]any); ok {
				for _, it := range isrContents {
					itm, ok := it.(map[string]any)
					if !ok {
						continue
					}
					if shelf, ok := itm["musicPlaylistShelfRenderer"].(map[string]any); ok {
						if c, token := shelfPage(shelf); c != nil {
							return c, token
						}
					}
					if shelf, ok := itm["musicShelfRenderer"].(map[string]any); ok {
						if c, token := shelfPage(shelf); c != nil {
							return c, token
						}
					}
				}
			}
		}
	}
	return nil, ""
}

func shelfPage(shelf map[string]any) ([]any, string) {
	contents, ok := shelf["contents"].([]any)
	if !ok {
		return nil, ""
	}
	token := firstContinuation(shelf["continuations"])
	if token == "" {
		token = continuationItemToken(contents)
	}
	return contents, token
}

func findGridItems(sections []any) []any {
	var allItems []any
	for _, s := range sections {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		for _, k := range []string{"gridRenderer", "musicGridRenderer", "musicShelfRenderer"} {
			if g, ok := sm[k].(map[string]any); ok {
				if items, ok := g["items"].([]any); ok {
					allItems = append(allItems, items...)
				} else if contents, ok := g["contents"].([]any); ok {
					allItems = append(allItems, contents...)
				}
			}
		}
		if isr, ok := sm["itemSectionRenderer"].(map[string]any); ok {
			if isrContents, ok := isr["contents"].([]any); ok {
				for _, it := range isrContents {
					itm, ok := it.(map[string]any)
					if !ok {
						continue
					}
					for _, k := range []string{"gridRenderer", "musicGridRenderer", "musicShelfRenderer"} {
						if g, ok := itm[k].(map[string]any); ok {
							if items, ok := g["items"].([]any); ok {
								allItems = append(allItems, items...)
							} else if contents, ok := g["contents"].([]any); ok {
								allItems = append(allItems, contents...)
							}
						}
					}
				}
			}
		}
	}
	return allItems
}

// BrowseGuide retrieves the sidebar playlists from YouTube Music.
func (c *Client) BrowseGuide(ctx context.Context) ([]PlaylistItem, error) {
	data, err := c.post(ctx, "guide", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	var playlists []PlaylistItem
	items, _ := root["items"].([]any)
	for _, it := range items {
		itm, ok := it.(map[string]any)
		if !ok {
			continue
		}
		sec, ok := itm["guideSectionRenderer"].(map[string]any)
		if !ok {
			continue
		}
		secItems, _ := sec["items"].([]any)
		for _, si := range secItems {
			sim, ok := si.(map[string]any)
			if !ok {
				continue
			}
			entry, ok := sim["guideEntryRenderer"].(map[string]any)
			if !ok {
				continue
			}
			title := RunsToString(entry["formattedTitle"])
			nav, _ := entry["navigationEndpoint"].(map[string]any)
			be, _ := nav["browseEndpoint"].(map[string]any)
			bid, _ := be["browseId"].(string)

			if bid == "FEmusic_home" || bid == "FEmusic_explore" || bid == "FEmusic_library_landing" {
				continue
			}

			if title != "" && bid != "" {
				thumbs := extractThumbnail(entry)
				playlists = append(playlists, PlaylistItem{
					ID:        bid,
					Title:     title,
					Thumbnail: BestThumbnail(thumbs),
				})
			}
		}
	}
	return playlists, nil
}

// BrowseLibraryPlaylists retrieves the user's saved library playlists.
func (c *Client) BrowseLibraryPlaylists(ctx context.Context) ([]PlaylistItem, error) {
	var playlists []PlaylistItem
	seen := map[string]bool{}

	// 1. First source: Guide endpoint (sidebar playlists shown in YouTube Music)
	guideItems, _ := c.BrowseGuide(ctx)
	for _, pl := range guideItems {
		if !seen[pl.ID] {
			seen[pl.ID] = true
			playlists = append(playlists, pl)
		}
	}

	// 2. Second source: FEmusic_liked_playlists browse endpoint
	body := map[string]any{
		"browseId": "FEmusic_liked_playlists",
	}
	data, err := c.post(ctx, "browse", body, nil)
	if err == nil {
		var root map[string]any
		if json.Unmarshal(data, &root) == nil {
			contents, _ := root["contents"].(map[string]any)
			sc, _ := contents["singleColumnBrowseResultsRenderer"].(map[string]any)
			if sc != nil {
				tabs, _ := sc["tabs"].([]any)
				if len(tabs) > 0 {
					tab0, _ := tabs[0].(map[string]any)
					tr, _ := tab0["tabRenderer"].(map[string]any)
					if tr != nil {
						content, _ := tr["content"].(map[string]any)
						if content != nil {
							sl, _ := content["sectionListRenderer"].(map[string]any)
							if sl != nil {
								secList, _ := sl["contents"].([]any)
								items := findGridItems(secList)
								for _, it := range items {
									itm, ok := it.(map[string]any)
									if !ok {
										continue
									}
									if twoRow, ok := itm["musicTwoRowItemRenderer"].(map[string]any); ok {
										ci := parseTwoRowItem(twoRow)
										if ci.ID != "" && ci.Title != "" && !seen[ci.ID] {
											seen[ci.ID] = true
											playlists = append(playlists, PlaylistItem{
												ID:         ci.ID,
												Title:      ci.Title,
												Author:     ci.Subtitle,
												Thumbnail:  ci.Thumbnail,
												TrackCount: ci.Subtitle,
											})
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return playlists, nil
}

// BrowseLikedSongs retrieves the authenticated user's Liked Music playlist.
func (c *Client) BrowseLikedSongs(ctx context.Context) (*Playlist, error) {
	pl, err := c.BrowsePlaylist(ctx, "VLLM")
	if err != nil || len(pl.Tracks) == 0 {
		return c.BrowsePlaylist(ctx, "LM")
	}
	return pl, nil
}

// LikeSong adds a song to the user's liked music in YouTube Music.
func (c *Client) LikeSong(ctx context.Context, videoID string) error {
	body := map[string]any{
		"target": map[string]any{
			"videoId": videoID,
		},
	}
	_, err := c.post(ctx, "like/like", body, nil)
	return err
}

// UnlikeSong removes a song from the user's liked music in YouTube Music.
func (c *Client) UnlikeSong(ctx context.Context, videoID string) error {
	body := map[string]any{
		"target": map[string]any{
			"videoId": videoID,
		},
	}
	_, err := c.post(ctx, "like/removelike", body, nil)
	return err
}

// hasMusicVideo reports whether a watch endpoint is a music video rather
// than an audio art track.
func hasMusicVideo(nav map[string]any) bool {
	if nav == nil {
		return false
	}
	watch, _ := nav["watchEndpoint"].(map[string]any)
	supported, _ := watch["watchEndpointMusicSupportedConfigs"].(map[string]any)
	config, _ := supported["watchEndpointMusicConfig"].(map[string]any)
	videoType, _ := config["musicVideoType"].(string)
	return videoType != "" && videoType != "MUSIC_VIDEO_TYPE_ATV"
}
