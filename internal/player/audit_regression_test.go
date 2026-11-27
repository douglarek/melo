package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestAuditMP3StreamSeekFailsWhenServerIgnoresRangeAndPreservesStream(t *testing.T) {
	data := silentHeadMP3(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(data) // always 200 OK, ignores Range
	}))
	defer srv.Close()

	st, err := openMP3Stream(t.Context(), &ytmusic.StreamInfo{
		DirectURL:     srv.URL,
		Format:        "mp3",
		DurationMs:    3000,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("openMP3Stream: %v", err)
	}
	defer st.Close()

	before := st.currentFrame()
	if err := st.SetPosition(44100 * 3 / 2); err == nil {
		t.Fatal("SetPosition succeeded despite HTTP 200")
	}
	if st.currentFrame() != before {
		t.Fatalf("curFrame changed on failed seek: got %d, want %d", st.currentFrame(), before)
	}
	if st.Seekable() {
		t.Fatal("stream still reports Seekable() after HTTP 200")
	}
	// Reading after failed seek must not panic and must continue decoding.
	buf := make([]float32, 2048)
	n, err := st.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("Read after failed seek = %d, %v; want samples", n, err)
	}
}

func TestAuditMP3StreamSeekRejectsInvalidContentRange(t *testing.T) {
	data := silentHeadMP3(t)
	cases := []struct {
		name         string
		contentRange string
	}{
		{"missing", ""},
		{"garbage", "garbage"},
		{"non_numeric", "bytes nope-100/200"},
		{"wildcard_range", "bytes */200"},
		{"wrong_start", "bytes 0-100/999999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "audio/mpeg")
				if r.Header.Get("Range") == "" {
					_, _ = w.Write(data)
					return
				}
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data)
			}))
			defer srv.Close()

			st, err := openMP3Stream(t.Context(), &ytmusic.StreamInfo{
				DirectURL:     srv.URL,
				Format:        "mp3",
				DurationMs:    3000,
				ContentLength: int64(len(data)),
			})
			if err != nil {
				t.Fatalf("openMP3Stream: %v", err)
			}
			defer st.Close()

			if err := st.SetPosition(44100 * 3 / 2); err == nil {
				t.Fatalf("SetPosition accepted Content-Range %q", tc.contentRange)
			}
			// Preserved stream stays readable from start (silent first second).
			if p, n := peak(t, st, 8820); n != 8820 || p > 0.05 {
				t.Fatalf("peak after rejected seek = %v (%d frames), want silent head preserved", p, n)
			}
		})
	}
}

func TestAuditMP3StreamUnknownDurationReportsMinusOne(t *testing.T) {
	data := silentHeadMP3(t)
	srv := serveMP3(t, data)
	st, err := openMP3Stream(t.Context(), &ytmusic.StreamInfo{
		DirectURL: srv.URL,
		Format:    "mp3",
	})
	if err != nil {
		t.Fatalf("openMP3Stream: %v", err)
	}
	defer st.Close()

	if got := st.Len(); got != -1 {
		t.Fatalf("Len() = %d, want -1 for unknown duration", got)
	}
}

func TestAuditPlayerNonSeekableMP3ContinuesPlaybackOnSeek(t *testing.T) {
	data := silentHeadMP3(t)
	rangeSeen := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		if r.Header.Get("Range") != "" {
			select {
			case rangeSeen <- struct{}{}:
			default:
			}
		}
		_, _ = w.Write(data) // always 200 OK
	}))
	defer srv.Close()

	// 1. Direct seekGuardDecoder check: rejecting Range does not return an error to audio.Player.
	st, err := openMP3Stream(t.Context(), &ytmusic.StreamInfo{
		DirectURL:     srv.URL,
		Format:        "mp3",
		DurationMs:    3000,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("openMP3Stream: %v", err)
	}
	var rejected atomic.Bool
	guard := newSeekGuardDecoder(st, func(frame int64) {
		rejected.Store(true)
	})
	if err := guard.SetPosition(44100 * 3 / 2); err != nil {
		_ = st.Close()
		t.Fatalf("seekGuardDecoder.SetPosition returned error to audio.Player: %v", err)
	}
	if !rejected.Load() || st.Seekable() {
		_ = st.Close()
		t.Fatalf("expected seek rejection to disable seeking: rejected=%v seekable=%v", rejected.Load(), st.Seekable())
	}
	buf := make([]float32, 2048)
	if n, err := guard.Read(buf); err != nil || n == 0 {
		_ = st.Close()
		t.Fatalf("guard.Read after rejected seek = %d, %v", n, err)
	}
	_ = st.Close()

	// 2. Full Player integration: Seek on non-Range MP3 keeps track active and disables CanSeek.
	p := newTestPlayer(t)
	defer p.Close()
	p.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
		return &ytmusic.StreamInfo{
			VideoID:       track.ID,
			DirectURL:     srv.URL,
			Format:        "mp3",
			ContentLength: int64(len(data)),
		}, nil
	}
	p.SetQueue([]ytmusic.Track{{ID: "ep-1", Title: "Podcast", DurationMs: 3000, Podcast: true}}, 0)

	deadline := time.Now().Add(3 * time.Second)
	for p.Loading() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Loading() || p.Duration() != 3*time.Second {
		t.Fatalf("loading=%v duration=%v, want false / 3s", p.Loading(), p.Duration())
	}
	p.Seek(1500 * time.Millisecond)
	select {
	case <-rangeSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("seek never hit MP3 server")
	}
	deadline = time.Now().Add(2 * time.Second)
	for p.CanSeek() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.CanSeek() {
		t.Fatal("Player.CanSeek remained true after HTTP 200 seek")
	}
	if cur := p.Current(); cur == nil || cur.ID != "ep-1" {
		t.Fatalf("track ended/changed after rejected seek: %+v", cur)
	}

	// 3. Accept-Ranges: none disables CanSeek immediately before any seek attempt.
	srvNone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Accept-Ranges", "none")
		_, _ = w.Write(data)
	}))
	defer srvNone.Close()

	p2 := newTestPlayer(t)
	defer p2.Close()
	p2.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
		return &ytmusic.StreamInfo{
			VideoID:       track.ID,
			DirectURL:     srvNone.URL,
			Format:        "mp3",
			DurationMs:    3000,
			ContentLength: int64(len(data)),
		}, nil
	}
	p2.SetQueue([]ytmusic.Track{{ID: "ep-2", Title: "Podcast 2", DurationMs: 3000}}, 0)
	deadline = time.Now().Add(3 * time.Second)
	for p2.Loading() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p2.CanSeek() {
		t.Fatal("Accept-Ranges: none did not disable CanSeek")
	}
}

type playerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f playerRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type playerPOTFunc func(context.Context, string) (string, error)

func (f playerPOTFunc) Token(ctx context.Context, binding string) (string, error) {
	return f(ctx, binding)
}

func testUMPVarint(v int) []byte {
	if v < 128 {
		return []byte{byte(v)}
	}
	return []byte{byte(0x80 | (v & 0x3f)), byte(v >> 6)}
}

func testUMPPart(typ int, payload []byte) []byte {
	var out bytes.Buffer
	out.Write(testUMPVarint(typ))
	out.Write(testUMPVarint(len(payload)))
	out.Write(payload)
	return out.Bytes()
}

func testProtoUint(field uint64, val uint64) []byte {
	var b []byte
	b = binary.AppendUvarint(b, field<<3)
	b = binary.AppendUvarint(b, val)
	return b
}

func TestAuditPlayerSabrWaitFailureClosesSession(t *testing.T) {
	dir := t.TempDir()
	var sabrCalls atomic.Int32
	c := ytmusic.NewClient()
	c.SetSabr(playerPOTFunc(func(context.Context, string) (string, error) {
		return "dGVzdC1wb3Q=", nil
	}), dir)
	restore := c.SetTestTransport(playerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "next"), strings.Contains(req.URL.Path, "visitor"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"responseContext":{"visitorData":"vis-1"}}`)),
				Request:    req,
			}, nil
		case strings.Contains(req.URL.Path, "player"):
			body := `{
				"playabilityStatus":{"status":"OK"},
				"playerConfig":{"mediaCommonConfig":{"mediaUstreamerRequestConfig":{"videoPlaybackUstreamerConfig":"cfg"}}},
				"streamingData":{
					"serverAbrStreamingUrl":"https://sabr.invalid/stream",
					"adaptiveFormats":[{
						"itag":140,
						"mimeType":"audio/mp4; codecs=\"mp4a.40.2\"",
						"contentLength":"128",
						"approxDurationMs":"3000",
						"audioSampleRate":"44100"
					}]
				}
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		case strings.Contains(req.URL.Host, "sabr.invalid"):
			if sabrCalls.Add(1) > 1 {
				return nil, errors.New("sabr stream aborted")
			}
			// Deliver an unparseable 32-byte "init" segment (of 128 total) so
			// startSabrSession becomes ready, openSabrPure fails, Wait runs and
			// fails on the aborted second request, and Player must close SabrSession & clean .tmp.
			var hdr bytes.Buffer
			hdr.Write(testProtoUint(1, 1))   // headerID = 1
			hdr.Write(testProtoUint(6, 0))   // offset = 0
			hdr.Write(testProtoUint(8, 1))   // isInitSeg = true
			hdr.Write(testProtoUint(14, 32)) // contentLength = 32
			var fmtInit bytes.Buffer
			fmtInit.Write(testProtoUint(4, 2)) // endSeg = 2
			var media bytes.Buffer
			media.Write(testUMPVarint(1))
			media.Write(make([]byte, 32))
			var body bytes.Buffer
			body.Write(testUMPPart(42, fmtInit.Bytes()))
			body.Write(testUMPPart(20, hdr.Bytes()))
			body.Write(testUMPPart(21, media.Bytes()))
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}
	}))
	defer restore()

	p := newTestPlayer(t)
	defer p.Close()
	errCh := make(chan error, 4)
	p.Error = func(_ string, err error) {
		errCh <- err
	}
	resolved := make(chan struct{}, 1)
	p.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
		info, err := c.ResolveStream(ctx, track)
		select {
		case resolved <- struct{}{}:
		default:
		}
		return info, err
	}

	p.SetQueue([]ytmusic.Track{{ID: "sabr-wait-fail", Title: "Bad Sabr", DurationMs: 3000}}, 0)

	select {
	case <-resolved:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Resolve")
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil playback error after SABR Wait failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Player playback error after SABR Wait failure")
	}

	// SabrSession must have been closed and its temp file removed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries, _ := os.ReadDir(dir)
	t.Fatalf("SabrSession leaked files after Wait failure: %v", entries)
}

func TestAuditCacheProtectsActiveKeepTempAndMigratesSabrDir(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1) // 1 byte limit forces aggressive pruning

	activeTemp := filepath.Join(c.SabrDir(), "playing.m4a.12345.tmp")
	staleTemp := filepath.Join(c.SabrDir(), "abandoned.m4a.12345.tmp")
	writeCacheTestFile(t, activeTemp, 64, -15*time.Minute)
	writeCacheTestFile(t, staleTemp, 64, -15*time.Minute)

	c.SetKeep("playing")
	c.Prune()

	if _, err := os.Stat(activeTemp); err != nil {
		t.Fatalf("active kept temp file was pruned: %v", err)
	}
	if _, err := os.Stat(staleTemp); !os.IsNotExist(err) {
		t.Fatal("abandoned SABR temp file was not pruned")
	}

	// Migrate should clean >24h SABR temp files while still respecting SetKeep.
	veryOldAbandoned := filepath.Join(c.SabrDir(), "old-abandoned.m4a.999.tmp")
	veryOldActive := filepath.Join(c.SabrDir(), "playing.m4a.999.tmp")
	writeCacheTestFile(t, veryOldAbandoned, 32, -48*time.Hour)
	writeCacheTestFile(t, veryOldActive, 32, -48*time.Hour)

	freed := c.Migrate()
	if freed != 32 {
		t.Fatalf("Migrate freed %d, want 32", freed)
	}
	if _, err := os.Stat(veryOldAbandoned); !os.IsNotExist(err) {
		t.Fatal("Migrate left old abandoned SABR temp file")
	}
	if _, err := os.Stat(veryOldActive); err != nil {
		t.Fatalf("Migrate removed kept SABR temp file: %v", err)
	}
}

func TestAuditReadBoxHeaderRejectsNegativeAndOverflowingSizes(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
		pos  int64
		tot  int64
	}{
		{
			name: "32-bit size smaller than header",
			buf:  []byte{0, 0, 0, 4, 'm', 'o', 'o', 'f'},
			pos:  0,
			tot:  8,
		},
		{
			name: "64-bit size smaller than 16-byte header",
			buf:  []byte{0, 0, 0, 1, 'm', 'o', 'o', 'f', 0, 0, 0, 0, 0, 0, 0, 12},
			pos:  0,
			tot:  16,
		},
		{
			name: "64-bit size overflows pos+size",
			buf: func() []byte {
				b := make([]byte, 32)
				binary.BigEndian.PutUint32(b[16:], 1)
				copy(b[20:24], "moof")
				binary.BigEndian.PutUint64(b[24:32], math.MaxInt64)
				return b
			}(),
			pos: 16,
			tot: 32,
		},
		{
			name: "64-bit size overflows int64",
			buf: func() []byte {
				b := make([]byte, 16)
				binary.BigEndian.PutUint32(b[0:], 1)
				copy(b[4:8], "moof")
				binary.BigEndian.PutUint64(b[8:16], math.MaxInt64+1)
				return b
			}(),
			pos: 0,
			tot: 16,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fmp4Reader{src: bytes.NewReader(tc.buf), total: tc.tot}
			if _, _, _, err := r.readBoxHeader(tc.pos); !errors.Is(err, errFMP4) {
				t.Fatalf("readBoxHeader err = %v, want errFMP4", err)
			}
		})
	}
}

func TestAuditFMP4RejectsOutOfBoundsAndOversizedSamples(t *testing.T) {
	tfhd := fullBox("tfhd", 0, []byte{0, 0, 0, 1})
	tfdt := fullBox("tfdt", 0, []byte{0, 0, 0, 0})

	// Sample size 2 MiB (> maxFMP4SampleSize).
	trunBig := make([]byte, 16)
	binary.BigEndian.PutUint32(trunBig[0:], 1)
	binary.BigEndian.PutUint32(trunBig[4:], 0)
	binary.BigEndian.PutUint32(trunBig[8:], 2<<20)
	moofBig := box("moof", box("traf", append(append(tfhd, tfdt...), fullBox("trun", 0x201, trunBig)...)))
	mdat := box("mdat", []byte{1, 2, 3, 4})
	dataBig := append(moofBig, mdat...)
	rBig := &fmp4Reader{
		src:   bytes.NewReader(dataBig),
		total: int64(len(dataBig)),
		init:  &fmp4Init{timescale: 44100},
	}
	if err := rBig.fill(); !errors.Is(err, errFMP4) {
		t.Fatalf("oversized sample err = %v, want errFMP4", err)
	}

	// Sample pointing past mdatEnd.
	trunOOB := make([]byte, 16)
	binary.BigEndian.PutUint32(trunOOB[0:], 1)
	binary.BigEndian.PutUint32(trunOOB[4:], 0) // data_offset relative to moof (outside mdat payload)
	binary.BigEndian.PutUint32(trunOOB[8:], 4)
	moofOOB := box("moof", box("traf", append(append(tfhd, tfdt...), fullBox("trun", 0x201, trunOOB)...)))
	dataOOB := append(moofOOB, mdat...)
	rOOB := &fmp4Reader{
		src:   bytes.NewReader(dataOOB),
		total: int64(len(dataOOB)),
		init:  &fmp4Init{timescale: 44100},
	}
	if err := rOOB.fill(); !errors.Is(err, errFMP4) {
		t.Fatalf("out-of-bounds sample err = %v, want errFMP4", err)
	}
}

func TestAuditInFlightFeedDoesNotBlockNewQueueFeed(t *testing.T) {
	p := newTestPlayer(t)
	defer p.Close()

	feedAStarted := make(chan struct{})
	releaseFeedA := make(chan struct{})
	feedBCalled := make(chan struct{})

	p.SetQueue([]ytmusic.Track{{ID: "a1", Title: "A1"}}, 0)
	p.AttachFeed(func(got func([]ytmusic.Track)) {
		close(feedAStarted)
		<-releaseFeedA
		got([]ytmusic.Track{{ID: "a-stale", Title: "A Stale"}})
	})

	select {
	case <-feedAStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("feed A did not start")
	}

	p.SetQueue([]ytmusic.Track{{ID: "b1", Title: "B1"}}, 0)
	p.AttachFeed(func(got func([]ytmusic.Track)) {
		got([]ytmusic.Track{{ID: "b2", Title: "B2"}})
		close(feedBCalled)
	})

	select {
	case <-feedBCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight feed A blocked new queue B feed")
	}

	close(releaseFeedA)
	time.Sleep(20 * time.Millisecond)

	q := p.Queue()
	if len(q) != 2 || q[0].ID != "b1" || q[1].ID != "b2" {
		t.Fatalf("queue = %+v, want [b1 b2] without stale a-stale", q)
	}
}

func TestAuditPlayerRejectedMP3SeekResyncsPositionAndKeepsPlaying(t *testing.T) {
	data := silentHeadMP3(t)

	for _, tc := range []struct {
		name    string
		handler func(rangeSeen chan struct{}) http.HandlerFunc
	}{
		{
			name: "HTTP 200 ignores Range",
			handler: func(rangeSeen chan struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "audio/mpeg")
					if r.Header.Get("Range") != "" {
						select {
						case rangeSeen <- struct{}{}:
						default:
						}
					}
					_, _ = w.Write(data)
				}
			},
		},
		{
			name: "invalid Content-Range",
			handler: func(rangeSeen chan struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "audio/mpeg")
					if r.Header.Get("Range") == "" {
						_, _ = w.Write(data)
						return
					}
					select {
					case rangeSeen <- struct{}{}:
					default:
					}
					w.Header().Set("Content-Range", "bytes */200")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rangeSeen := make(chan struct{}, 4)
			srv := httptest.NewServer(tc.handler(rangeSeen))
			defer srv.Close()

			p := newTestPlayer(t)
			defer p.Close()
			p.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
				return &ytmusic.StreamInfo{
					VideoID:       track.ID,
					DirectURL:     srv.URL,
					Format:        "mp3",
					DurationMs:    3000,
					ContentLength: int64(len(data)),
				}, nil
			}
			p.SetQueue([]ytmusic.Track{{ID: "ep-seek", Title: "Podcast", DurationMs: 3000, Podcast: true}}, 0)

			deadline := time.Now().Add(3 * time.Second)
			for p.Loading() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if p.Loading() || !p.Playing() {
				t.Fatalf("loading=%v playing=%v, want false/true", p.Loading(), p.Playing())
			}
			time.Sleep(30 * time.Millisecond)

			target := 2500 * time.Millisecond
			p.Seek(target)

			select {
			case <-rangeSeen:
			case <-time.After(2 * time.Second):
				t.Fatal("seek never hit MP3 server")
			}

			deadline = time.Now().Add(2 * time.Second)
			for (p.CanSeek() || p.Position() >= 1800*time.Millisecond) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if p.CanSeek() {
				t.Fatal("CanSeek remained true after rejected seek")
			}
			if !p.Playing() {
				t.Fatal("player stopped playing after rejected seek")
			}
			if cur := p.Current(); cur == nil || cur.ID != "ep-seek" {
				t.Fatalf("current track = %+v, want ep-seek", cur)
			}
			pos1 := p.Position()
			if pos1 >= 1800*time.Millisecond {
				t.Fatalf("position stuck near requested target %v: got %v", target, pos1)
			}
			time.Sleep(80 * time.Millisecond)
			if !p.Playing() {
				t.Fatal("player stopped playing shortly after resync")
			}
			pos2 := p.Position()
			if pos2 < pos1 {
				t.Fatalf("position moved backwards after resync: %v -> %v", pos1, pos2)
			}
		})
	}

	t.Run("pending resumeAt rejected before initial play", func(t *testing.T) {
		rangeSeen := make(chan struct{}, 4)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/mpeg")
			if r.Header.Get("Range") != "" {
				select {
				case rangeSeen <- struct{}{}:
				default:
				}
			}
			_, _ = w.Write(data)
		}))
		defer srv.Close()

		started := make(chan struct{})
		release := make(chan struct{})
		p := newTestPlayer(t)
		defer p.Close()
		p.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
			close(started)
			<-release
			return &ytmusic.StreamInfo{
				VideoID:       track.ID,
				DirectURL:     srv.URL,
				Format:        "mp3",
				DurationMs:    3000,
				ContentLength: int64(len(data)),
			}, nil
		}
		p.SetQueue([]ytmusic.Track{{ID: "ep-resume", Title: "Podcast", DurationMs: 3000, Podcast: true}}, 0)
		<-started
		p.Seek(2500 * time.Millisecond)
		close(release)

		select {
		case <-rangeSeen:
		case <-time.After(3 * time.Second):
			t.Fatal("resumeAt seek never hit MP3 server")
		}
		deadline := time.Now().Add(2 * time.Second)
		for (p.Loading() || p.CanSeek() || p.Position() >= 1800*time.Millisecond) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if p.CanSeek() {
			t.Fatal("CanSeek remained true after rejected resumeAt seek")
		}
		if pos := p.Position(); pos >= 1800*time.Millisecond {
			t.Fatalf("position remained near rejected resumeAt target: got %v", pos)
		}
		if !p.Playing() {
			t.Fatal("player not playing after rejected resumeAt seek")
		}
	})

	t.Run("range-capable server still seeks", func(t *testing.T) {
		srv := serveMP3(t, data)
		p := newTestPlayer(t)
		defer p.Close()
		p.Resolve = func(ctx context.Context, track ytmusic.Track) (*ytmusic.StreamInfo, error) {
			return &ytmusic.StreamInfo{
				VideoID:       track.ID,
				DirectURL:     srv.URL,
				Format:        "mp3",
				DurationMs:    3000,
				ContentLength: int64(len(data)),
			}, nil
		}
		p.SetQueue([]ytmusic.Track{{ID: "ep-ok", Title: "Seekable", DurationMs: 3000, Podcast: true}}, 0)

		deadline := time.Now().Add(3 * time.Second)
		for p.Loading() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		p.Seek(2000 * time.Millisecond)
		if !p.CanSeek() {
			t.Fatal("CanSeek became false on range-capable server")
		}
		if pos := p.Position(); pos < 1900*time.Millisecond || pos > 2500*time.Millisecond {
			t.Fatalf("position on range-capable server = %v, want ~2s", pos)
		}
	})
}
