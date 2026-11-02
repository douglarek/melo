package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
	aacpcm "github.com/tphakala/go-aac/pcm"
	"github.com/tphakala/go-m4a/aacm4a"

	"github.com/douglarek/melo/internal/ytmusic"
)

type stream interface {
	audio.Decoder
	Close() error
	Spectrum() []float32
}

type hlsStream struct {
	mu          sync.Mutex
	info        *ytmusic.StreamInfo
	cache       *Cache
	client      *http.Client
	sampleRate  int
	channels    int
	totalFrames int64
	curFrame    int64

	// Segments state
	segments    []ytmusic.StreamSegment
	segData     [][]byte
	downloading map[int]bool
	cancel      context.CancelFunc
	ctx         context.Context

	// Audio decoder
	decReader *segmentReader
	decoder   *aacpcm.Decoder
	pcmBuffer []float32
	spectrum  []float32
}

func openHLSStream(ctx context.Context, info *ytmusic.StreamInfo, cache *Cache) (*hlsStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	sr := 44100
	if info.SampleRate > 0 {
		sr = info.SampleRate
	}
	ch := 2
	if info.Channels > 0 {
		ch = info.Channels
	}
	totalFrames := info.DurationMs * int64(sr) / 1000

	s := &hlsStream{
		info:        info,
		cache:       cache,
		client:      &http.Client{Timeout: 15 * time.Second},
		sampleRate:  sr,
		channels:    ch,
		totalFrames: totalFrames,
		segments:    info.Segments,
		segData:     make([][]byte, len(info.Segments)),
		downloading: make(map[int]bool),
		cancel:      cancel,
		ctx:         streamCtx,
		spectrum:    make([]float32, 16),
	}

	// Check disk cache first
	if cache != nil {
		if cachedAudio, _, ok := cache.Lookup(info.VideoID); ok && len(cachedAudio) > 0 {
			dec, err := aacpcm.NewDecoder(bytes.NewReader(cachedAudio))
			if err == nil {
				s.decoder = dec
				return s, nil
			}
		}
	}

	// Prepare segment reader
	s.decReader = &segmentReader{s: s}
	// Pre-fetch first 2 segments synchronously to initialize decoder
	if len(s.segments) > 0 {
		_ = s.fetchSegment(0)
	}
	if len(s.segments) > 1 {
		go s.fetchSegment(1)
	}

	dec, err := aacpcm.NewDecoder(s.decReader)
	if err != nil {
		// Fallback: dummy empty stream rather than crashing
		s.decoder = nil
	} else {
		s.decoder = dec
	}

	// Start background fetching of remaining segments
	go s.prefetchAll()

	return s, nil
}

func (s *hlsStream) fetchSegment(idx int) error {
	s.mu.Lock()
	if idx < 0 || idx >= len(s.segments) || s.segData[idx] != nil || s.downloading[idx] {
		s.mu.Unlock()
		return nil
	}
	s.downloading[idx] = true
	segURI := s.segments[idx].URI
	s.mu.Unlock()

	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, segURI, nil)
	if err != nil {
		s.mu.Lock()
		s.downloading[idx] = false
		s.mu.Unlock()
		return err
	}
	req.Header.Set("User-Agent", "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)")

	resp, err := s.client.Do(req)
	if err != nil {
		s.mu.Lock()
		s.downloading[idx] = false
		s.mu.Unlock()
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		s.mu.Lock()
		s.downloading[idx] = false
		s.mu.Unlock()
		return err
	}

	// Strip ID3 tag if present
	cleanData := stripID3(data)

	s.mu.Lock()
	s.segData[idx] = cleanData
	s.downloading[idx] = false
	s.mu.Unlock()

	return nil
}

func stripID3(data []byte) []byte {
	if len(data) > 10 && string(data[:3]) == "ID3" {
		id3Len := int(data[6]&0x7F)<<21 | int(data[7]&0x7F)<<14 | int(data[8]&0x7F)<<7 | int(data[9]&0x7F)
		offset := 10 + id3Len
		if offset < len(data) {
			return data[offset:]
		}
	}
	return data
}

func (s *hlsStream) prefetchAll() {
	for i := range s.segments {
		select {
		case <-s.ctx.Done():
			return
		default:
			_ = s.fetchSegment(i)
		}
	}

	// When all segments are downloaded, save to disk cache
	s.mu.Lock()
	defer s.mu.Unlock()
	allDownloaded := true
	var fullAudio []byte
	for _, seg := range s.segData {
		if seg == nil {
			allDownloaded = false
			break
		}
		fullAudio = append(fullAudio, seg...)
	}
	if allDownloaded && s.cache != nil && len(fullAudio) > 0 {
		s.cache.Save(s.info.VideoID, s.info, fullAudio)
	}
}

func (s *hlsStream) SampleRate() int {
	return s.sampleRate
}

func (s *hlsStream) Channels() int {
	return s.channels
}

func (s *hlsStream) Len() int64 {
	return s.totalFrames
}

func (s *hlsStream) Read(buf []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.decoder == nil {
		return 0, io.EOF
	}

	neededBytes := len(buf) * 2 // 16-bit PCM = 2 bytes per sample
	pcmRaw := make([]byte, neededBytes)
	nBytes, err := s.decoder.Read(pcmRaw)
	if nBytes <= 0 && err != nil {
		return 0, err
	}

	samplesRead := nBytes / 2
	for i := 0; i < samplesRead; i++ {
		val := int16(binary.LittleEndian.Uint16(pcmRaw[i*2 : i*2+2]))
		f := float32(val) / 32768.0
		buf[i] = f
	}

	// Update spectrum measurement
	s.updateSpectrum(buf[:samplesRead])
	s.curFrame += int64(samplesRead / s.channels)

	return samplesRead, err
}

func (s *hlsStream) updateSpectrum(samples []float32) {
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
		// Smooth filter
		s.spectrum[b] = s.spectrum[b]*0.6 + avg*0.4
	}
}

func (s *hlsStream) Spectrum() []float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]float32, len(s.spectrum))
	copy(res, s.spectrum)
	return res
}

func (s *hlsStream) SetPosition(frame int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetMs := frame * 1000 / int64(s.sampleRate)
	var accumulatedMs int64
	targetSeg := 0
	for i, seg := range s.segments {
		if accumulatedMs+seg.DurationMs > targetMs {
			targetSeg = i
			break
		}
		accumulatedMs += seg.DurationMs
	}

	// Seek segment reader to targetSeg
	s.decReader.segIdx = targetSeg
	s.decReader.offset = 0

	dec, err := aacpcm.NewDecoder(s.decReader)
	if err != nil {
		return err
	}
	s.decoder = dec
	s.curFrame = frame
	return nil
}

func (s *hlsStream) Close() error {
	s.cancel()
	return nil
}

// segmentReader streams through fetched segments sequentially.
type segmentReader struct {
	s      *hlsStream
	segIdx int
	offset int
}

func (sr *segmentReader) Read(p []byte) (int, error) {
	for {
		sr.s.mu.Lock()
		if sr.segIdx >= len(sr.s.segments) {
			sr.s.mu.Unlock()
			return 0, io.EOF
		}
		data := sr.s.segData[sr.segIdx]
		sr.s.mu.Unlock()

		if data == nil {
			// Fetch on demand
			if err := sr.s.fetchSegment(sr.segIdx); err != nil {
				return 0, err
			}
			sr.s.mu.Lock()
			data = sr.s.segData[sr.segIdx]
			sr.s.mu.Unlock()
		}

		if data != nil && sr.offset < len(data) {
			n := copy(p, data[sr.offset:])
			sr.offset += n
			return n, nil
		}

		// Move to next segment
		sr.segIdx++
		sr.offset = 0
	}
}

// mp4Stream decodes an MP4/M4A audio file using aacm4a.NewSeekableDecoder.
type mp4Stream struct {
	mu          sync.Mutex
	file        *os.File
	dec         *aacm4a.Decoder
	sampleRate  int
	channels    int
	totalFrames int64
	curFrame    int64
	spectrum    []float32
	cancel      context.CancelFunc
}

func openMP4Stream(filePath string) (*mp4Stream, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	dec, info, err := aacm4a.NewSeekableDecoder(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	sr := info.SampleRate
	if sr <= 0 {
		sr = 44100
	}
	ch := info.Channels
	if ch <= 0 {
		ch = 2
	}
	totalFrames := int64(info.Duration.Seconds() * float64(sr))

	return &mp4Stream{
		file:        f,
		dec:         dec,
		sampleRate:  sr,
		channels:    ch,
		totalFrames: totalFrames,
		spectrum:    make([]float32, 16),
	}, nil
}

func (s *mp4Stream) SampleRate() int {
	return s.sampleRate
}

func (s *mp4Stream) Channels() int {
	return s.channels
}

func (s *mp4Stream) Len() int64 {
	return s.totalFrames
}

func (s *mp4Stream) Read(buf []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dec == nil {
		return 0, io.EOF
	}

	neededBytes := len(buf) * 2 // 16-bit PCM = 2 bytes per sample
	pcmRaw := make([]byte, neededBytes)
	nBytes, err := s.dec.Read(pcmRaw)
	if nBytes <= 0 && err != nil {
		return 0, err
	}

	samplesRead := nBytes / 2
	for i := 0; i < samplesRead; i++ {
		val := int16(binary.LittleEndian.Uint16(pcmRaw[i*2 : i*2+2]))
		f := float32(val) / 32768.0
		buf[i] = f
	}

	s.updateSpectrum(buf[:samplesRead])
	s.curFrame += int64(samplesRead / s.channels)
	return samplesRead, err
}

func (s *mp4Stream) updateSpectrum(samples []float32) {
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

func (s *mp4Stream) Spectrum() []float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]float32, len(s.spectrum))
	copy(res, s.spectrum)
	return res
}

func (s *mp4Stream) SetPosition(frame int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := time.Duration(float64(frame) / float64(s.sampleRate) * float64(time.Second))
	err := s.dec.SeekTime(t)
	if err == nil {
		s.curFrame = frame
	}
	return err
}

func (s *mp4Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		return err
	}
	return nil
}

type download struct {
	mu     sync.Mutex
	cond   sync.Cond
	file   *os.File
	path   string
	part   string
	n      int64
	done   bool
	err    error
	cancel context.CancelFunc
}

func startDownload(ctx context.Context, u string, destPath string, contentLength int64) (*download, error) {
	partPath := destPath + ".part"
	f, err := os.Create(partPath)
	if err != nil {
		return nil, err
	}

	dlCtx, cancel := context.WithCancel(ctx)
	d := &download{
		file:   f,
		path:   destPath,
		part:   partPath,
		cancel: cancel,
	}
	d.cond.L = &d.mu

	go func() {
		defer func() {
			d.mu.Lock()
			d.done = true
			d.cond.Broadcast()
			d.mu.Unlock()
		}()

		var dlErr error
		if contentLength > 0 {
			dlErr = d.downloadChunks(dlCtx, u, contentLength)
		} else {
			dlErr = d.downloadAll(dlCtx, u)
		}

		d.mu.Lock()
		if dlErr != nil {
			d.err = dlErr
		} else {
			_ = os.Rename(partPath, destPath)
		}
		d.mu.Unlock()
	}()

	return d, nil
}

func (d *download) downloadAll(ctx context.Context, u string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("stream request HTTP %d", resp.StatusCode)
	}
	return d.copyBody(resp.Body)
}

func (d *download) downloadChunks(ctx context.Context, u string, total int64) error {
	const chunkSize = 256 * 1024
	for start := int64(0); start < total; start += chunkSize {
		end := start + chunkSize - 1
		if end >= total {
			end = total - 1
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip")
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return fmt.Errorf("server ignored range request")
			}
			return fmt.Errorf("stream request HTTP %d", resp.StatusCode)
		}
		err = d.copyBody(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *download) copyBody(r io.Reader) error {
	buf := make([]byte, 64*1024)
	for {
		nr, er := r.Read(buf)
		if nr > 0 {
			nw, ew := d.file.Write(buf[:nr])
			d.mu.Lock()
			d.n += int64(nw)
			d.cond.Broadcast()
			d.mu.Unlock()
			if ew != nil {
				return ew
			}
		}
		if er == io.EOF {
			return nil
		}
		if er != nil {
			return er
		}
	}
}

type downloadReader struct {
	d   *download
	pos int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	d := r.d
	d.mu.Lock()
	for d.n <= r.pos && !d.done {
		d.cond.Wait()
	}
	if d.n <= r.pos {
		err := d.err
		d.mu.Unlock()
		if err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	avail := d.n - r.pos
	d.mu.Unlock()

	toRead := int64(len(p))
	if toRead > avail {
		toRead = avail
	}
	n, err := d.file.ReadAt(p[:toRead], r.pos)
	r.pos += int64(n)
	return n, err
}

func (r *downloadReader) Seek(offset int64, whence int) (int64, error) {
	d := r.d
	d.mu.Lock()
	defer d.mu.Unlock()
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = r.pos + offset
	case io.SeekEnd:
		for !d.done {
			d.cond.Wait()
		}
		newPos = d.n + offset
	}
	if newPos < 0 {
		return 0, fmt.Errorf("negative seek offset")
	}
	r.pos = newPos
	return newPos, nil
}

func openStreamingMP4(ctx context.Context, u string, cachePath string, durationMs int64, contentLength int64, resumeAt time.Duration) (*mp4Stream, error) {
	// If already fully cached on disk:
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		if st, err := openMP4Stream(cachePath); err == nil {
			return st, nil
		}
		_ = os.Remove(cachePath)
	}

	dl, err := startDownload(ctx, u, cachePath, contentLength)
	if err != nil {
		return nil, err
	}

	// Seeking a fragmented MP4 requires the complete file, so a resumed
	// uncached track waits for its download before opening the decoder.
	if resumeAt > 0 {
		dl.mu.Lock()
		for !dl.done {
			dl.cond.Wait()
		}
		err = dl.err
		dl.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if dl.file != nil {
			_ = dl.file.Close()
		}
		return openMP4Stream(cachePath)
	}

	// Wait for the first 160 KB (contains ftyp and moov box at offset 24)
	dl.mu.Lock()
	for dl.n < 160*1024 && !dl.done {
		dl.cond.Wait()
	}
	if dl.err != nil {
		dl.mu.Unlock()
		return nil, dl.err
	}
	dl.mu.Unlock()

	r := &downloadReader{d: dl}
	dec, info, err := aacm4a.NewSeekableDecoder(r)
	if err != nil {
		// Wait for up to 350 KB if header was slightly larger
		dl.mu.Lock()
		for dl.n < 350*1024 && !dl.done {
			dl.cond.Wait()
		}
		dl.mu.Unlock()
		r.pos = 0
		dec, info, err = aacm4a.NewSeekableDecoder(r)
		if err != nil {
			return nil, err
		}
	}

	sr := info.SampleRate
	if sr <= 0 {
		sr = 44100
	}
	ch := info.Channels
	if ch <= 0 {
		ch = 2
	}
	totalFrames := int64(info.Duration.Seconds() * float64(sr))
	if totalFrames <= 0 && durationMs > 0 {
		totalFrames = durationMs * int64(sr) / 1000
	}

	return &mp4Stream{
		file:        dl.file,
		dec:         dec,
		sampleRate:  sr,
		channels:    ch,
		totalFrames: totalFrames,
		spectrum:    make([]float32, 16),
		cancel:      dl.cancel,
	}, nil
}

// DownloadToFile downloads url directly to destPath.
func DownloadToFile(ctx context.Context, u string, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	partPath := destPath + ".part"
	out, err := os.Create(partPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		_ = os.Remove(partPath)
		return err
	}

	return os.Rename(partPath, destPath)
}
