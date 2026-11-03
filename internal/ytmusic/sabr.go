package ytmusic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SABR (server-side adaptive bitrate) over UMP, as the VISIONOS client
// serves it: a POST of a protobuf VideoPlaybackAbrRequest and a stream of
// framed parts back. The field numbers follow the public protocol
// descriptions the community clients use.

type sabrClientInfo struct {
	clientName    string
	clientID      int
	clientVersion string
	userAgent     string
	osName        string
	osVersion     string
	deviceMake    string
	deviceModel   string
	androidSDK    int
}

var visionOSClient = sabrClientInfo{
	clientName:    "VISIONOS",
	clientID:      101,
	clientVersion: "1.02",
	userAgent:     "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
	osName:        "visionOS",
	osVersion:     "26.5.23O471",
	deviceMake:    "Apple",
	deviceModel:   "RealityDevice17,1",
}

// SabrFormat is one audio format of a SABR session.
type SabrFormat struct {
	Itag             int
	LastModified     int64
	ContentLength    int64
	ApproxDurationMs int64
	SampleRate       int
	Channels         int
	Bitrate          int64
}

// SabrStream carries what a SABR download needs.
type SabrStream struct {
	VideoID         string
	VisitorData     string
	URL             string
	UstreamerConfig []byte
	Format          SabrFormat
}

// sabrParts are the UMP part types this client understands.
const (
	umpMediaHeader = 20
	umpMedia       = 21
	umpNext        = 35
	umpFormatInit  = 42
	umpRedirect    = 43
	umpSabrError   = 44
	umpProtection  = 58
)

type umpPart struct {
	typ     int
	payload []byte
}

type sabrMediaHeader struct {
	headerID int
	seq      int
	init     bool
	content  int64
	startMs  int64
	durMs    int64
	offset   int64
}

// protoBuf is a minimal protobuf wire writer.
type protoBuf struct{ bytes.Buffer }

func (p *protoBuf) varint(v uint64) {
	for v >= 0x80 {
		p.WriteByte(byte(v) | 0x80)
		v >>= 7
	}
	p.WriteByte(byte(v))
}

func (p *protoBuf) tag(field, wire int) { p.varint(uint64(field<<3 | wire)) }

func (p *protoBuf) uint(field int, v uint64) {
	p.tag(field, 0)
	p.varint(v)
}

func (p *protoBuf) bytes(field int, b []byte) {
	p.tag(field, 2)
	p.varint(uint64(len(b)))
	p.Write(b)
}

func (p *protoBuf) str(field int, s string) { p.bytes(field, []byte(s)) }

type protoValue struct {
	varint uint64
	bytes  []byte
}

func protoParse(b []byte) (map[int][]protoValue, error) {
	out := map[int][]protoValue{}
	for pos := 0; pos < len(b); {
		tag, n := readVarint(b[pos:])
		if n == 0 {
			return nil, errors.New("sabr: bad protobuf tag")
		}
		pos += n
		field := int(tag >> 3)
		switch tag & 7 {
		case 0:
			v, n := readVarint(b[pos:])
			if n == 0 {
				return nil, errors.New("sabr: bad protobuf varint")
			}
			pos += n
			out[field] = append(out[field], protoValue{varint: v})
		case 2:
			ln, n := readVarint(b[pos:])
			if n == 0 || pos+n+int(ln) > len(b) {
				return nil, errors.New("sabr: bad protobuf bytes")
			}
			pos += n
			out[field] = append(out[field], protoValue{bytes: b[pos : pos+int(ln)]})
			pos += int(ln)
		case 1:
			pos += 8
		case 5:
			pos += 4
		default:
			return nil, fmt.Errorf("sabr: unsupported protobuf wire type %d", tag&7)
		}
	}
	return out, nil
}

func readVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// readUMPVarint reads UMP's length varint, whose leading bits carry the
// width (unlike protobuf's LEB128).
func readUMPVarint(b []byte, pos int) (v, size int, ok bool) {
	if pos >= len(b) {
		return 0, 0, false
	}
	b0 := b[pos]
	size = 1
	switch {
	case b0 >= 240:
		size = 5
	case b0 >= 224:
		size = 4
	case b0 >= 192:
		size = 3
	case b0 >= 128:
		size = 2
	}
	if pos+size > len(b) {
		return 0, 0, false
	}
	switch size {
	case 1:
		v = int(b0)
	case 2:
		v = int(b0&0x3f) + int(b[pos+1])*64
	case 3:
		v = int(b0&0x1f) + int(b[pos+1])*32 + int(b[pos+2])*8192
	case 4:
		v = int(b0&0x0f) + int(b[pos+1])*16 + int(b[pos+2])*4096 + int(b[pos+3])*1048576
	case 5:
		v = int(b[pos+1]) + int(b[pos+2])*256 + int(b[pos+3])*65536 + int(b[pos+4])*16777216
	}
	return v, size, true
}

func parseUMP(b []byte) ([]umpPart, error) {
	var parts []umpPart
	for pos := 0; pos < len(b); {
		typ, n, ok := readUMPVarint(b, pos)
		if !ok {
			return nil, errors.New("sabr: truncated UMP header")
		}
		pos += n
		size, n, ok := readUMPVarint(b, pos)
		if !ok || pos+n+size > len(b) {
			return nil, errors.New("sabr: truncated UMP payload")
		}
		pos += n
		parts = append(parts, umpPart{typ: typ, payload: b[pos : pos+size]})
		pos += size
	}
	return parts, nil
}

func decodeBase64Any(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func decodePoToken(token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("sabr: empty po token")
	}
	return decodeBase64Any(token)
}

// freshVisitorData asks the music web client for a visitor id; a fresh one
// is what the SABR clients accept.
func (c *Client) freshVisitorData(ctx context.Context, videoID string) (string, error) {
	data, err := c.post(ctx, "next", map[string]any{"videoId": videoID}, nil)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return "", err
	}
	rc, _ := root["responseContext"].(map[string]any)
	visitor, _ := rc["visitorData"].(string)
	if visitor == "" {
		return "", errors.New("sabr: no visitorData in response")
	}
	return visitor, nil
}

func (c *Client) visionOSPlayer(ctx context.Context, videoID, visitor string) (map[string]any, error) {
	c.mu.RLock()
	lang, region := c.language, c.region
	c.mu.RUnlock()
	payload := map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName":    visionOSClient.clientName,
			"clientVersion": visionOSClient.clientVersion,
			"hl":            lang,
			"gl":            region,
			"visitorData":   visitor,
			"osName":        visionOSClient.osName,
			"osVersion":     visionOSClient.osVersion,
			"deviceMake":    visionOSClient.deviceMake,
			"deviceModel":   visionOSClient.deviceModel,
		}},
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"player?prettyPrint=false", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", visionOSClient.userAgent)
	req.Header.Set("X-YouTube-Client-Name", strconv.Itoa(visionOSClient.clientID))
	req.Header.Set("X-YouTube-Client-Version", visionOSClient.clientVersion)
	req.Header.Set("X-Goog-Visitor-Id", visitor)
	req.Header.Set("X-Origin", "https://music.youtube.com")
	req.Header.Set("Referer", "https://music.youtube.com/")
	c.mu.RLock()
	cookie := c.cookie
	c.mu.RUnlock()
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sabr player: HTTP %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func pickSabrAudio(sd map[string]any) (SabrFormat, bool) {
	items, _ := sd["adaptiveFormats"].([]any)
	var best SabrFormat
	found := false
	for _, raw := range items {
		f, _ := raw.(map[string]any)
		mime, _ := f["mimeType"].(string)
		if !strings.HasPrefix(mime, "audio/mp4") {
			continue
		}
		itag, _ := f["itag"].(float64)
		if int(itag) != 140 {
			continue
		}
		best.Itag = int(itag)
		if lm, ok := f["lastModified"].(string); ok {
			best.LastModified, _ = strconv.ParseInt(lm, 10, 64)
		}
		if cl, ok := f["contentLength"].(string); ok {
			best.ContentLength, _ = strconv.ParseInt(cl, 10, 64)
		}
		if d, ok := f["approxDurationMs"].(string); ok {
			best.ApproxDurationMs, _ = strconv.ParseInt(d, 10, 64)
		}
		if sr, ok := f["audioSampleRate"].(string); ok {
			best.SampleRate, _ = strconv.Atoi(sr)
		}
		if ch, ok := f["audioChannels"].(float64); ok {
			best.Channels = int(ch)
		}
		if br, ok := f["bitrate"].(float64); ok {
			best.Bitrate = int64(br)
		}
		found = true
	}
	if !found {
		return SabrFormat{}, false
	}
	if best.SampleRate == 0 {
		best.SampleRate = 44100
	}
	if best.Channels == 0 {
		best.Channels = 2
	}
	return best, true
}

func (c *Client) resolveSabr(ctx context.Context, videoID, visitor string) (*SabrStream, error) {
	resp, err := c.visionOSPlayer(ctx, videoID, visitor)
	if err != nil {
		return nil, err
	}
	ps, _ := resp["playabilityStatus"].(map[string]any)
	if status, _ := ps["status"].(string); status != "OK" {
		return nil, fmt.Errorf("sabr player: %s", status)
	}
	sd, _ := resp["streamingData"].(map[string]any)
	if sd == nil {
		return nil, errors.New("sabr player: no streamingData")
	}
	sabrURL, _ := sd["serverAbrStreamingUrl"].(string)
	if sabrURL == "" {
		return nil, errors.New("sabr player: no serverAbrStreamingUrl")
	}
	format, ok := pickSabrAudio(sd)
	if !ok {
		return nil, errors.New("sabr player: no AAC audio format")
	}
	ust := ""
	if pc, ok := resp["playerConfig"].(map[string]any); ok {
		if mc, ok := pc["mediaCommonConfig"].(map[string]any); ok {
			if mu, ok := mc["mediaUstreamerRequestConfig"].(map[string]any); ok {
				ust, _ = mu["videoPlaybackUstreamerConfig"].(string)
			}
		}
	}
	if ust == "" {
		return nil, errors.New("sabr player: no ustreamer config")
	}
	ustBytes, err := decodeBase64Any(ust)
	if err != nil {
		return nil, err
	}
	return &SabrStream{
		VideoID:         videoID,
		VisitorData:     visitor,
		URL:             sabrURL,
		UstreamerConfig: ustBytes,
		Format:          format,
	}, nil
}

func (c *Client) sabrRequest(s *SabrStream, pot []byte, cookie []byte, playerTimeMs, bufEndMs int64, lastSeq, endSeg int) []byte {
	var req protoBuf
	var cas protoBuf
	if playerTimeMs > 0 {
		cas.uint(28, uint64(playerTimeMs))
	}
	cas.uint(40, 1)
	req.bytes(1, cas.Bytes())

	var fid protoBuf
	fid.uint(1, uint64(s.Format.Itag))
	if s.Format.LastModified > 0 {
		fid.uint(2, uint64(s.Format.LastModified))
	}
	if lastSeq > 0 {
		req.bytes(2, fid.Bytes())
		var br protoBuf
		br.bytes(1, fid.Bytes())
		br.uint(2, 0)
		br.uint(3, uint64(bufEndMs))
		br.uint(4, 1)
		br.uint(5, uint64(lastSeq))
		req.bytes(3, br.Bytes())
		req.uint(4, uint64(playerTimeMs))
	}
	req.bytes(5, s.UstreamerConfig)
	req.bytes(16, fid.Bytes())

	var ci protoBuf
	if visionOSClient.deviceMake != "" {
		ci.str(12, visionOSClient.deviceMake)
	}
	if visionOSClient.deviceModel != "" {
		ci.str(13, visionOSClient.deviceModel)
	}
	ci.uint(16, uint64(visionOSClient.clientID))
	ci.str(17, visionOSClient.clientVersion)
	if visionOSClient.osName != "" {
		ci.str(18, visionOSClient.osName)
	}
	if visionOSClient.osVersion != "" {
		ci.str(19, visionOSClient.osVersion)
	}
	var sc protoBuf
	sc.bytes(1, ci.Bytes())
	if len(pot) > 0 {
		sc.bytes(2, pot)
	}
	if len(cookie) > 0 {
		sc.bytes(3, cookie)
	}
	req.bytes(19, sc.Bytes())
	return req.Bytes()
}

// SabrSession is a SABR download in progress, read as a growing file: a
// reader waits for the bytes it needs while the loop fills them in.
type SabrSession struct {
	mu        sync.Mutex
	cond      *sync.Cond
	file      *os.File
	path      string
	total     int64
	pos       int64
	covered   []sabrInterval
	index     []sabrSegment
	initOff   int64
	initLen   int64
	ready     chan struct{}
	readyOnce sync.Once
	doneCh    chan struct{}
	done      bool
	err       error
	cancel    context.CancelFunc
	closed    bool
}

type sabrInterval struct{ start, end int64 }

type sabrSegment struct {
	startMs, durMs, offset, length int64
}

func newSabrSession(path string, total int64) (*SabrSession, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(total); err != nil {
		f.Close()
		return nil, err
	}
	s := &SabrSession{file: f, path: path, total: total, ready: make(chan struct{}), doneCh: make(chan struct{})}
	s.cond = sync.NewCond(&s.mu)
	return s, nil
}

func (s *SabrSession) signalReady() { s.readyOnce.Do(func() { close(s.ready) }) }

func (s *SabrSession) mark(off, n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.covered = append(s.covered, sabrInterval{start: off, end: off + n})
	sort.Slice(s.covered, func(i, j int) bool { return s.covered[i].start < s.covered[j].start })
	merged := s.covered[:0]
	for _, iv := range s.covered {
		if len(merged) == 0 || iv.start > merged[len(merged)-1].end {
			merged = append(merged, iv)
			continue
		}
		if iv.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = iv.end
		}
	}
	s.covered = merged
	s.cond.Broadcast()
}

func (s *SabrSession) coveredLocked(off, n int64) bool {
	end := off + n
	for _, iv := range s.covered {
		if iv.start <= off && iv.end >= end {
			return true
		}
		if iv.start > off {
			return false
		}
	}
	return false
}

func (s *SabrSession) wait(off, n int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.err != nil {
			return s.err
		}
		if s.coveredLocked(off, n) {
			return nil
		}
		if s.done {
			return io.ErrUnexpectedEOF
		}
		s.cond.Wait()
	}
}

func (s *SabrSession) writeAt(p []byte, off int64) error {
	if _, err := s.file.WriteAt(p, off); err != nil {
		return err
	}
	s.mark(off, int64(len(p)))
	return nil
}

func (s *SabrSession) addSegment(startMs, durMs, offset, length int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range s.index {
		if seg.offset == offset {
			return
		}
	}
	s.index = append(s.index, sabrSegment{startMs: startMs, durMs: durMs, offset: offset, length: length})
	sort.Slice(s.index, func(i, j int) bool { return s.index[i].startMs < s.index[j].startMs })
}

func (s *SabrSession) setInit(offset, length int64) {
	if length <= 0 {
		return
	}
	s.mu.Lock()
	s.initOff, s.initLen = offset, length
	s.mu.Unlock()
}

// SeekPoint returns the segment covering ms.
func (s *SabrSession) SeekPoint(ms int64) (offset, startMs int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.index) == 0 {
		return 0, 0, false
	}
	seg := s.index[0]
	for _, candidate := range s.index {
		if candidate.startMs > ms {
			break
		}
		seg = candidate
	}
	return seg.offset, seg.startMs, true
}

func (s *SabrSession) InitRange() (offset, length int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initOff, s.initLen
}

func (s *SabrSession) Total() int64 { return s.total }

// ReadAt waits for the requested range to arrive, then reads it.
func (s *SabrSession) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= s.total {
		return 0, io.EOF
	}
	n := int64(len(p))
	if off+n > s.total {
		n = s.total - off
	}
	if err := s.wait(off, n); err != nil {
		return 0, err
	}
	return s.file.ReadAt(p[:n], off)
}

func (s *SabrSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	pos := s.pos
	s.mu.Unlock()
	n, err := s.ReadAt(p, pos)
	s.mu.Lock()
	s.pos += int64(n)
	s.mu.Unlock()
	return n, err
}

func (s *SabrSession) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = s.pos + offset
	case io.SeekEnd:
		pos = s.total + offset
	default:
		return 0, errors.New("sabr: invalid seek")
	}
	if pos < 0 {
		return 0, errors.New("sabr: negative seek")
	}
	s.pos = pos
	return pos, nil
}

func (s *SabrSession) finish(err error) {
	s.mu.Lock()
	s.done = true
	s.err = err
	s.mu.Unlock()
	s.cond.Broadcast()
	s.signalReady()
	close(s.doneCh)
}

// Wait blocks until the download finishes or ctx is done.
func (s *SabrSession) Wait(ctx context.Context) error {
	select {
	case <-s.doneCh:
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.done {
			return errors.New("sabr: session closed")
		}
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the download and drops a file it did not finish.
func (s *SabrSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	failed := s.err != nil
	if s.err == nil {
		s.err = errors.New("sabr: session closed")
	}
	done := s.done
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.cond.Broadcast()
	var err error
	if s.file != nil {
		err = s.file.Close()
	}
	if !done || failed {
		_ = os.Remove(s.path)
		_ = os.Remove(s.path + ".ok")
	}
	return err
}

// startSabrSession starts draining and returns once the first segment is in.
func (c *Client) startSabrSession(ctx context.Context, stream *SabrStream, token, dest string) (*SabrSession, error) {
	if stream.Format.ContentLength <= 0 {
		return nil, errors.New("sabr: unknown content length")
	}
	sess, err := newSabrSession(dest, stream.Format.ContentLength)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	sess.cancel = cancel
	size := strconv.FormatInt(stream.Format.ContentLength, 10)
	go func() {
		err := c.runSabr(runCtx, stream, token, sess)
		if err == nil {
			if werr := os.WriteFile(dest+".ok", []byte(size), 0o644); werr != nil {
				err = werr
			} else {
				pruneSabrDir(filepath.Dir(dest), filepath.Base(dest))
			}
		}
		sess.finish(err)
	}()
	select {
	case <-sess.ready:
		sess.mu.Lock()
		err := sess.err
		sess.mu.Unlock()
		if err != nil {
			sess.Close()
			return nil, err
		}
		return sess, nil
	case <-runCtx.Done():
		sess.Close()
		return nil, runCtx.Err()
	case <-time.After(20 * time.Second):
		sess.Close()
		return nil, errors.New("sabr: first segment timed out")
	}
}

// DownloadSabr drains a SABR session into dest, for callers that want the
// whole file first.
func (c *Client) DownloadSabr(ctx context.Context, s *SabrStream, token string, dest string) error {
	sess, err := c.startSabrSession(ctx, s, token, dest)
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.mu.Lock()
	for !sess.done {
		sess.cond.Wait()
	}
	err = sess.err
	sess.mu.Unlock()
	return err
}

func (c *Client) runSabr(ctx context.Context, s *SabrStream, token string, sess *SabrSession) error {
	pot, err := decodePoToken(token)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	segs := map[int]int64{}
	var initBytes int64
	playerTimeMs, bufEndMs := int64(0), int64(0)
	lastSeq, endSeg := 0, 0
	var playbackCookie []byte
	rawURL := s.URL
	dry := 0
	for iter := 0; iter < 400 && dry < 6; iter++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		body := c.sabrRequest(s, pot, playbackCookie, playerTimeMs, bufEndMs, lastSeq, endSeg)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("User-Agent", visionOSClient.userAgent)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("sabr: HTTP %d", resp.StatusCode)
		}
		parts, err := parseUMP(data)
		if err != nil {
			return err
		}
		headers := map[int]*sabrMediaHeader{}
		redirect := ""
		newSeg := false
		for _, p := range parts {
			switch p.typ {
			case umpMediaHeader:
				fields, err := protoParse(p.payload)
				if err != nil {
					return err
				}
				h := &sabrMediaHeader{}
				if v := fields[1]; len(v) > 0 {
					h.headerID = int(v[0].varint)
				}
				if v := fields[9]; len(v) > 0 {
					h.seq = int(v[0].varint)
				}
				if v := fields[8]; len(v) > 0 {
					h.init = v[0].varint != 0
				}
				if v := fields[14]; len(v) > 0 {
					h.content = int64(v[0].varint)
				}
				if v := fields[6]; len(v) > 0 {
					h.offset = int64(v[0].varint)
				}
				if tr := fields[15]; len(tr) > 0 {
					tf, err := protoParse(tr[0].bytes)
					if err == nil {
						ts := int64(1000)
						if v := tf[3]; len(v) > 0 {
							ts = int64(v[0].varint)
						}
						if v := tf[1]; len(v) > 0 {
							h.startMs = int64(v[0].varint) * 1000 / ts
						}
						if v := tf[2]; len(v) > 0 {
							h.durMs = int64(v[0].varint) * 1000 / ts
						}
					}
				}
				if !h.init {
					sess.addSegment(h.startMs, h.durMs, h.offset, h.content)
				}
				headers[h.headerID] = h
			case umpMedia:
				id, n, ok := readUMPVarint(p.payload, 0)
				if !ok {
					continue
				}
				h := headers[id]
				if h == nil {
					continue
				}
				chunk := p.payload[n:]
				if h.init {
					if initBytes == 0 {
						if err := sess.writeAt(chunk, h.offset); err != nil {
							return err
						}
						sess.setInit(h.offset, int64(len(chunk)))
						initBytes = int64(len(chunk))
					}
					continue
				}
				if _, ok := segs[h.seq]; ok {
					continue
				}
				if err := sess.writeAt(chunk, h.offset); err != nil {
					return err
				}
				h.offset += int64(len(chunk))
			case umpFormatInit:
				if fields, err := protoParse(p.payload); err == nil {
					if v := fields[4]; len(v) > 0 {
						endSeg = int(v[0].varint)
					}
				}
			case umpNext:
				if fields, err := protoParse(p.payload); err == nil {
					if v := fields[7]; len(v) > 0 {
						playbackCookie = v[0].bytes
					}
				}
			case umpRedirect:
				if _, n, ok := readUMPVarint(p.payload, 1); ok {
					redirect = string(p.payload[1+n:])
				}
			case umpProtection:
				if fields, err := protoParse(p.payload); err == nil {
					if v := fields[1]; len(v) > 0 && v[0].varint >= 3 {
						return errors.New("sabr: stream needs attestation")
					}
				}
			case umpSabrError:
				return fmt.Errorf("sabr: server error: %x", p.payload)
			}
		}
		for _, h := range headers {
			if h.init {
				if initBytes == 0 {
					initBytes = h.content
				}
				continue
			}
			if _, ok := segs[h.seq]; !ok {
				segs[h.seq] = h.content
				newSeg = true
			}
			if h.seq > lastSeq {
				lastSeq = h.seq
			}
			if end := h.startMs + h.durMs; end > bufEndMs {
				bufEndMs = end
			}
		}
		if _, ok := segs[1]; ok {
			sess.signalReady()
		}
		playerTimeMs = bufEndMs
		if redirect != "" && !newSeg {
			rawURL = redirect
			continue
		}
		if newSeg {
			dry = 0
		} else {
			dry++
		}
		if endSeg > 0 && lastSeq >= endSeg {
			break
		}
	}
	if endSeg == 0 || lastSeq < endSeg {
		return fmt.Errorf("sabr: incomplete (%d/%d segments)", lastSeq, endSeg)
	}
	var sum int64
	for _, n := range segs {
		sum += n
	}
	if initBytes+sum != s.Format.ContentLength {
		return fmt.Errorf("sabr: size mismatch init=%d segments=%d want=%d", initBytes, sum, s.Format.ContentLength)
	}
	return nil
}

// POTProvider mints PO tokens bound to a visitor data or a video id.
type POTProvider interface {
	Token(ctx context.Context, binding string) (string, error)
}

// SetSabr enables the SABR path: tokens mints per visitor and dir holds the
// drained files.
func (c *Client) SetSabr(tokens POTProvider, dir string) {
	c.mu.Lock()
	c.sabrTokens = tokens
	c.sabrDir = dir
	c.mu.Unlock()
}

// pruneSabrDir keeps the drained files under limit bytes, oldest first,
// never removing keep.
func pruneSabrDir(dir, keep string) {
	const limit = int64(512 << 20)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || e.Name() == keep {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{path: filepath.Join(dir, e.Name()), size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= limit {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}

func (c *Client) sabrInfo(videoID, dest string, s *SabrStream) *StreamInfo {
	return &StreamInfo{
		VideoID:       videoID,
		Format:        "file",
		LocalPath:     dest,
		SampleRate:    s.Format.SampleRate,
		Channels:      s.Format.Channels,
		DurationMs:    s.Format.ApproxDurationMs,
		ContentLength: s.Format.ContentLength,
	}
}

func (c *Client) resolveSabrFile(ctx context.Context, videoID string) (*StreamInfo, error) {
	c.mu.RLock()
	tokens := c.sabrTokens
	dir := c.sabrDir
	c.mu.RUnlock()
	if tokens == nil || dir == "" {
		return nil, errors.New("sabr: not configured")
	}
	visitor, err := c.freshVisitorData(ctx, videoID)
	if err != nil {
		return nil, err
	}
	s, err := c.resolveSabr(ctx, videoID, visitor)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(dir, videoID+".m4a")
	marker := dest + ".ok"
	size := strconv.FormatInt(s.Format.ContentLength, 10)
	if st, err := os.Stat(dest); err == nil && st.Size() == s.Format.ContentLength {
		if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == size {
			return c.sabrInfo(videoID, dest, s), nil
		}
	}
	token, err := tokens.Token(ctx, visitor)
	if err != nil {
		return nil, err
	}
	sess, err := c.startSabrSession(ctx, s, token, dest)
	if err != nil {
		return nil, err
	}
	info := c.sabrInfo(videoID, dest, s)
	info.Sabr = sess
	return info, nil
}
