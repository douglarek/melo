package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
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
	// mu protects segment state, counters, and decoder metadata.
	mu sync.Mutex
	// decMu serializes decoder construction and use.
	decMu       sync.Mutex
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
	segErr      []error
	downloading map[int]bool
	cancel      context.CancelFunc
	ctx         context.Context
	closeOnce   sync.Once

	// Audio decoder
	cacheHit  bool
	cacheData []byte
	decReader *segmentReader
	decoder   *aacpcm.Decoder
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
		segErr:      make([]error, len(info.Segments)),
		downloading: make(map[int]bool),
		cancel:      cancel,
		ctx:         streamCtx,
		spectrum:    make([]float32, 16),
	}

	// Cached audio is decoded from memory; seeking rebuilds the decoder and
	// discards frames before the target.
	if cache != nil {
		if cachedAudio, cachedInfo, ok := cache.Lookup(info.VideoID); ok && len(cachedAudio) > 0 {
			if dec, err := aacpcm.NewDecoder(bytes.NewReader(cachedAudio)); err == nil {
				s.cacheHit = true
				s.cacheData = cachedAudio
				s.decoder = dec
				s.setDecoderInfo(dec.Info(), cachedInfo)
				return s, nil
			}
		}
	}

	if len(s.segments) == 0 {
		cancel()
		return nil, errors.New("hls stream has no segments")
	}

	// Prepare segment reader
	s.decReader = &segmentReader{s: s}
	// Fetch the first segment before constructing the decoder; fetch the
	// second concurrently.
	if err := s.fetchSegment(0); err != nil {
		cancel()
		return nil, fmt.Errorf("hls first segment: %w", err)
	}
	if len(s.segments) > 1 {
		go func() {
			_ = s.fetchSegment(1)
		}()
	}

	dec, err := aacpcm.NewDecoder(s.decReader)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hls decoder: %w", err)
	}
	s.decoder = dec
	s.setDecoderInfo(dec.Info(), nil)

	// Start background fetching of remaining segments
	go s.prefetchAll()

	return s, nil
}

// setDecoderInfo applies the format reported by the decoder during
// construction.
func (s *hlsStream) setDecoderInfo(info aacpcm.Info, cached *ytmusic.StreamInfo) {
	if info.SampleRate > 0 {
		s.sampleRate = info.SampleRate
	}
	if info.Channels > 0 {
		s.channels = info.Channels
	}
	durationMs := s.info.DurationMs
	if cached != nil && cached.DurationMs > 0 {
		durationMs = cached.DurationMs
	}
	if durationMs > 0 {
		s.totalFrames = durationMs * int64(s.sampleRate) / 1000
	}
}

func (s *hlsStream) fetchSegment(idx int) error {
	s.mu.Lock()
	if idx < 0 || idx >= len(s.segments) || s.segData[idx] != nil || s.downloading[idx] {
		s.mu.Unlock()
		return nil
	}
	if err := s.segErr[idx]; err != nil {
		s.mu.Unlock()
		return err
	}
	s.downloading[idx] = true
	segURI := s.segments[idx].URI
	s.mu.Unlock()

	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, segURI, nil)
	if err != nil {
		s.failSegment(idx, err)
		return err
	}
	req.Header.Set("User-Agent", "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)")

	resp, err := s.client.Do(req)
	if err != nil {
		s.failSegment(idx, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err := fmt.Errorf("segment %d HTTP %d: %s", idx, resp.StatusCode, string(body))
		s.failSegment(idx, err)
		return err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		s.failSegment(idx, err)
		return err
	}

	// Strip ID3 tag if present
	cleanData := stripID3(data)

	s.mu.Lock()
	s.segData[idx] = cleanData
	s.segErr[idx] = nil
	s.downloading[idx] = false
	s.mu.Unlock()

	return nil
}

func (s *hlsStream) failSegment(idx int, err error) {
	s.mu.Lock()
	s.downloading[idx] = false
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		s.segErr[idx] = err
	}
	s.mu.Unlock()
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
			if err := s.fetchSegment(i); err != nil {
				return
			}
		}
	}

	// Save a complete segment set to the disk cache.
	s.mu.Lock()
	allDownloaded := true
	segData := make([][]byte, len(s.segData))
	copy(segData, s.segData)
	for _, seg := range segData {
		if seg == nil {
			allDownloaded = false
			break
		}
	}
	s.mu.Unlock()

	if !allDownloaded || s.cache == nil {
		return
	}

	var fullAudio []byte
	for _, seg := range segData {
		fullAudio = append(fullAudio, seg...)
	}
	if len(fullAudio) > 0 {
		s.cache.Save(s.info.VideoID, s.info, fullAudio)
	}
}

func (s *hlsStream) SampleRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sampleRate
}

func (s *hlsStream) Channels() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels
}

func (s *hlsStream) Len() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalFrames
}

func (s *hlsStream) Read(buf []float32) (int, error) {
	s.decMu.Lock()
	dec := s.decoder
	if dec == nil {
		s.decMu.Unlock()
		return 0, io.EOF
	}

	neededBytes := len(buf) * 2 // 16-bit PCM = 2 bytes per sample
	pcmRaw := make([]byte, neededBytes)
	nBytes, err := dec.Read(pcmRaw)
	s.decMu.Unlock()
	if nBytes <= 0 && err != nil {
		return 0, err
	}

	samplesRead := nBytes / 2
	for i := 0; i < samplesRead; i++ {
		val := int16(binary.LittleEndian.Uint16(pcmRaw[i*2 : i*2+2]))
		f := float32(val) / 32768.0
		buf[i] = f
	}

	s.mu.Lock()
	s.updateSpectrumLocked(buf[:samplesRead])
	s.curFrame += int64(samplesRead) / int64(max(1, s.channels))
	s.mu.Unlock()

	return samplesRead, err
}

func (s *hlsStream) updateSpectrumLocked(samples []float32) {
	if len(samples) == 0 {
		return
	}
	bands := len(s.spectrum)
	chunkSize := max(1, len(samples)/bands)
	for b := range bands {
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
	s.decMu.Lock()
	defer s.decMu.Unlock()

	s.mu.Lock()
	sr := s.sampleRate
	channels := s.channels
	total := s.totalFrames
	cacheHit := s.cacheHit
	cacheData := s.cacheData
	segments := slices.Clone(s.segments)
	s.mu.Unlock()

	if frame < 0 {
		frame = 0
	}
	if total > 0 && frame > total {
		frame = total
	}

	if cacheHit {
		return s.seekCached(frame, cacheData, channels)
	}
	return s.seekSegments(frame, sr, segments)
}

// seekCached rebuilds a decoder over the cached audio and discards decoded
// frames before the target. The caller holds decMu.
func (s *hlsStream) seekCached(frame int64, data []byte, channels int) error {
	dec, err := aacpcm.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return err
	}
	skipBytes := frame * int64(max(1, channels)) * 2
	if skipBytes > 0 {
		if _, err := io.CopyN(io.Discard, dec, skipBytes); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	s.decoder = dec
	s.decReader = nil
	s.mu.Lock()
	s.curFrame = frame
	s.mu.Unlock()
	return nil
}

// seekSegments restarts the decoder at a segment boundary. The caller holds
// decMu.
func (s *hlsStream) seekSegments(frame int64, sampleRate int, segments []ytmusic.StreamSegment) error {
	if len(segments) == 0 {
		return errors.New("hls stream has no segments")
	}
	if sampleRate <= 0 {
		sampleRate = 44100
	}

	targetMs := frame * 1000 / int64(sampleRate)
	targetSeg := len(segments) - 1
	var accumulatedMs int64
	for i, seg := range segments {
		if accumulatedMs+seg.DurationMs > targetMs {
			targetSeg = i
			break
		}
		accumulatedMs += seg.DurationMs
	}

	sr := &segmentReader{s: s, segIdx: targetSeg}
	dec, err := aacpcm.NewDecoder(sr)
	if err != nil {
		return err
	}
	s.decReader = sr
	s.decoder = dec
	s.mu.Lock()
	s.curFrame = frame
	s.mu.Unlock()
	return nil
}

func (s *hlsStream) Close() error {
	s.closeOnce.Do(s.cancel)
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
		if err := sr.s.ctx.Err(); err != nil {
			return 0, err
		}

		sr.s.mu.Lock()
		if sr.segIdx >= len(sr.s.segments) {
			sr.s.mu.Unlock()
			return 0, io.EOF
		}
		data := sr.s.segData[sr.segIdx]
		segErr := sr.s.segErr[sr.segIdx]
		sr.s.mu.Unlock()

		if segErr != nil {
			return 0, segErr
		}

		if data == nil {
			// Wait for an in-flight fetch of this segment.
			if err := sr.s.fetchSegment(sr.segIdx); err != nil {
				return 0, err
			}
			sr.s.mu.Lock()
			data = sr.s.segData[sr.segIdx]
			segErr = sr.s.segErr[sr.segIdx]
			sr.s.mu.Unlock()
			if segErr != nil {
				return 0, segErr
			}
			if data == nil {
				select {
				case <-sr.s.ctx.Done():
					return 0, sr.s.ctx.Err()
				case <-time.After(5 * time.Millisecond):
					continue
				}
			}
		}

		if sr.offset < len(data) {
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
	closer      io.Closer
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
	return newMP4Stream(f, f)
}

func openSabrStream(sess *ytmusic.SabrSession) (*mp4Stream, error) {
	return newMP4Stream(sess, sess)
}

func newMP4Stream(r io.ReadSeeker, closer io.Closer) (*mp4Stream, error) {
	dec, info, err := aacm4a.NewSeekableDecoder(r)
	if err != nil {
		closer.Close()
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
		closer:      closer,
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
	if s.closer != nil {
		err := s.closer.Close()
		s.closer = nil
		return err
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
	total  int64
	done   bool
	closed bool
	err    error
	cancel context.CancelFunc
}

// close stops the download and releases its file, removing the partial file
// when asked. A finished download keeps its cached file.
func (d *download) close(remove bool) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
	d.mu.Lock()
	for !d.done {
		d.cond.Wait()
	}
	f := d.file
	d.file = nil
	d.mu.Unlock()
	if f != nil {
		_ = f.Close()
	}
	if remove {
		_ = os.Remove(d.part)
	}
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
		total:  contentLength,
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
		if d.total > 0 {
			newPos = d.total + offset
			break
		}
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
			dl.close(true)
			return nil, err
		}
		dl.close(false)
		return openMP4Stream(cachePath)
	}

	// Wait for the first 160 KB (contains ftyp and moov box at offset 24)
	dl.mu.Lock()
	for dl.n < 160*1024 && !dl.done {
		dl.cond.Wait()
	}
	if dl.err != nil {
		err := dl.err
		dl.mu.Unlock()
		dl.close(true)
		return nil, err
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
			dl.close(true)
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
