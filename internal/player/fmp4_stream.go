package player

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	aacpcm "github.com/tphakala/go-aac/pcm"

	"github.com/douglarek/melo/internal/ytmusic"
)

// fmp4Stream decodes a SABR download as it arrives: fmp4Reader demuxes the
// growing fragmented MP4 into access units and go-aac decodes them.
type fmp4Stream struct {
	mu          sync.Mutex
	sess        *ytmusic.SabrSession
	rd          *fmp4Reader
	dec         *aacpcm.Decoder
	sampleRate  int
	channels    int
	totalFrames int64
	curFrame    int64
	spectrum    []float32
	closed      bool
}

func openSabrPure(sess *ytmusic.SabrSession, info *ytmusic.StreamInfo) (*fmp4Stream, error) {
	initOff, initLen := sess.InitRange()
	if initLen <= 0 {
		return nil, errors.New("fmp4: no init segment")
	}
	rd, err := newFMP4Reader(sess, sess.Total(), initOff, initLen, sess.SeekPoint)
	if err != nil {
		return nil, err
	}
	dec, err := aacpcm.NewDecoder(rd, aacpcm.WithRawStream(rd.ASC()))
	if err != nil {
		return nil, err
	}
	sr := info.SampleRate
	if sr <= 0 {
		sr = rd.init.sampleRate
	}
	ch := info.Channels
	if ch <= 0 {
		ch = rd.init.channels
	}
	total := int64(0)
	if info.DurationMs > 0 {
		total = info.DurationMs * int64(sr) / 1000
	}
	return &fmp4Stream{
		sess:        sess,
		rd:          rd,
		dec:         dec,
		sampleRate:  sr,
		channels:    ch,
		totalFrames: total,
		spectrum:    make([]float32, 16),
	}, nil
}

func (s *fmp4Stream) SampleRate() int { return s.sampleRate }
func (s *fmp4Stream) Channels() int   { return s.channels }
func (s *fmp4Stream) Len() int64      { return s.totalFrames }

func (s *fmp4Stream) Read(buf []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.dec == nil {
		return 0, io.EOF
	}
	raw := make([]byte, len(buf)*2)
	n, err := s.dec.Read(raw)
	samples := n / 2
	for i := 0; i < samples; i++ {
		v := int16(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
		buf[i] = float32(v) / 32768
	}
	if samples > 0 {
		s.updateSpectrum(buf[:samples])
		s.curFrame += int64(samples / s.channels)
	}
	if err == io.EOF {
		return samples, io.EOF
	}
	return samples, err
}

func (s *fmp4Stream) updateSpectrum(samples []float32) {
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

func (s *fmp4Stream) Spectrum() []float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]float32, len(s.spectrum))
	copy(res, s.spectrum)
	return res
}

// SetPosition seeks to frame by restarting the demuxer at the movie
// fragment that covers it.
func (s *fmp4Stream) SetPosition(frame int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("fmp4: stream closed")
	}
	ms := frame * 1000 / int64(s.sampleRate)
	if err := s.rd.SeekTime(ms); err != nil {
		return err
	}
	if s.dec != nil {
		if err := s.dec.Reset(s.rd, aacpcm.WithRawStream(s.rd.ASC())); err != nil {
			return err
		}
	}
	s.curFrame = frame
	return nil
}

func (s *fmp4Stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	if s.sess != nil {
		return s.sess.Close()
	}
	return nil
}

// sabrWaitTimeout bounds how long a fallback waits for the whole file.
const sabrWaitTimeout = 5 * time.Minute

var _ stream = (*fmp4Stream)(nil)
