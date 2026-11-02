package player

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
	aacpcm "github.com/tphakala/go-aac/pcm"

	"github.com/douglarek/melo/internal/ytmusic"
)

// RepeatMode sets queue repetition behavior.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// Session saves the player's queue and state across application launches.
type Session struct {
	Queue    []ytmusic.Track `json:"queue"`
	At       int             `json:"at"`
	Position time.Duration   `json:"position"`
	Shuffle  bool            `json:"shuffle"`
	Repeat   RepeatMode      `json:"repeat"`
	Volume   float64         `json:"volume"`
	Radio    string          `json:"radio,omitempty"`
}

// Player drives music queueing and playback.
type Player struct {
	mu      sync.Mutex
	post    func(func())
	Changed func()
	// DurationResolved reports a duration learned from the resolved stream
	// when the queue entry did not carry one.
	DurationResolved func(videoID string, durationMs int64)
	Cache            *Cache
	Resolve          func(ctx context.Context, id string) (*ytmusic.StreamInfo, error)

	queue   []ytmusic.Track
	order   []int
	at      int
	shuffle bool
	repeat  RepeatMode
	volume  float64

	// Radio feed
	radio    string
	feed     func(got func([]ytmusic.Track))
	feeding  bool
	radioGen int

	track   *ytmusic.Track
	out     *audio.Player
	stream  stream
	info    *ytmusic.StreamInfo
	fade    fader
	loading bool

	gen        int
	cancelLoad context.CancelFunc

	// Position tracking
	playStart   time.Time
	pauseOffset time.Duration
	resumeAt    time.Duration
	resumeID    string
	pendingPlay bool
	playing     bool
}

// New returns a newly initialized Player.
func New(post func(func())) *Player {
	p := &Player{
		post:    post,
		volume:  1.0,
		at:      -1,
		shuffle: false,
		repeat:  RepeatOff,
	}
	p.fade.setGain(p.volume)
	return p
}

func (p *Player) changed() {
	if p.Changed != nil {
		p.Changed()
	}
}

// Volume returns current output volume [0.0, 1.0].
func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// SetVolume sets the output volume level.
func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	p.volume = max(0, min(1, v))
	p.fade.setGain(p.volume)
	p.mu.Unlock()
	p.changed()
}

// Playing reports whether a track is actively playing.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Loading reports whether a track stream is currently loading.
func (p *Player) Loading() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loading
}

// Current returns the track currently playing or paused.
func (p *Player) Current() *ytmusic.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.track
}

// Queue returns a copy of the current queue.
func (p *Player) Queue() []ytmusic.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := make([]ytmusic.Track, len(p.queue))
	copy(res, p.queue)
	return res
}

// QueueView returns the queue in playback order and the position of the
// current track in that order.
func (p *Player) QueueView() ([]ytmusic.Track, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tracks := make([]ytmusic.Track, 0, len(p.order))
	for _, idx := range p.order {
		if idx >= 0 && idx < len(p.queue) {
			tracks = append(tracks, p.queue[idx])
		}
	}
	return tracks, p.at
}

// PlayAt jumps to the track at index in the playback order.
func (p *Player) PlayAt(index int) {
	p.mu.Lock()
	if index < 0 || index >= len(p.order) {
		p.mu.Unlock()
		return
	}
	p.at = index
	p.mu.Unlock()
	p.playCurrent()
}

// At returns the current track index in queue.
func (p *Player) At() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.at < 0 || p.at >= len(p.order) {
		return -1
	}
	return p.order[p.at]
}

// Shuffle reports whether shuffle mode is enabled.
func (p *Player) Shuffle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shuffle
}

// SetShuffle enables or disables queue shuffling.
func (p *Player) SetShuffle(s bool) {
	p.mu.Lock()
	if p.shuffle == s {
		p.mu.Unlock()
		return
	}
	p.shuffle = s
	p.reorder()
	p.mu.Unlock()
	p.changed()
}

// Repeat returns the active repeat mode.
func (p *Player) Repeat() RepeatMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.repeat
}

// SetRepeat changes repeat mode.
func (p *Player) SetRepeat(r RepeatMode) {
	p.mu.Lock()
	p.repeat = r
	p.mu.Unlock()
	p.changed()
}

// ToggleRepeat cycles through RepeatOff, RepeatAll, RepeatOne.
func (p *Player) ToggleRepeat() {
	p.mu.Lock()
	p.repeat = (p.repeat + 1) % 3
	p.mu.Unlock()
	p.changed()
}

func (p *Player) reorder() {
	n := len(p.queue)
	p.order = make([]int, n)
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle && n > 1 {
		// Keep current track at front if playing
		curIdx := -1
		if p.at >= 0 && p.at < len(p.order) {
			curIdx = p.order[p.at]
		}
		rand.Shuffle(n, func(i, j int) {
			p.order[i], p.order[j] = p.order[j], p.order[i]
		})
		if curIdx >= 0 {
			for i, idx := range p.order {
				if idx == curIdx {
					p.order[0], p.order[i] = p.order[i], p.order[0]
					p.at = 0
					break
				}
			}
		}
	}
}

// SetQueue sets a new queue of tracks and immediately starts playback.
func (p *Player) SetQueue(tracks []ytmusic.Track, at int) {
	p.mu.Lock()
	p.queue = append([]ytmusic.Track(nil), tracks...)
	p.reorder()
	p.feed = nil
	p.radio = ""
	p.radioGen++
	if at >= 0 && at < len(tracks) {
		for i, idx := range p.order {
			if idx == at {
				p.at = i
				break
			}
		}
	} else {
		p.at = 0
	}
	p.mu.Unlock()
	p.playCurrent()
}

// PlayTrack sets a single track as queue and plays it.
func (p *Player) PlayTrack(track ytmusic.Track) {
	p.SetQueue([]ytmusic.Track{track}, 0)
}

// Append adds tracks to the end of the queue.
func (p *Player) Append(tracks ...ytmusic.Track) {
	p.mu.Lock()
	start := len(p.queue)
	p.queue = append(p.queue, tracks...)
	for i := range tracks {
		p.order = append(p.order, start+i)
	}
	p.mu.Unlock()
	p.changed()
}

// InsertNext inserts tracks right after the current track.
func (p *Player) InsertNext(tracks ...ytmusic.Track) {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		p.SetQueue(tracks, 0)
		return
	}
	p.queue = append(p.queue, tracks...)
	p.reorder()
	p.mu.Unlock()
	p.changed()
}

func (p *Player) stop() {
	if p.cancelLoad != nil {
		p.cancelLoad()
		p.cancelLoad = nil
	}
	if out := p.out; out != nil {
		out.Pause()
		p.out = nil
		time.AfterFunc(80*time.Millisecond, out.Close)
	}
	if st := p.stream; st != nil {
		p.stream = nil
		st.Close()
	}
	p.info = nil
	p.fade.attach(nil, 1)
}

func (p *Player) playCurrent() {
	p.mu.Lock()
	if p.at < 0 || p.at >= len(p.order) {
		p.mu.Unlock()
		return
	}
	track := p.queue[p.order[p.at]]
	resumeAt := time.Duration(0)
	if p.resumeID != "" && p.resumeID == track.ID {
		resumeAt = p.resumeAt
	}
	p.resumeAt, p.resumeID = 0, ""
	p.track = &track
	p.loading = true
	p.playing = false
	p.pendingPlay = true
	p.pauseOffset = resumeAt
	p.playStart = time.Now()

	p.stop()
	p.gen++
	gen := p.gen

	ctx, cancel := context.WithCancel(context.Background())
	p.cancelLoad = cancel
	p.mu.Unlock()
	p.changed()

	resolver := p.Resolve
	if resolver == nil {
		resolver = ytmusic.DefaultClient.ResolveStream
	}

	// Load stream on goroutine
	go func() {
		info, err := resolver(ctx, track.ID)
		if err != nil {
			p.post(func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				if gen != p.gen {
					return
				}
				p.loading = false
				p.playing = false
				p.pendingPlay = false
				p.changed()
			})
			return
		}

		// Keep the queue entry complete when the source track had no duration.
		resolved := int64(0)
		if info.DurationMs > 0 {
			p.mu.Lock()
			if track.DurationMs <= 0 {
				track.DurationMs = info.DurationMs
				resolved = info.DurationMs
			}
			p.mu.Unlock()
		}
		if resolved > 0 && p.DurationResolved != nil {
			p.DurationResolved(track.ID, resolved)
		}

		var st stream
		switch {
		case info.HEAAC:
			st, err = openHEAACStream(ctx, info, p.Cache)
		case info.Format == "mp4" && info.DirectURL != "":
			cachePath := p.Cache.Path(info.VideoID, "mp4")
			durMs := track.DurationMs
			if durMs <= 0 {
				durMs = info.DurationMs
			}
			st, err = openStreamingMP4(ctx, info.DirectURL, cachePath, durMs, info.ContentLength, resumeAt)
			if err != nil && (errors.Is(err, aacpcm.ErrUnsupportedSBR) || errors.Is(err, aacpcm.ErrUnsupportedPS)) {
				st, err = openHEAACStream(ctx, info, p.Cache)
			}
		default:
			st, err = openHLSStream(ctx, info, p.Cache)
		}

		if err != nil {
			p.post(func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				if gen != p.gen {
					return
				}
				p.loading = false
				p.playing = false
				p.pendingPlay = false
				p.changed()
			})
			return
		}

		p.mu.Lock()
		if gen != p.gen {
			p.mu.Unlock()
			st.Close()
			return
		}
		p.mu.Unlock()

		out := audio.NewPlayer(st)
		if resumeAt > 0 {
			out.Seek(resumeAt)
		}
		out.OnEnd(func() {
			p.post(func() {
				p.onTrackEnd(out)
			})
		})

		p.post(func() {
			p.mu.Lock()
			if gen != p.gen {
				p.mu.Unlock()
				st.Close()
				out.Close()
				return
			}
			p.stream = st
			p.out = out
			p.info = info
			p.loading = false
			playNow := p.pendingPlay
			p.pendingPlay = false
			p.playing = playNow
			p.fade.attach(p.out, 1.0)
			if playNow {
				p.playStart = time.Now()
				p.out.Play()
			}
			p.mu.Unlock()
			p.changed()

			// Check if approaching queue end to ask radio
			p.checkRadioFeed()
		})
	}()
}

func (p *Player) checkRadioFeed() {
	p.mu.Lock()
	if p.feed == nil || p.feeding || len(p.queue)-p.at > 3 {
		p.mu.Unlock()
		return
	}
	p.feeding = true
	feed := p.feed
	gen := p.radioGen
	p.mu.Unlock()

	go feed(func(more []ytmusic.Track) {
		p.post(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.feeding = false
			if p.radioGen != gen || len(more) == 0 {
				return
			}
			start := len(p.queue)
			p.queue = append(p.queue, more...)
			for i := range more {
				p.order = append(p.order, start+i)
			}
			p.changed()
		})
	})
}

// AttachFeed configures auto-continuation for a queue that loads its next
// page as playback approaches the end.
func (p *Player) AttachFeed(feed func(got func([]ytmusic.Track))) {
	p.mu.Lock()
	p.feed = feed
	p.radioGen++
	p.mu.Unlock()
	p.checkRadioFeed()
}

// DetachFeed disconnects auto-continuation.
func (p *Player) DetachFeed() {
	p.mu.Lock()
	p.feed = nil
	p.radioGen++
	p.mu.Unlock()
}

// AttachRadio configures auto-continuation for endless radio playback.
func (p *Player) AttachRadio(radioID string, feed func(got func([]ytmusic.Track))) {
	p.mu.Lock()
	p.radio = radioID
	p.feed = feed
	p.radioGen++
	p.mu.Unlock()
	p.checkRadioFeed()
}

// DetachRadio disconnects radio auto-feed.
func (p *Player) DetachRadio() {
	p.mu.Lock()
	p.radio = ""
	p.feed = nil
	p.radioGen++
	p.mu.Unlock()
}

// onTrackEnd advances the queue when the current output reaches its end.
func (p *Player) onTrackEnd(out *audio.Player) {
	p.mu.Lock()
	if p.out != out {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.advanceAfterEnd()
}

func (p *Player) advanceAfterEnd() {
	p.mu.Lock()
	if p.at < 0 || p.at >= len(p.order) {
		p.playing = false
		p.mu.Unlock()
		p.changed()
		return
	}
	hasNext := p.repeat != RepeatOff || p.at+1 < len(p.order)
	p.playing = false
	p.mu.Unlock()

	if hasNext {
		p.Next()
	} else {
		// End of the queue: rewind so the play button restarts the track.
		p.Seek(0)
	}
	p.changed()
}

// Play starts or resumes playback.
func (p *Player) Play() {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return
	}
	if p.loading {
		p.pendingPlay = true
		p.mu.Unlock()
		p.changed()
		return
	}
	if p.track == nil && len(p.queue) > 0 {
		p.mu.Unlock()
		p.playCurrent()
		return
	}
	if p.track != nil && p.out == nil && p.stream == nil {
		p.mu.Unlock()
		p.playCurrent()
		return
	}
	p.playing = true
	p.playStart = time.Now()
	if p.out != nil {
		p.out.Play()
		p.fade.fade(1.0, fadeIn, nil)
	}
	p.mu.Unlock()
	p.changed()
}

// Pause pauses playback with a quick fade-out.
func (p *Player) Pause() {
	p.mu.Lock()
	if !p.playing && !p.loading {
		p.mu.Unlock()
		return
	}
	loading := p.loading
	if !loading {
		p.pauseOffset += time.Since(p.playStart)
	}
	p.playing = false
	p.pendingPlay = false
	if p.out != nil && !loading {
		p.fade.fade(0.0, fadeOut, func() {
			p.mu.Lock()
			if !p.playing && p.out != nil {
				p.out.Pause()
			}
			p.mu.Unlock()
		})
	}
	p.mu.Unlock()
	p.changed()
}

// PendingPlay reports whether a loading track will start when it is ready.
func (p *Player) PendingPlay() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingPlay
}

// Active reports whether playback is running or a track is loading to play.
func (p *Player) Active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing || (p.loading && p.pendingPlay)
}

// TogglePlay switches between playing and paused.
func (p *Player) TogglePlay() {
	if p.Active() {
		p.Pause()
	} else {
		p.Play()
	}
}

// Next skips to the next track.
func (p *Player) Next() {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return
	}
	if p.repeat == RepeatOne {
		p.mu.Unlock()
		p.playCurrent()
		return
	}
	nextAt := p.at + 1
	if nextAt >= len(p.order) {
		if p.repeat == RepeatAll {
			nextAt = 0
		} else {
			p.mu.Unlock()
			return
		}
	}
	p.at = nextAt
	p.mu.Unlock()
	p.playCurrent()
}

// Prev skips to previous track or restarts the current one if past 3 seconds.
func (p *Player) Prev() {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return
	}
	if p.positionLocked() > 3*time.Second {
		p.pauseOffset = 0
		p.playStart = time.Now()
		if p.out != nil {
			p.out.Seek(0)
		}
		p.mu.Unlock()
		p.changed()
		return
	}
	prevAt := p.at - 1
	if prevAt < 0 {
		prevAt = 0
	}
	p.at = prevAt
	p.mu.Unlock()
	p.playCurrent()
}

func (p *Player) positionLocked() time.Duration {
	if p.out != nil {
		return p.out.Position()
	}
	if !p.playing {
		return p.pauseOffset
	}
	return p.pauseOffset + time.Since(p.playStart)
}

// StreamInfo returns the resolved stream currently playing, if any.
func (p *Player) StreamInfo() *ytmusic.StreamInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

// Position returns current playback position.
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.positionLocked()
}

// Duration returns the total duration of the current track.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.track != nil && p.track.DurationMs > 0 {
		return time.Duration(p.track.DurationMs) * time.Millisecond
	}
	if p.stream != nil && p.stream.Len() > 0 && p.stream.SampleRate() > 0 {
		return time.Duration(float64(p.stream.Len()) / float64(p.stream.SampleRate()) * float64(time.Second))
	}
	return 0
}

// Seek seeks to pos.
func (p *Player) Seek(pos time.Duration) {
	p.mu.Lock()
	p.pauseOffset = pos
	p.playStart = time.Now()
	if p.out != nil {
		p.out.Seek(pos)
	} else if p.stream != nil {
		sr := p.stream.SampleRate()
		if sr > 0 {
			frame := int64(pos.Seconds() * float64(sr))
			_ = p.stream.SetPosition(frame)
		}
	}
	p.mu.Unlock()
	p.changed()
}

// Spectrum returns current frequency spectrum readings.
func (p *Player) Spectrum() []float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == nil || !p.playing {
		return make([]float32, 16)
	}
	return p.stream.Spectrum()
}

// Session returns snapshot for persistence.
func (p *Player) Session() (Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return Session{}, false
	}
	atIdx := -1
	if p.at >= 0 && p.at < len(p.order) {
		atIdx = p.order[p.at]
	}
	return Session{
		Queue:    append([]ytmusic.Track(nil), p.queue...),
		At:       atIdx,
		Position: p.positionLocked(),
		Shuffle:  p.shuffle,
		Repeat:   p.repeat,
		Volume:   p.volume,
		Radio:    p.radio,
	}, true
}

// Restore restores playback from a saved session.
func (p *Player) Restore(s Session) {
	p.mu.Lock()
	p.queue = append([]ytmusic.Track(nil), s.Queue...)
	p.shuffle = s.Shuffle
	p.repeat = s.Repeat
	p.volume = s.Volume
	p.reorder()
	p.playing = false
	p.loading = false
	if s.At >= 0 && s.At < len(p.queue) {
		for i, idx := range p.order {
			if idx == s.At {
				p.at = i
				break
			}
		}
		track := p.queue[s.At]
		p.track = &track
		p.pauseOffset = s.Position
		p.resumeAt = s.Position
		p.resumeID = track.ID
		p.playStart = time.Now()
	}
	p.fade.setGain(p.volume)
	p.mu.Unlock()
	p.changed()
}
