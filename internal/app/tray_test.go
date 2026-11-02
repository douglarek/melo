package app

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayIcons(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		width    int
		height   int
		template bool
	}{
		{"template", trayTemplate, 36, 36, true},
		{"color", trayColor, 64, 64, false},
	}

	for _, tt := range tests {
		img, err := png.Decode(bytes.NewReader(tt.data))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := img.Bounds().Dx(); got != tt.width {
			t.Fatalf("%s width = %d, want %d", tt.name, got, tt.width)
		}
		if got := img.Bounds().Dy(); got != tt.height {
			t.Fatalf("%s height = %d, want %d", tt.name, got, tt.height)
		}

		var opaque, transparent, black, colored int
		for y := 0; y < tt.height; y++ {
			for x := 0; x < tt.width; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a == 0 {
					transparent++
					continue
				}
				opaque++
				if r == 0 && g == 0 && b == 0 {
					black++
				}
				if r != g || g != b {
					colored++
				}
			}
		}
		if opaque == 0 {
			t.Fatalf("%s has no visible pixels", tt.name)
		}
		if tt.template {
			if transparent == 0 {
				t.Fatalf("%s has no transparent pixels", tt.name)
			}
			if black == 0 {
				t.Fatalf("%s has no black pixels", tt.name)
			}
		}
		if !tt.template && colored == 0 {
			t.Fatalf("%s has no colored pixels", tt.name)
		}
	}
}
