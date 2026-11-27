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
	"sync/atomic"
	"testing"
	"time"

	aacpcm "github.com/tphakala/go-aac/pcm"
	m4a "github.com/tphakala/go-m4a"

	"github.com/douglarek/melo/internal/ytmusic"
)

func TestSegmentReaderWaitsForInFlightFetch(t *testing.T) {
	s := &hlsStream{
		ctx:         context.Background(),
		segments:    []ytmusic.StreamSegment{{URI: "http://example.invalid"}},
		segData:     make([][]byte, 1),
		segErr:      make([]error, 1),
		segDone:     []chan struct{}{make(chan struct{})},
		downloading: map[int]bool{0: true},
	}
	sr := &segmentReader{s: s, segIdx: 0}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 8)
		n, err := sr.Read(buf)
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		got <- string(buf[:n])
	}()

	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	s.segData[0] = []byte("abc")
	s.downloading[0] = false
	s.finishSegmentLocked(0)
	s.mu.Unlock()

	select {
	case text := <-got:
		if text != "abc" {
			t.Fatalf("read = %q, want abc", text)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not wake when the segment finished")
	}
}

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

func TestHLSBackwardSeekAfterEviction(t *testing.T) {
	const segmentCount = 10
	chunks := make([][]byte, segmentCount)
	var combined []byte
	for i := range chunks {
		chunks[i] = encodeADTS(t, 0.25)
		combined = append(combined, chunks[i]...)
	}

	reqCounts := make([]atomic.Int32, segmentCount)
	releaseTail := make(chan struct{})
	var releaseOnce atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/seg/")
		idx, err := strconv.Atoi(raw)
		if err != nil || idx < 0 || idx >= len(chunks) {
			http.NotFound(w, r)
			return
		}
		reqCounts[idx].Add(1)
		if idx == segmentCount-1 {
			<-releaseTail
		}
		_, _ = w.Write(chunks[idx])
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if releaseOnce.CompareAndSwap(false, true) {
			close(releaseTail)
		}
	})

	cache := NewCache(t.TempDir(), 10<<20)
	info := &ytmusic.StreamInfo{
		VideoID:    "hls-backseek",
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

	s, err := openHLSStream(t.Context(), info, cache)
	if err != nil {
		t.Fatalf("openHLSStream: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Consume past the prefetch window (hlsPrefetchWindow = 4) while the tail segment
	// keeps prefetchAll active, forcing eviction of early segments.
	buf := make([]float32, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := s.Read(buf)
		if err != nil {
			t.Fatalf("Read before seek: %v", err)
		}
		if n == 0 {
			t.Fatal("Read returned 0 samples before reaching target segment")
		}
		s.mu.Lock()
		curSeg := s.curSeg
		evicted0 := s.segData[0] == nil
		evicted1 := s.segData[1] == nil
		s.mu.Unlock()
		if curSeg >= hlsPrefetchWindow+2 && evicted0 && evicted1 {
			break
		}
	}

	s.mu.Lock()
	curSeg := s.curSeg
	evicted1 := s.segData[1] == nil
	s.mu.Unlock()
	if curSeg < hlsPrefetchWindow+2 || !evicted1 {
		t.Fatalf("expected consumption past window and eviction: curSeg=%d evicted1=%v", curSeg, evicted1)
	}

	// Seek back into already-evicted segment 1 (0.25s = 11025 frames at 44100 Hz).
	beforeReqs := reqCounts[1].Load()
	if err := s.SetPosition(11025); err != nil {
		t.Fatalf("backward SetPosition failed: %v", err)
	}
	if got := reqCounts[1].Load(); got <= beforeReqs {
		t.Fatalf("segment 1 was not re-fetched after eviction: requests before=%d after=%d", beforeReqs, got)
	}

	// Release the tail segment and verify decoding continues cleanly through EOF.
	if releaseOnce.CompareAndSwap(false, true) {
		close(releaseTail)
	}

	readDone := make(chan error, 1)
	var samplesAfterSeek int
	go func() {
		for {
			n, err := s.Read(buf)
			samplesAfterSeek += n
			if errors.Is(err, io.EOF) {
				readDone <- nil
				return
			}
			if err != nil {
				readDone <- err
				return
			}
		}
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("Read after backward seek: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HLS read after backward seek deadlocked")
	}
	if samplesAfterSeek == 0 {
		t.Fatal("expected decoded samples after backward seek")
	}

	// Early segments should remain evicted after re-reading, and cache publication
	// should contain every segment in exact order without gaps or duplicates.
	s.mu.Lock()
	stillEvicted := s.segData[0] == nil && s.segData[1] == nil
	s.mu.Unlock()
	if !stillEvicted {
		t.Fatal("expected early segments to stay evicted after re-reading")
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cached, _, ok := cache.Lookup(info.VideoID); ok {
			if !bytes.Equal(cached, combined) {
				t.Fatalf("cached HLS bytes mismatch: got %d bytes, want %d bytes", len(cached), len(combined))
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cached HLS audio was not published after backward seek")
}

func buildSyntheticDashMP4(t *testing.T, seconds float64, minSize int) []byte {
	t.Helper()

	adts := encodeADTS(t, seconds)
	var aus [][]byte
	for i := 0; i < len(adts); {
		if i+7 > len(adts) || adts[i] != 0xff || adts[i+1]&0xf6 != 0xf0 {
			t.Fatalf("invalid ADTS frame at %d", i)
		}
		frameLen := (int(adts[i+3]&0x03) << 11) | (int(adts[i+4]) << 3) | (int(adts[i+5]) >> 5)
		aus = append(aus, adts[i+7:i+frameLen])
		i += frameLen
	}

	// AAC-LC, 44100 Hz (index 4), 2 channels: (2<<11) | (4<<7) | (2<<3) = 0x1210.
	wcfg := m4a.WriterConfig{
		SampleRate: 44100,
		Channels:   2,
		ASC:        []byte{0x12, 0x10},
	}
	initSeg, err := m4a.InitSegment(wcfg)
	if err != nil {
		t.Fatalf("InitSegment: %v", err)
	}
	stream := bytes.Clone(initSeg)

	// Pad with a free box if requested so multi-chunk Range downloads (>256 KiB) are exercised.
	if minSize > len(stream)+8 {
		pad := make([]byte, minSize-len(stream))
		binary.BigEndian.PutUint32(pad[0:4], uint32(len(pad)))
		copy(pad[4:8], "free")
		stream = append(stream, pad...)
	}

	fw, err := m4a.NewFragmentWriter(wcfg)
	if err != nil {
		t.Fatalf("NewFragmentWriter: %v", err)
	}
	for i, au := range aus {
		if err := fw.WriteFrame(au); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		if (i+1)%10 == 0 {
			stream, err = fw.AppendSegment(stream)
			if err != nil {
				t.Fatalf("AppendSegment: %v", err)
			}
		}
	}
	if fw.PendingSamples() > 0 {
		stream, err = fw.AppendSegment(stream)
		if err != nil {
			t.Fatalf("AppendSegment tail: %v", err)
		}
	}
	return stream
}

func TestOpenStreamingMP4FragmentedDashRangeOnly(t *testing.T) {
	data := buildSyntheticDashMP4(t, 1.0, 270*1024)
	var rangeReqs atomic.Int32
	var fullGetReqs atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng == "" {
			fullGetReqs.Add(1)
			http.Error(w, "full GET forbidden", http.StatusForbidden)
			return
		}
		rangeReqs.Add(1)
		spec, ok := strings.CutPrefix(rng, "bytes=")
		if !ok {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		startStr, endStr, ok := strings.Cut(spec, "-")
		if !ok {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		start, err1 := strconv.ParseInt(startStr, 10, 64)
		end, err2 := strconv.ParseInt(endStr, 10, 64)
		if err1 != nil || err2 != nil || start < 0 || end >= int64(len(data)) || start > end {
			http.Error(w, "range out of bounds", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "dash.mp4")
	st, err := openStreamingMP4(t.Context(), srv.URL, cache, 1000, int64(len(data)), 0)
	if err != nil {
		t.Fatalf("openStreamingMP4: %v", err)
	}
	defer st.Close()

	if fullGetReqs.Load() != 0 {
		t.Fatalf("saw %d full-GET requests, want 0", fullGetReqs.Load())
	}
	if rangeReqs.Load() < 2 {
		t.Fatalf("range requests = %d, want >= 2", rangeReqs.Load())
	}
	if st.SampleRate() != 44100 || st.Channels() != 2 {
		t.Fatalf("format = %dHz %dch, want 44100Hz 2ch", st.SampleRate(), st.Channels())
	}

	buf := make([]float32, 4096)
	n, err := st.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read: %v", err)
	}
	if n == 0 {
		t.Fatal("Read returned 0 PCM samples")
	}
}
