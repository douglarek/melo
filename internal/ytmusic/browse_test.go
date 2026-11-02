package ytmusic

import (
	"encoding/json"
	"testing"
)

func TestParseAlbumKeepsHeaderArtistOnTracks(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"musicResponsiveHeaderRenderer": {
										"title": {"runs": [{"text": "琥珀色的情话"}]},
										"straplineTextOne": {
											"runs": [
												{"text": "专辑", "navigationEndpoint": {"browseEndpoint": {"browseId": "FEmusic_new_releases"}}},
												{"text": " • "},
												{"text": "周传雄", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCGh_v5aXDXCanMfU6O31JXw"}}}
											]
										},
										"subtitle": {"runs": [{"text": "单曲"}, {"text": " • "}, {"text": "2026年"}]},
										"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
											{"url": "https://example/cover.jpg", "width": 544, "height": 544}
										]}}}
									}
								}]
							}
						}
					}
				}],
				"secondaryContents": {
					"sectionListRenderer": {
						"contents": [{
							"musicShelfRenderer": {
								"contents": [{
									"musicResponsiveListItemRenderer": {
										"flexColumns": [
											{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "琥珀色的情话"}]}}},
											{"musicResponsiveListItemFlexColumnRenderer": {"text": {}}},
											{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "播放次数：4.8万"}]}}}
										],
										"fixedColumns": [{
											"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "4:34"}]}}
										}],
										"playlistItemData": {"videoId": "HZKq239-Bpk"}
									}
								}]
							}
						}]
					}
				}
			}
		}
	}`)

	al, err := parseAlbumResponse(raw, "MPREb_nLfdlDLvDnQ")
	if err != nil {
		t.Fatalf("parseAlbumResponse: %v", err)
	}

	if len(al.Artists) != 1 || al.Artists[0].Name != "周传雄" || al.Artists[0].ID != "UCGh_v5aXDXCanMfU6O31JXw" {
		t.Fatalf("album artists = %+v, want 周传雄", al.Artists)
	}
	if al.Year != "2026年" {
		t.Errorf("album year = %q, want 2026年", al.Year)
	}
	if len(al.Tracks) != 1 {
		t.Fatalf("album tracks = %d, want 1", len(al.Tracks))
	}
	track := al.Tracks[0]
	if len(track.Artists) != 1 || track.Artists[0].Name != "周传雄" {
		t.Errorf("track artists = %+v, want the album artist", track.Artists)
	}
	if track.Album.ID != "MPREb_nLfdlDLvDnQ" || track.Album.Name != "琥珀色的情话" {
		t.Errorf("track album = %+v", track.Album)
	}
	if track.DurationMs != 274000 {
		t.Errorf("track duration = %d, want 274000", track.DurationMs)
	}
	if track.Thumbnail == "" || track.Thumbnail != al.Thumbnail {
		t.Errorf("track thumbnail = %q, album thumbnail = %q", track.Thumbnail, al.Thumbnail)
	}
}

func TestParseSectionsKeepsMoodsAndGenres(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"musicCarouselShelfRenderer": {
										"header": {
											"musicCarouselShelfBasicHeaderRenderer": {
												"title": {"runs": [{"text": "心情与流派"}]}
											}
										},
										"contents": [{
											"musicNavigationButtonRenderer": {
												"buttonText": {"runs": [{"text": "华语/粤语流行音乐"}]},
												"clickCommand": {
													"browseEndpoint": {
														"browseId": "FEmusic_moods_and_genres_category",
														"params": "ggMPOg1uX2hXVUQwc0JTNXlE"
													}
												},
												"solid": {"leftStripeColor": 4294936963}
											}
										}]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(sections))
	}
	sec := sections[0]
	if sec.Title != "心情与流派" || sec.Layout != "moods" {
		t.Fatalf("section = %+v", sec)
	}
	if len(sec.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(sec.Items))
	}
	item := sec.Items[0]
	if item.Type != "mood" || item.Title != "华语/粤语流行音乐" || item.Params != "ggMPOg1uX2hXVUQwc0JTNXlE" || item.Color != 4294936963 {
		t.Fatalf("mood item = %+v", item)
	}
}

func TestParseSectionsKeepsGridShelves(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"gridRenderer": {
										"header": {
											"gridHeaderRenderer": {
												"title": {"runs": [{"text": "精选播放列表"}]}
											}
										},
										"items": [{
											"musicTwoRowItemRenderer": {
												"title": {"runs": [{"text": "1980年代西洋金曲精选"}]},
												"subtitle": {"runs": [{"text": "Michael Jackson"}]},
												"navigationEndpoint": {
													"browseEndpoint": {
														"browseId": "VLRDCLAK5uy_lMzHW51iFg1Kx0d_2EHpzbOgCrwtu8cgI",
														"browseEndpointContextSupportedConfigs": {
															"browseEndpointContextMusicConfig": {
																"pageType": "MUSIC_PAGE_TYPE_PLAYLIST"
															}
														}
													}
												}
											}
										}]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(sections))
	}
	sec := sections[0]
	if sec.Title != "精选播放列表" || len(sec.Items) != 1 {
		t.Fatalf("section = %+v", sec)
	}
	item := sec.Items[0]
	if item.Type != "playlist" || item.Title != "1980年代西洋金曲精选" {
		t.Fatalf("grid item = %+v", item)
	}
}

func TestParseSectionsHandlesGridWithoutHeader(t *testing.T) {
	root := map[string]any{
		"contents": map[string]any{
			"singleColumnBrowseResultsRenderer": map[string]any{
				"tabs": []any{map[string]any{
					"tabRenderer": map[string]any{
						"content": map[string]any{
							"sectionListRenderer": map[string]any{
								"contents": []any{map[string]any{
									"gridRenderer": map[string]any{
										"items": []any{map[string]any{
											"musicTwoRowItemRenderer": map[string]any{
												"title": map[string]any{"runs": []any{map[string]any{"text": "无标题网格"}}},
												"navigationEndpoint": map[string]any{
													"browseEndpoint": map[string]any{"browseId": "VL123"},
												},
											},
										}},
									},
								}},
							},
						},
					},
				}},
			},
		},
	}

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(sections))
	}
	if sections[0].Title != "" || len(sections[0].Items) != 1 || sections[0].Items[0].Title != "无标题网格" {
		t.Fatalf("section = %+v", sections[0])
	}
}

func TestContinuationItemsReadsAppendAction(t *testing.T) {
	root := map[string]any{
		"onResponseReceivedActions": []any{
			map[string]any{
				"appendContinuationItemsAction": map[string]any{
					"continuationItems": []any{
						map[string]any{"musicResponsiveListItemRenderer": map[string]any{}},
						map[string]any{"continuationItemRenderer": map[string]any{
							"continuationEndpoint": map[string]any{
								"continuationCommand": map[string]any{"token": "next-page"},
							},
						}},
					},
				},
			},
		},
	}
	items, token := continuationItems(root)
	if len(items) != 2 || token != "next-page" {
		t.Fatalf("items=%d token=%q", len(items), token)
	}
}

func TestFindShelfContentsReadsContinuationItem(t *testing.T) {
	shelf := map[string]any{
		"contents": []any{
			map[string]any{"musicResponsiveListItemRenderer": map[string]any{}},
			map[string]any{"continuationItemRenderer": map[string]any{
				"continuationEndpoint": map[string]any{
					"continuationCommand": map[string]any{"token": "next-page"},
				},
			}},
		},
	}
	items, token := findShelfContents([]any{
		map[string]any{"musicPlaylistShelfRenderer": shelf},
	})
	if len(items) != 2 || token != "next-page" {
		t.Fatalf("items=%d token=%q", len(items), token)
	}
}

func TestPlaylistTrackCount(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"134 首歌曲 • 超过 6 小时", 134},
		{"1,234 songs • 8 hours", 1234},
		{"自动播放列表 • 2026", 0},
	}
	for _, tc := range tests {
		header := map[string]any{
			"secondSubtitle": map[string]any{"runs": []any{map[string]any{"text": tc.text}}},
		}
		if got := playlistTrackCount(header); got != tc.want {
			t.Errorf("playlistTrackCount(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestParseChartCountriesMapsByEntityKey(t *testing.T) {
	option := func(name, key string, selected bool) map[string]any {
		item := map[string]any{
			"title":             map[string]any{"runs": []any{map[string]any{"text": name}}},
			"formItemEntityKey": key,
		}
		if !selected {
			item["selectedCommand"] = map[string]any{}
		}
		return map[string]any{"musicMultiSelectMenuItemRenderer": item}
	}
	choice := func(key, code string, selected bool) map[string]any {
		return map[string]any{
			"entityKey": key,
			"payload": map[string]any{
				"musicFormBooleanChoice": map[string]any{
					"opaqueToken": code,
					"selected":    selected,
				},
			},
		}
	}

	root := map[string]any{
		"contents": map[string]any{
			"singleColumnBrowseResultsRenderer": map[string]any{
				"tabs": []any{map[string]any{
					"tabRenderer": map[string]any{
						"content": map[string]any{
							"sectionListRenderer": map[string]any{
								"contents": []any{map[string]any{
									"musicShelfRenderer": map[string]any{
										"subheaders": []any{map[string]any{
											"musicSideAlignedItemRenderer": map[string]any{
												"startItems": []any{map[string]any{
													"musicSortFilterButtonRenderer": map[string]any{
														"menu": map[string]any{
															"musicMultiSelectMenuRenderer": map[string]any{
																"options": []any{
																	option("印度", "key-in", true),
																	option("印度尼西亚", "key-id", false),
																},
															},
														},
													},
												}},
											},
										}},
									},
								}},
							},
						},
					},
				}},
			},
		},
		"frameworkUpdates": map[string]any{
			"entityBatchUpdate": map[string]any{
				// Deliberately not in menu order.
				"mutations": []any{
					choice("key-id", "ID", false),
					choice("key-in", "IN", true),
				},
			},
		},
	}

	countries, selected := parseChartCountries(root)
	if selected != "IN" {
		t.Fatalf("selected = %q, want IN", selected)
	}
	if len(countries) != 2 || countries[0] != (ChartCountry{Code: "IN", Name: "印度"}) || countries[1] != (ChartCountry{Code: "ID", Name: "印度尼西亚"}) {
		t.Fatalf("countries = %+v", countries)
	}
}

func TestAlbumArtistsFromHeader(t *testing.T) {
	tests := []struct {
		name string
		runs []any
		want string
	}{
		{
			name: "single artist",
			runs: []any{
				map[string]any{"text": "周传雄", "navigationEndpoint": map[string]any{"browseEndpoint": map[string]any{"browseId": "UCGh_v5aXDXCanMfU6O31JXw"}}},
			},
			want: "周传雄",
		},
		{
			name: "album label and artist",
			runs: []any{
				map[string]any{"text": "Album", "navigationEndpoint": map[string]any{"browseEndpoint": map[string]any{"browseId": "FEmusic_new_releases"}}},
				map[string]any{"text": " • "},
				map[string]any{"text": "周传雄", "navigationEndpoint": map[string]any{"browseEndpoint": map[string]any{"browseId": "UCGh_v5aXDXCanMfU6O31JXw"}}},
			},
			want: "周传雄",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := map[string]any{"straplineTextOne": map[string]any{"runs": tc.runs}}
			artists := albumArtistsFromHeader(header)
			if len(artists) != 1 || artists[0].Name != tc.want {
				t.Fatalf("artists = %+v, want %q", artists, tc.want)
			}
		})
	}
}

func TestParseResponsiveArtistCardWithPlayVideoID(t *testing.T) {
	r := map[string]any{
		"flexColumns": []any{
			map[string]any{"musicResponsiveListItemFlexColumnRenderer": map[string]any{"text": map[string]any{"runs": []any{map[string]any{"text": "Seyi Vibez"}}}}},
			map[string]any{"musicResponsiveListItemFlexColumnRenderer": map[string]any{"text": map[string]any{"runs": []any{map[string]any{"text": "456万位订阅者"}}}}},
		},
		"playlistItemData": map[string]any{"videoId": "top-song-video"},
		"navigationEndpoint": map[string]any{
			"browseEndpoint": map[string]any{
				"browseId": "UCartist",
				"browseEndpointContextSupportedConfigs": map[string]any{
					"browseEndpointContextMusicConfig": map[string]any{"pageType": "MUSIC_PAGE_TYPE_ARTIST"},
				},
			},
		},
	}
	item := parseResponsiveAsCard(r)
	if item.Type != "artist" || item.ID != "UCartist" || item.Track != nil {
		t.Fatalf("item = %+v, want artist UCartist without track", item)
	}
}

func TestParseAlbumUnavailableResponseReturnsError(t *testing.T) {
	for _, data := range []string{`{}`, `{"contents":{}}`, `{"contents":{"twoColumnBrowseResultsRenderer":{}}}`} {
		if _, err := parseAlbumResponse([]byte(data), "MPREb_missing"); err == nil {
			t.Fatalf("parseAlbumResponse(%s) returned no error", data)
		}
	}
}

func TestHasMusicVideo(t *testing.T) {
	nav := func(videoType string) map[string]any {
		return map[string]any{
			"watchEndpoint": map[string]any{
				"watchEndpointMusicSupportedConfigs": map[string]any{
					"watchEndpointMusicConfig": map[string]any{"musicVideoType": videoType},
				},
			},
		}
	}
	if !hasMusicVideo(nav("MUSIC_VIDEO_TYPE_OMV")) {
		t.Fatal("OMV should be a video")
	}
	if hasMusicVideo(nav("MUSIC_VIDEO_TYPE_ATV")) {
		t.Fatal("ATV should be audio only")
	}
	if hasMusicVideo(nil) {
		t.Fatal("nil navigation should not be a video")
	}
}
