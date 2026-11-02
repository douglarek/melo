package player

import (
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
)

const (
	fadeOut = 180 * time.Millisecond
	fadeIn  = 260 * time.Millisecond
)

type fader struct {
	mu    sync.Mutex
	out   *audio.Player
	gain  float64
	from  float64
	to    float64
	start time.Time
	dur   time.Duration
	gen   int
}

func (f *fader) level(now time.Time) float64 {
	t := 1.0
	if f.dur > 0 {
		t = min(1, now.Sub(f.start).Seconds()/f.dur.Seconds())
	}
	return f.from + (f.to-f.from)*(0.5-0.5*math.Cos(math.Pi*t))
}

func (f *fader) apply(now time.Time) {
	if f.out != nil {
		f.out.SetVolume(f.gain * f.level(now))
	}
}

func (f *fader) attach(out *audio.Player, level float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	f.out, f.from, f.to, f.dur = out, level, level, 0
	f.apply(time.Now())
}

func (f *fader) setGain(g float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gain = g
	f.apply(time.Now())
}

func (f *fader) silent() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.level(time.Now()) == 0
}

func (f *fader) fade(to float64, d time.Duration, done func()) {
	f.mu.Lock()
	now := time.Now()
	from := f.level(now)
	f.gen++
	gen := f.gen
	if span := math.Abs(to - from); span < 1 {
		d = time.Duration(float64(d) * span)
	}
	f.from, f.to, f.start, f.dur = from, to, now, d
	f.mu.Unlock()

	go func() {
		tick := time.NewTicker(8 * time.Millisecond)
		defer tick.Stop()
		for {
			f.mu.Lock()
			if f.gen != gen {
				f.mu.Unlock()
				return
			}
			now := time.Now()
			f.apply(now)
			finished := now.Sub(f.start) >= f.dur
			f.mu.Unlock()
			if finished {
				if done != nil {
					done()
				}
				return
			}
			<-tick.C
		}
	}()
}
