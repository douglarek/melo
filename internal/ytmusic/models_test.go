package ytmusic

import "testing"

func TestCleanArtistName(t *testing.T) {
	cases := map[string]string{
		"Nasboi • 422万次观看": "Nasboi",
		"周杰伦":              "周杰伦",
		"Artist • 3:45":    "Artist",
	}
	for in, want := range cases {
		if got := CleanArtistName(in); got != want {
			t.Errorf("CleanArtistName(%q) = %q, want %q", in, got, want)
		}
	}
	if !VideoByline("Nasboi • 422万次观看") {
		t.Error("view count byline was not detected as a video")
	}
	if VideoByline("Nasboi") {
		t.Error("plain artist name was taken for a video byline")
	}
}
