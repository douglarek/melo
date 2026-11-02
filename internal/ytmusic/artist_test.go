package ytmusic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBrowseArtistHandlesMissingHeaderAndTitles(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"contents": {
					"singleColumnBrowseResultsRenderer": {
						"tabs": [{
							"tabRenderer": {
								"content": {
									"sectionListRenderer": {
										"contents": [
											{"musicShelfRenderer": {"contents": []}},
											{"musicCarouselShelfRenderer": {"contents": []}}
										]
									}
								}
							}
						}]
					}
				}
			}`)),
		}, nil
	})}

	art, err := c.BrowseArtist(context.Background(), "UCtest")
	if err != nil {
		t.Fatal(err)
	}
	if art.ID != "UCtest" || art.Name != "" || len(art.TopTracks) != 0 || len(art.Albums) != 0 || len(art.Singles) != 0 {
		t.Fatalf("artist = %+v", art)
	}
}
