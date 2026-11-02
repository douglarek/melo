package ytmusic

import (
	"testing"
)

func TestPodcastSearchTypeParams(t *testing.T) {
	for _, kind := range []SearchType{SearchPodcasts, SearchPodcastEpisodes} {
		if kind.Param() == "" {
			t.Fatalf("%s param is empty", kind)
		}
		if kind.String() == "" || kind.String() == "全部" {
			t.Fatalf("%s string = %q", kind, kind.String())
		}
	}
}

func TestParsePodcastShowSearchResult(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"tabbedSearchResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"itemSectionRenderer": {
										"contents": [{
											"musicResponsiveListItemRenderer": {
												"flexColumns": [
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "测试播客"}]}}},
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "播客 • 测试主播"}]}}}
												],
												"navigationEndpoint": {
													"browseEndpoint": {
														"browseId": "MPSPPLtest",
														"browseEndpointContextSupportedConfigs": {
															"browseEndpointContextMusicConfig": {
																"pageType": "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE"
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

	res, err := parseSearchResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Podcasts) != 1 {
		t.Fatalf("podcasts = %+v", res.Podcasts)
	}
	pod := res.Podcasts[0]
	if pod.ID != "MPSPPLtest" || pod.Title != "测试播客" || pod.Type != "podcast" {
		t.Fatalf("podcast = %+v", pod)
	}
}

func TestParsePodcastEpisodeSearchResult(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"tabbedSearchResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"itemSectionRenderer": {
										"contents": [{
											"musicResponsiveListItemRenderer": {
												"playlistItemData": {"videoId": "episode1"},
												"flexColumns": [
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "测试分集"}]}}},
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "播客 • 测试主播"}]}}}
												],
												"overlay": {
													"musicItemThumbnailOverlayRenderer": {
														"content": {
															"musicPlayButtonRenderer": {
																"playNavigationEndpoint": {
																	"watchEndpoint": {
																		"videoId": "episode1",
																		"watchEndpointMusicSupportedConfigs": {
																			"watchEndpointMusicConfig": {
																				"musicVideoType": "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
																			}
																		}
																	}
																}
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

	res, err := parseSearchResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 {
		t.Fatalf("tracks = %+v", res.Tracks)
	}
	if res.Tracks[0].ID != "episode1" || !res.Tracks[0].Video {
		t.Fatalf("episode = %+v", res.Tracks[0])
	}
}

func TestParsePodcastTopResult(t *testing.T) {
	raw := []byte(`{
		"contents": {
			"tabbedSearchResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"musicCardShelfRenderer": {
										"title": {"runs": [{
											"text": "测试播客",
											"navigationEndpoint": {
												"browseEndpoint": {
													"browseId": "MPSPPLtest",
													"browseEndpointContextSupportedConfigs": {
														"browseEndpointContextMusicConfig": {
															"pageType": "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE"
														}
													}
												}
											}
										}]},
										"subtitle": {"runs": [{"text": "测试主播"}]}
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	res, err := parseSearchResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Podcasts) != 1 || res.Podcasts[0].ID != "MPSPPLtest" {
		t.Fatalf("podcasts = %+v", res.Podcasts)
	}
}
