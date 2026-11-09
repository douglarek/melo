package player

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mp3 "github.com/tphakala/go-mp3"

	"github.com/douglarek/melo/internal/ytmusic"
)

// silentHeadMP3 is a stereo 44.1 kHz stream, silent for its first second and
// a 440 Hz tone after, so a position read tells the truth.
func silentHeadMP3(t *testing.T) []byte {
	t.Helper()
	enc, err := mp3.NewEncoder(mp3.EncoderConfig{SampleRate: 44100, Channels: 2, Bitrate: 128000})
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	const seconds = 3
	var out []byte
	left := make([]float32, mp3.FrameSize)
	right := make([]float32, mp3.FrameSize)
	n := seconds * 44100
	for start := 0; start < n; start += mp3.FrameSize {
		frames := min(mp3.FrameSize, n-start)
		for i := 0; i < frames; i++ {
			at := start + i
			v := float32(0)
			if at >= 44100 {
				v = 0.5 * float32(math.Sin(2*math.Pi*440*float64(at)/44100))
			}
			left[i], right[i] = v, v
		}
		out, err = enc.EncodeFrame(out, [][]float32{left[:frames], right[:frames]})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	out, err = enc.EncodeFrame(out, nil)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	return out
}

func serveMP3(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		http.ServeContent(w, r, "episode.mp3", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// peak returns the loudest sample of frames frames, and how many it read.
func peak(t *testing.T, st interface {
	Read([]float32) (int, error)
	Channels() int
}, frames int) (float32, int) {
	t.Helper()
	buf := make([]float32, frames*st.Channels())
	read := 0
	for read < len(buf) {
		n, err := st.Read(buf[read:])
		read += n
		if err != nil {
			break
		}
	}
	p := float32(0)
	for _, v := range buf[:read] {
		p = max(p, float32(math.Abs(float64(v))))
	}
	return p, read / st.Channels()
}

// TestMP3StreamPlays checks that a stream decodes as it downloads, with the
// rate and duration its player reported.
func TestMP3StreamPlays(t *testing.T) {
	data := silentHeadMP3(t)
	srv := serveMP3(t, data)
	st, err := openMP3Stream(context.Background(), &ytmusic.StreamInfo{
		DirectURL:     srv.URL,
		Format:        "mp3",
		DurationMs:    3000,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("openMP3Stream: %v", err)
	}
	defer st.Close()

	if st.SampleRate() != 44100 || st.Channels() != 2 {
		t.Fatalf("%d Hz, %d channels", st.SampleRate(), st.Channels())
	}
	if got := st.Len(); got < 3*44100 || got > 3*44100+4*mp3.FrameSize {
		t.Fatalf("Len = %d, want about %d", got, 3*44100)
	}
	if p, _ := peak(t, st, 44100); p > 0.05 {
		t.Fatalf("peak of the silent second = %v", p)
	}
	if p, _ := peak(t, st, 44100); p < 0.2 {
		t.Fatalf("peak of the tone = %v", p)
	}
}

// TestMP3StreamSeeks checks that a seek reads from the byte its time maps
// to: the silent head is skipped, and seeking back plays it again.
func TestMP3StreamSeeks(t *testing.T) {
	data := silentHeadMP3(t)
	srv := serveMP3(t, data)
	st, err := openMP3Stream(context.Background(), &ytmusic.StreamInfo{
		DirectURL:     srv.URL,
		Format:        "mp3",
		DurationMs:    3000,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("openMP3Stream: %v", err)
	}
	defer st.Close()

	if err := st.SetPosition(44100 * 3 / 2); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if p, n := peak(t, st, 8820); n != 8820 || p < 0.2 {
		t.Fatalf("peak after seeking to 1.5s = %v over %d frames", p, n)
	}

	if err := st.SetPosition(8820); err != nil {
		t.Fatalf("SetPosition back: %v", err)
	}
	if p, _ := peak(t, st, 8820); p > 0.05 {
		t.Fatalf("peak after seeking back to 0.2s = %v", p)
	}
}
