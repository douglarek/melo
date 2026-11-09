package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	aacpcm "github.com/tphakala/go-aac/pcm"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestHLSSkipFrames(t *testing.T) {
	if got := hlsSkipFrames(5000, 2000, 44100); got != 132300 {
		t.Fatalf("skip = %d, want 132300", got)
	}
	if got := hlsSkipFrames(2000, 2000, 44100); got != 0 {
		t.Fatalf("skip at segment start = %d, want 0", got)
	}
	if got := hlsSkipFrames(1000, 2000, 44100); got != 0 {
		t.Fatalf("skip before segment start = %d, want 0", got)
	}
}

func TestOpenStreamingMP4CleansUpAfterHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "song.mp4")
	for _, resume := range []time.Duration{0, 3 * time.Second} {
		if _, err := openStreamingMP4(context.Background(), srv.URL, cache, 0, 0, resume); err == nil {
			t.Fatalf("resume %v: want an error", resume)
		}
		if _, err := os.Stat(cache + ".part"); !os.IsNotExist(err) {
			t.Fatalf("resume %v: part file left: %v", resume, err)
		}
	}
}

func TestOpenStreamingMP4CleansUpOnBadData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(make([]byte, 200*1024))
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "bad.mp4")
	if _, err := openStreamingMP4(context.Background(), srv.URL, cache, 0, 0, 0); err == nil {
		t.Fatal("want a decoder error")
	}
	if _, err := os.Stat(cache + ".part"); !os.IsNotExist(err) {
		t.Fatalf("part file left: %v", err)
	}
}

func encodeADTS(t *testing.T, seconds float64) []byte {
	t.Helper()

	const (
		sampleRate = 44100
		channels   = 2
	)
	samples := int(float64(sampleRate) * seconds)
	pcm := make([]byte, samples*channels*2)
	for i := range samples {
		sample := int16(12000)
		if i%2 == 1 {
			sample = -12000
		}
		binary.LittleEndian.PutUint16(pcm[i*channels*2:], uint16(sample))
	}

	var buf bytes.Buffer
	cfg := aacpcm.Config{
		SampleRate: sampleRate,
		BitDepth:   16,
		Channels:   channels,
		Bitrate:    128000,
	}
	if err := aacpcm.EncodeInterleaved(&buf, cfg, pcm); err != nil {
		t.Fatalf("encode ADTS fixture: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadReaderSeekEndUsesKnownLength(t *testing.T) {
	d := &download{total: 12345}
	d.cond.L = &d.mu
	r := &downloadReader{d: d}
	pos, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("seek end: %v", err)
	}
	if pos != 12345 {
		t.Fatalf("seek end = %d, want 12345", pos)
	}
}

func TestHLSCachedSeek(t *testing.T) {
	audio := encodeADTS(t, 1)
	cache := NewCache(t.TempDir(), 0)
	info := &ytmusic.StreamInfo{
		VideoID:    "cached",
		Format:     "hls",
		SampleRate: 44100,
		Channels:   2,
		DurationMs: 1000,
	}
	cache.Save(info.VideoID, info, audio)

	s, err := openHLSStream(t.Context(), info, cache)
	if err != nil {
		t.Fatalf("open cached HLS stream: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.cacheHit {
		t.Fatal("expected cache hit")
	}

	buf := make([]float32, 1024)
	if n, err := s.Read(buf); err != nil || n == 0 {
		t.Fatalf("initial read = %d, %v; want samples", n, err)
	}
	if err := s.SetPosition(22050); err != nil {
		t.Fatalf("seek cached stream: %v", err)
	}
	if n, err := s.Read(buf); err != nil || n == 0 {
		t.Fatalf("read after seek = %d, %v; want samples", n, err)
	}
	if err := s.SetPosition(0); err != nil {
		t.Fatalf("rewind cached stream: %v", err)
	}
	if n, err := s.Read(buf); err != nil || n == 0 {
		t.Fatalf("read after rewind = %d, %v; want samples", n, err)
	}
}

func TestHLSUncachedReadDoesNotDeadlock(t *testing.T) {
	const segmentCount = 8
	chunks := make([][]byte, segmentCount)
	for i := range chunks {
		chunks[i] = encodeADTS(t, 0.25)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/seg/")
		idx, err := strconv.Atoi(raw)
		if err != nil || idx < 0 || idx >= len(chunks) {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(chunks[idx])
	}))
	t.Cleanup(server.Close)

	info := &ytmusic.StreamInfo{
		VideoID:    "uncached",
		Format:     "hls",
		SampleRate: 44100,
		Channels:   2,
		DurationMs: segmentCount * 250,
	}
	for i := range segmentCount {
		info.Segments = append(info.Segments, ytmusic.StreamSegment{
			URI:        fmt.Sprintf("%s/seg/%d", server.URL, i),
			DurationMs: 250,
		})
	}

	s, err := openHLSStream(t.Context(), info, nil)
	if err != nil {
		t.Fatalf("open HLS stream: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	done := make(chan error, 1)
	go func() {
		buf := make([]float32, 4096)
		for range 100 {
			n, err := s.Read(buf)
			if errors.Is(err, io.EOF) {
				done <- nil
				return
			}
			if err != nil {
				done <- err
				return
			}
			if n == 0 {
				done <- nil
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read HLS stream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HLS read did not finish; likely deadlocked")
	}
}

func TestHLSCloseWakesSegmentWait(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	info := &ytmusic.StreamInfo{
		VideoID:    "slow",
		Format:     "hls",
		SampleRate: 44100,
		Channels:   2,
		DurationMs: 1000,
		Segments: []ytmusic.StreamSegment{{
			URI:        server.URL + "/seg/0",
			DurationMs: 1000,
		}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	openDone := make(chan error, 1)
	go func() {
		s, err := openHLSStream(ctx, info, nil)
		if s != nil {
			_ = s.Close()
		}
		openDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("segment request did not start")
	}
	cancel()

	select {
	case err := <-openDone:
		if err == nil {
			t.Fatal("expected HLS open to fail after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HLS open did not return after cancellation")
	}
}
