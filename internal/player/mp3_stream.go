package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"

	mp3 "github.com/tphakala/go-mp3"

	"github.com/douglarek/melo/internal/ytmusic"
)

// mp3Stream decodes an MP3 served over HTTP, which is how a podcast episode
// arrives. Frames decode as they download, and a seek reads the byte the
// time maps to, which the stream's constant bitrate makes exact.
type mp3Stream struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	url    string

	body io.ReadCloser
	dec  *mp3.Decoder
	// pending holds downloaded bytes the decoder has not consumed, chunk the
	// buffer a read fills, and pcm one frame of decoded samples.
	pending []byte
	chunk   []byte
	pcm     []float32
	// samples holds decoded samples the player has not read.
	samples    []float32
	eof        bool
	closed     bool
	sampleRate int
	channels   int
	// bytesPerSecond maps a position to the offset a seek reads from.
	bytesPerSecond int64
	contentLength  int64
	totalFrames    int64
	curFrame       int64
	spectrum       []float32
}

const (
	// mp3ReadChunk is how many bytes one read from the network asks for.
	mp3ReadChunk = 64 << 10
	// mp3HoldBack is how many bytes to keep for the next frame, more than
	// the longest MP3 frame, so one never straddles a buffer.
	mp3HoldBack = 16 << 10
	// mp3FrameSamples is the most samples a frame holds per channel.
	mp3FrameSamples = 1152
)

func openMP3Stream(ctx context.Context, info *ytmusic.StreamInfo) (*mp3Stream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	s := &mp3Stream{
		ctx:           streamCtx,
		cancel:        cancel,
		url:           info.DirectURL,
		contentLength: info.ContentLength,
		chunk:         make([]byte, mp3ReadChunk),
		pcm:           make([]float32, mp3FrameSamples*2),
		spectrum:      make([]float32, 16),
	}
	if err := s.open(0); err != nil {
		cancel()
		return nil, err
	}
	if err := s.probe(info); err != nil {
		cancel()
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// probe decodes the first frame, which gives the stream its rate, its
// channels, and the bitrate a seek needs.
func (s *mp3Stream) probe(info *ytmusic.StreamInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if err := s.decodeFrame(); err != nil {
			return err
		}
		if len(s.samples) > 0 {
			break
		}
		if s.eof && len(s.pending) == 0 {
			return errors.New("mp3: no audio frame")
		}
	}
	if info.DurationMs > 0 && s.sampleRate > 0 {
		s.totalFrames = info.DurationMs * int64(s.sampleRate) / 1000
	}
	if s.bytesPerSecond <= 0 && s.contentLength > 0 && info.DurationMs > 0 {
		s.bytesPerSecond = s.contentLength * 1000 / info.DurationMs
	}
	return nil
}

func (s *mp3Stream) SampleRate() int { return s.sampleRate }
func (s *mp3Stream) Channels() int   { return s.channels }
func (s *mp3Stream) Len() int64      { return s.totalFrames }

func (s *mp3Stream) Read(buf []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.EOF
	}
	n := 0
	for n < len(buf) {
		if len(s.samples) > 0 {
			c := copy(buf[n:], s.samples)
			s.samples = s.samples[c:]
			n += c
			continue
		}
		if err := s.decodeFrame(); err != nil {
			if n > 0 {
				break
			}
			return 0, err
		}
	}
	s.updateSpectrum(buf[:n])
	return n, nil
}

// decodeFrame decodes the next frame into samples, downloading more when
// the buffer holds too little to tell a frame from its edge.
func (s *mp3Stream) decodeFrame() error {
	for {
		if len(s.pending) == 0 {
			if s.eof {
				return io.EOF
			}
			if err := s.fill(); err != nil {
				return err
			}
			continue
		}
		if !s.eof && len(s.pending) < mp3HoldBack {
			if err := s.fill(); err != nil {
				return err
			}
			continue
		}
		n, frame, err := s.dec.DecodeFrame(s.pending, s.pcm)
		if frame.Channels > 0 {
			s.sampleRate = frame.SampleRate
			s.channels = frame.Channels
			if frame.Bitrate > 0 {
				s.bytesPerSecond = int64(frame.Bitrate) * 125
			}
		}
		if frame.FrameBytes == 0 {
			if s.eof {
				return io.EOF
			}
			if err := s.fill(); err != nil {
				return err
			}
			continue
		}
		s.pending = s.pending[frame.FrameBytes:]
		if n > 0 {
			s.samples = append(s.samples, s.pcm[:n*frame.Channels]...)
			s.curFrame += int64(n)
			return nil
		}
		if err != nil && !errors.Is(err, mp3.ErrUnsupported) {
			return err
		}
	}
}

func (s *mp3Stream) fill() error {
	n, err := s.body.Read(s.chunk)
	if n > 0 {
		s.pending = append(s.pending, s.chunk[:n]...)
	}
	if errors.Is(err, io.EOF) {
		s.eof = true
		return nil
	}
	return err
}

// open reads the stream from offset, which the Range header serves.
func (s *mp3Stream) open(offset int64) error {
	if s.body != nil {
		_ = s.body.Close()
		s.body = nil
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		_ = resp.Body.Close()
		return fmt.Errorf("mp3 request HTTP %d", resp.StatusCode)
	}
	s.body = resp.Body
	s.dec = mp3.NewDecoder()
	s.pending = s.pending[:0]
	s.samples = s.samples[:0]
	s.eof = false
	return nil
}

func (s *mp3Stream) SetPosition(frame int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("mp3: stream is closed")
	}
	if frame < 0 {
		frame = 0
	}
	if s.totalFrames > 0 && frame > s.totalFrames {
		frame = s.totalFrames
	}
	offset := int64(0)
	if s.bytesPerSecond > 0 && s.sampleRate > 0 {
		offset = frame * s.bytesPerSecond / int64(s.sampleRate)
		if s.contentLength > 0 && offset > s.contentLength {
			offset = s.contentLength
		}
	}
	if err := s.open(offset); err != nil {
		return err
	}
	s.curFrame = frame
	return nil
}

func (s *mp3Stream) updateSpectrum(samples []float32) {
	if len(samples) == 0 {
		return
	}
	bands := len(s.spectrum)
	chunkSize := max(1, len(samples)/bands)
	for b := 0; b < bands; b++ {
		var sum float64
		start := b * chunkSize
		end := min(len(samples), start+chunkSize)
		for i := start; i < end; i++ {
			sum += math.Abs(float64(samples[i]))
		}
		avg := float32(sum / float64(max(1, end-start)))
		s.spectrum[b] = s.spectrum[b]*0.6 + avg*0.4
	}
}

func (s *mp3Stream) Spectrum() []float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]float32, len(s.spectrum))
	copy(res, s.spectrum)
	return res
}

func (s *mp3Stream) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.body != nil {
		err := s.body.Close()
		s.body = nil
		return err
	}
	return nil
}
