package player

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/douglarek/melo/internal/ytmusic"
)

func newTestPlayer() *Player {
	p := New(func(fn func()) { fn() })
	p.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		return nil, errors.New("test mock offline")
	}
	return p
}

func TestAttachFeedAppendsNearQueueEnd(t *testing.T) {
	p := newTestPlayer()
	t1 := ytmusic.Track{ID: "v1", Title: "Song One", DurationMs: 180000}
	t2 := ytmusic.Track{ID: "v2", Title: "Song Two", DurationMs: 240000}
	t3 := ytmusic.Track{ID: "v3", Title: "Song Three", DurationMs: 200000}
	t4 := ytmusic.Track{ID: "v4", Title: "Song Four", DurationMs: 210000}

	p.SetQueue([]ytmusic.Track{t1, t2, t3}, 2)
	done := make(chan struct{})
	p.AttachFeed(func(got func([]ytmusic.Track)) {
		got([]ytmusic.Track{t4})
		close(done)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("feed was not called near the queue end")
	}
	if got := len(p.Queue()); got != 4 {
		t.Fatalf("queue length = %d, want 4", got)
	}
}

func TestPlayerQueueAndControls(t *testing.T) {
	p := newTestPlayer()

	t1 := ytmusic.Track{ID: "v1", Title: "Song One", DurationMs: 180000}
	t2 := ytmusic.Track{ID: "v2", Title: "Song Two", DurationMs: 240000}
	t3 := ytmusic.Track{ID: "v3", Title: "Song Three", DurationMs: 200000}

	p.SetQueue([]ytmusic.Track{t1, t2, t3}, 0)
	if p.At() != 0 {
		t.Fatalf("expected at 0, got %d", p.At())
	}
	if p.Current() == nil || p.Current().ID != "v1" {
		t.Fatalf("expected current track v1, got %+v", p.Current())
	}

	// Next to 1
	p.Next()
	if p.At() != 1 {
		t.Fatalf("expected at 1, got %d", p.At())
	}
	// Next to 2
	p.Next()
	if p.At() != 2 {
		t.Fatalf("expected at 2, got %d", p.At())
	}
	// Prev back to 1
	p.Prev()
	if p.At() != 1 {
		t.Fatalf("expected at 1 after Prev, got %d", p.At())
	}

	// Volume
	p.SetVolume(0.8)
	if p.Volume() != 0.8 {
		t.Fatalf("expected volume 0.8, got %f", p.Volume())
	}

	// Repeat mode
	p.SetRepeat(RepeatAll)
	if p.Repeat() != RepeatAll {
		t.Fatalf("expected RepeatAll, got %v", p.Repeat())
	}
	p.ToggleRepeat()
	if p.Repeat() != RepeatOne {
		t.Fatalf("expected RepeatOne, got %v", p.Repeat())
	}

	// Session save and restore
	sess, ok := p.Session()
	if !ok || len(sess.Queue) != 3 {
		t.Fatalf("expected valid session, got %+v", sess)
	}

	p2 := newTestPlayer()
	p2.Restore(sess)
	if p2.At() != 1 || len(p2.Queue()) != 3 || p2.Volume() != 0.8 {
		t.Fatalf("restore mismatch: at=%d, len=%d, vol=%f", p2.At(), len(p2.Queue()), p2.Volume())
	}
}

func TestPlayerShuffle(t *testing.T) {
	p := newTestPlayer()
	var tracks []ytmusic.Track
	for i := 0; i < 20; i++ {
		tracks = append(tracks, ytmusic.Track{ID: "v", Title: "Track"})
	}
	p.SetQueue(tracks, 0)
	p.SetShuffle(true)
	if !p.Shuffle() {
		t.Fatal("expected shuffle true")
	}
	// Current track should stay at index 0
	if p.At() != 0 {
		t.Fatalf("expected at 0 after shuffle, got %d", p.At())
	}
	p.SetShuffle(false)
	if p.Shuffle() {
		t.Fatal("expected shuffle false")
	}
}

func TestPlayerDurationAndSeek(t *testing.T) {
	p := newTestPlayer()
	track := ytmusic.Track{ID: "v1", Title: "Song", DurationMs: 120000}
	p.SetQueue([]ytmusic.Track{track}, 0)

	if p.Duration() != 120*time.Second {
		t.Fatalf("expected 120s duration, got %v", p.Duration())
	}

	p.Seek(30 * time.Second)
	pos := p.Position()
	if pos < 29*time.Second || pos > 31*time.Second {
		t.Fatalf("expected ~30s position, got %v", pos)
	}
}

func TestCachePrune(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1024*1024)

	info := &ytmusic.StreamInfo{VideoID: "test1", SampleRate: 44100}
	c.Save("test1", info, []byte("fake aac audio data"))

	data, gotInfo, ok := c.Lookup("test1")
	if !ok || string(data) != "fake aac audio data" || gotInfo.VideoID != "test1" {
		t.Fatalf("cache lookup failed")
	}

	c.Clear()
	_, _, ok = c.Lookup("test1")
	if ok {
		t.Fatalf("expected cache empty after Clear")
	}
}

func TestPlayerLoadingState(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	p := newTestPlayer()
	p.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		close(started)
		<-release
		return nil, errors.New("test stop")
	}

	p.SetQueue([]ytmusic.Track{{ID: "v1", Title: "Song"}}, 0)
	<-started
	if !p.Loading() || !p.PendingPlay() || !p.Active() || p.Playing() {
		t.Fatalf("loading=%v pending=%v active=%v playing=%v", p.Loading(), p.PendingPlay(), p.Active(), p.Playing())
	}

	p.TogglePlay()
	if p.PendingPlay() || p.Active() {
		t.Fatal("pausing during loading should cancel pending playback")
	}
	if !p.Loading() {
		t.Fatal("pausing should not stop the stream load")
	}

	p.TogglePlay()
	if !p.PendingPlay() || !p.Active() {
		t.Fatal("playing again during loading should restore pending playback")
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for p.Loading() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Loading() {
		t.Fatal("load did not finish")
	}
	if p.PendingPlay() || p.Active() {
		t.Fatal("a failed load should clear pending playback")
	}
}

func TestAdvanceAfterEnd(t *testing.T) {
	t1 := ytmusic.Track{ID: "v1", Title: "Song One", DurationMs: 180000}
	t2 := ytmusic.Track{ID: "v2", Title: "Song Two", DurationMs: 240000}

	t.Run("next track", func(t *testing.T) {
		p := newTestPlayer()
		p.SetQueue([]ytmusic.Track{t1, t2}, 0)
		p.advanceAfterEnd()
		if p.Current() == nil || p.Current().ID != "v2" {
			t.Fatalf("current = %+v, want v2", p.Current())
		}
	})

	t.Run("repeat one", func(t *testing.T) {
		p := newTestPlayer()
		p.SetQueue([]ytmusic.Track{t1, t2}, 0)
		p.SetRepeat(RepeatOne)
		p.advanceAfterEnd()
		if p.Current() == nil || p.Current().ID != "v1" {
			t.Fatalf("current = %+v, want v1", p.Current())
		}
	})

	t.Run("repeat all wraps", func(t *testing.T) {
		p := newTestPlayer()
		p.SetQueue([]ytmusic.Track{t1, t2}, 1)
		p.SetRepeat(RepeatAll)
		p.advanceAfterEnd()
		if p.Current() == nil || p.Current().ID != "v1" {
			t.Fatalf("current = %+v, want v1", p.Current())
		}
	})

	t.Run("end of queue rewinds", func(t *testing.T) {
		p := newTestPlayer()
		p.SetQueue([]ytmusic.Track{t1}, 0)
		p.advanceAfterEnd()
		if p.Playing() {
			t.Fatal("expected playback to stop at the end of the queue")
		}
		if got := p.Position(); got != 0 {
			t.Fatalf("position = %v, want 0", got)
		}
	})
}

func TestPlayerSessionRestore(t *testing.T) {
	p := newTestPlayer()
	t1 := ytmusic.Track{ID: "v1", Title: "Song One", DurationMs: 180000}
	t2 := ytmusic.Track{ID: "v2", Title: "Song Two", DurationMs: 240000}
	p.SetQueue([]ytmusic.Track{t1, t2}, 1)
	p.Seek(42 * time.Second)
	p.SetVolume(0.8)
	p.SetShuffle(true)
	p.SetRepeat(RepeatAll)

	sess, ok := p.Session()
	if !ok {
		t.Fatal("expected a session")
	}
	if sess.At != 1 || sess.Position != 42*time.Second {
		t.Fatalf("session at=%d position=%v", sess.At, sess.Position)
	}

	p2 := newTestPlayer()
	p2.Restore(sess)
	if p2.Current() == nil || p2.Current().ID != "v2" {
		t.Fatalf("restored current = %+v, want v2", p2.Current())
	}
	if got := p2.Position(); got != 42*time.Second {
		t.Fatalf("restored position = %v, want 42s", got)
	}
	if p2.Volume() != 0.8 || !p2.Shuffle() || p2.Repeat() != RepeatAll {
		t.Fatalf("restored controls volume=%v shuffle=%v repeat=%v", p2.Volume(), p2.Shuffle(), p2.Repeat())
	}
	if len(p2.Queue()) != 2 {
		t.Fatalf("restored queue length = %d", len(p2.Queue()))
	}

	// Play retries loading a restored track and keeps its saved position
	// when the mock resolver fails.
	p2.Play()
	time.Sleep(20 * time.Millisecond)
	if got := p2.Position(); got != 42*time.Second {
		t.Fatalf("position after play attempt = %v, want 42s", got)
	}
}

func TestPlayerQueueViewAndPlayAt(t *testing.T) {
	p := newTestPlayer()
	t1 := ytmusic.Track{ID: "v1", Title: "Song One", DurationMs: 180000}
	t2 := ytmusic.Track{ID: "v2", Title: "Song Two", DurationMs: 240000}
	t3 := ytmusic.Track{ID: "v3", Title: "Song Three", DurationMs: 200000}
	p.SetQueue([]ytmusic.Track{t1, t2, t3}, 0)

	tracks, at := p.QueueView()
	if len(tracks) != 3 || at != 0 {
		t.Fatalf("QueueView = %d tracks at %d, want 3 at 0", len(tracks), at)
	}
	if tracks[0].ID != "v1" || tracks[2].ID != "v3" {
		t.Fatalf("unexpected queue order: %+v", tracks)
	}

	p.PlayAt(2)
	if p.Current() == nil || p.Current().ID != "v3" {
		t.Fatalf("expected v3 after PlayAt(2), got %+v", p.Current())
	}
	if _, at := p.QueueView(); at != 2 {
		t.Fatalf("QueueView position = %d, want 2", at)
	}
}

func TestPlayerFillsResolvedDuration(t *testing.T) {
	p := newTestPlayer()
	p.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		return &ytmusic.StreamInfo{VideoID: id, Format: "mp4", DirectURL: "http://127.0.0.1:1/none", DurationMs: 269000}, nil
	}
	p.SetQueue([]ytmusic.Track{{ID: "v1", Title: "Song"}}, 0)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Duration() == 269*time.Second {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected resolved duration on current track, got %v", p.Duration())
}

func TestPlayerReportsResolvedDuration(t *testing.T) {
	p := newTestPlayer()
	p.Resolve = func(ctx context.Context, id string) (*ytmusic.StreamInfo, error) {
		return &ytmusic.StreamInfo{VideoID: id, Format: "mp4", DirectURL: "http://127.0.0.1:1/none", DurationMs: 269000}, nil
	}
	got := make(chan int64, 1)
	p.DurationResolved = func(id string, durationMs int64) {
		got <- durationMs
	}

	p.SetQueue([]ytmusic.Track{{ID: "v1", Title: "Song"}}, 0)

	select {
	case ms := <-got:
		if ms != 269000 {
			t.Fatalf("resolved duration = %d, want 269000", ms)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("player did not report its resolved duration")
	}
}

func TestSetQueueCopiesTracks(t *testing.T) {
	p := newTestPlayer()
	tracks := []ytmusic.Track{{ID: "v1", Title: "Original", DurationMs: 180000}}
	p.SetQueue(tracks, 0)
	tracks[0].Title = "Changed"

	if got := p.Queue()[0].Title; got != "Original" {
		t.Fatalf("queue shares caller slice: title = %q", got)
	}
}
