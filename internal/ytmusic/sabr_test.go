package ytmusic

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// umpEncode writes a UMP varint, the leading bits carrying the width.
func umpEncode(v int) []byte {
	switch {
	case v < 128:
		return []byte{byte(v)}
	case v < 16384:
		return []byte{byte(v&0x3f) | 0x80, byte(v >> 6)}
	case v < 2097152:
		return []byte{byte(v&0x1f) | 0xc0, byte(v >> 5), byte(v >> 13)}
	default:
		panic("test value too large")
	}
}

func TestUMPParse(t *testing.T) {
	payload := bytes.Repeat([]byte{7}, 300)
	var body bytes.Buffer
	body.Write(umpEncode(umpMediaHeader))
	body.Write(umpEncode(5))
	body.Write([]byte{1, 2, 3, 4, 5})
	body.Write(umpEncode(umpMedia))
	body.Write(umpEncode(len(payload)))
	body.Write(payload)
	body.Write(umpEncode(umpFormatInit))
	body.Write(umpEncode(2))
	body.Write([]byte{9, 9})

	parts, err := parseUMP(body.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("parts = %d, want 3", len(parts))
	}
	if parts[0].typ != umpMediaHeader || !bytes.Equal(parts[0].payload, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("header part = %+v", parts[0])
	}
	if parts[1].typ != umpMedia || !bytes.Equal(parts[1].payload, payload) {
		t.Fatalf("media part len = %d", len(parts[1].payload))
	}
	if parts[2].typ != umpFormatInit || !bytes.Equal(parts[2].payload, []byte{9, 9}) {
		t.Fatalf("format init part = %+v", parts[2])
	}
}

func TestSabrRequestFields(t *testing.T) {
	c := &Client{}
	s := &SabrStream{
		Format:          SabrFormat{Itag: 140, LastModified: 123456789},
		UstreamerConfig: []byte{0xde, 0xad},
	}
	body := c.sabrRequest(s, []byte{1, 2, 3}, []byte{9, 8}, 5000, 9000, 3, 21)
	fields, err := protoParse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state, err := protoParse(fields[1][0].bytes)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if got := state[28][0].varint; got != 5000 {
		t.Fatalf("playerTimeMs = %d", got)
	}
	if got := state[40][0].varint; got != 1 {
		t.Fatalf("enabledTrackTypesBitfield = %d", got)
	}
	if !bytes.Equal(fields[5][0].bytes, []byte{0xde, 0xad}) {
		t.Fatalf("ustreamer config = %x", fields[5][0].bytes)
	}
	fmtField, err := protoParse(fields[16][0].bytes)
	if err != nil {
		t.Fatalf("format id: %v", err)
	}
	if got := fmtField[1][0].varint; got != 140 {
		t.Fatalf("itag = %d", got)
	}
	if got := fmtField[2][0].varint; got != 123456789 {
		t.Fatalf("lastModified = %d", got)
	}
	ctx, err := protoParse(fields[19][0].bytes)
	if err != nil {
		t.Fatalf("streamer context: %v", err)
	}
	if !bytes.Equal(ctx[2][0].bytes, []byte{1, 2, 3}) {
		t.Fatalf("po token = %x", ctx[2][0].bytes)
	}
	if !bytes.Equal(ctx[3][0].bytes, []byte{9, 8}) {
		t.Fatalf("playback cookie = %x", ctx[3][0].bytes)
	}
	if len(fields[2]) != 1 || len(fields[3]) != 1 || len(fields[4]) != 1 {
		t.Fatalf("selected/buffered/playerTime fields missing: %v", fields)
	}
}

func TestSabrRequestColdStart(t *testing.T) {
	c := &Client{}
	s := &SabrStream{Format: SabrFormat{Itag: 140}, UstreamerConfig: []byte{1}}
	fields, err := protoParse(c.sabrRequest(s, nil, nil, 0, 0, 0, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fields[3]) != 0 || len(fields[4]) != 0 || len(fields[2]) != 0 {
		t.Fatalf("cold start has buffered fields: %v", fields)
	}
	ctx, _ := protoParse(fields[19][0].bytes)
	if len(ctx[2]) != 0 {
		t.Fatalf("cold start has po token")
	}
}

func TestPickSabrAudio(t *testing.T) {
	sd := map[string]any{
		"adaptiveFormats": []any{
			map[string]any{"itag": float64(251), "mimeType": "audio/webm; codecs=\"opus\"", "contentLength": "100"},
			map[string]any{"itag": float64(140), "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "3252803", "lastModified": "42", "audioSampleRate": "44100", "audioChannels": float64(2), "approxDurationMs": "200863"},
			map[string]any{"itag": float64(141), "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "contentLength": "6000000"},
		},
	}
	f, ok := pickSabrAudio(sd)
	if !ok {
		t.Fatal("no audio picked")
	}
	if f.Itag != 140 || f.ContentLength != 3252803 || f.SampleRate != 44100 || f.Channels != 2 {
		t.Fatalf("picked %+v", f)
	}
}

func TestDecodeBase64Any(t *testing.T) {
	raw := []byte{0xfa, 0xfb, 0xfc, 0xfd}
	std := base64.StdEncoding.EncodeToString(raw)
	urlNoPad := base64.RawURLEncoding.EncodeToString(raw)
	for _, s := range []string{std, urlNoPad, urlNoPad + "=="} {
		got, err := decodeBase64Any(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("%q -> %x", s, got)
		}
	}
}

// countingTokens is a POTProvider that counts the tokens it minted.
type countingTokens struct {
	mu    sync.Mutex
	mints int
}

func (c *countingTokens) Token(ctx context.Context, binding string) (string, error) {
	c.mu.Lock()
	c.mints++
	c.mu.Unlock()
	return base64.StdEncoding.EncodeToString([]byte("token")), nil
}

func (c *countingTokens) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mints
}

// TestDrainedTrackNeedsNoCredential checks that a track whose download
// finished plays from its file, with no visitor asked for, no token minted and
// no request made.
func TestDrainedTrackNeedsNoCredential(t *testing.T) {
	dir := t.TempDir()
	const id = "drained"
	dest := filepath.Join(dir, id+".m4a")
	if err := os.WriteFile(dest, make([]byte, 32), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(dest+".ok", []byte("32"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	tokens := &countingTokens{}
	c := NewClient()
	c.SetSabr(tokens, dir)

	info, err := c.resolveSabrFile(t.Context(), id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if info.LocalPath != dest || info.ContentLength != 32 {
		t.Fatalf("info = %+v, want the file at %s", info, dest)
	}
	if got := tokens.count(); got != 0 {
		t.Fatalf("minted %d tokens for a drained track", got)
	}
}

// TestDrainedFileChecksTheMarker checks what makes a file drained.
func TestDrainedFileChecksTheMarker(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "track.m4a")
	if err := os.WriteFile(dest, make([]byte, 10), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := drainedFile("track", dest); ok {
		t.Fatal("a file with no marker played as drained")
	}
	if err := os.WriteFile(dest+".ok", []byte("11"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if _, ok := drainedFile("track", dest); ok {
		t.Fatal("a file shorter than its marker played as drained")
	}
	if err := os.WriteFile(dest+".ok", []byte("10"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if _, ok := drainedFile("track", dest); !ok {
		t.Fatal("a file its marker matches did not play as drained")
	}
}

// TestSabrRetrySaysWhatAFailureIsWorth checks which failures mint another
// token, which get one more try as they are, and which end the attempt.
func TestSabrRetrySaysWhatAFailureIsWorth(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"forbidden", &sabrStatusError{code: http.StatusForbidden}, sabrRetryCredential},
		{"bad request", &sabrStatusError{code: http.StatusBadRequest}, sabrRetryCredential},
		{"unauthorized", &sabrStatusError{code: http.StatusUnauthorized}, sabrRetryCredential},
		{"server error", &sabrStatusError{code: http.StatusBadGateway}, sabrRetryNo},
		{"attestation", errSabrAttestation, sabrRetryCredential},
		{"rejected", fmt.Errorf("%w: 0a0b", errSabrRejected), sabrRetryCredential},
		{"no stream", fmt.Errorf("%w: no streamingData", errSabrNoStream), sabrRetryCredential},
		{"playback refused", &sabrPlaybackError{status: "LOGIN_REQUIRED"}, sabrRetryCredential},
		{"unavailable", fmt.Errorf("sabr player: %w", ErrUnavailable), sabrRetryNo},
		{"cancelled", context.Canceled, sabrRetryNo},
		{"timed out", context.DeadlineExceeded, sabrRetryTransient},
		{"network", errors.New("dial tcp: no route to host"), sabrRetryTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sabrRetryFor(tc.err); got != tc.want {
				t.Fatalf("sabrRetryFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestSabrRefusalWantsAnotherToken checks that a 403 from the SABR endpoint,
// as a stale token gets, is a failure another mint may pass.
func TestSabrRefusalWantsAnotherToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{VideoID: "track", URL: srv.URL, Format: SabrFormat{ContentLength: 64}}
	token := base64.StdEncoding.EncodeToString([]byte("token"))
	_, err := c.startSabrSession(t.Context(), stream, token, filepath.Join(t.TempDir(), "track.m4a"))
	if err == nil {
		t.Fatal("a refused stream started")
	}
	if got := sabrRetryFor(err); got != sabrRetryCredential {
		t.Fatalf("sabrRetryFor(%v) = %d, want %d", err, got, sabrRetryCredential)
	}
}

// TestSabrCredentialServesManyTracks checks that a fresh pair is reused, and
// that only the pair a refusal used is dropped.
func TestSabrCredentialServesManyTracks(t *testing.T) {
	tokens := &countingTokens{}
	c := NewClient()
	c.SetSabr(tokens, t.TempDir())
	c.sabrCred = &sabrCredential{visitor: "visitor", token: "token", minted: time.Now(), gen: 7}

	for _, id := range []string{"first", "second"} {
		cred, err := c.sabrCredential(t.Context(), id)
		if err != nil {
			t.Fatalf("credential for %s: %v", id, err)
		}
		if cred.visitor != "visitor" || cred.token != "token" || cred.gen != 7 {
			t.Fatalf("credential for %s = %+v, want the one at hand", id, cred)
		}
	}
	if got := tokens.count(); got != 0 {
		t.Fatalf("minted %d tokens with a fresh pair at hand", got)
	}

	c.dropSabrCredential(6)
	if c.sabrCred == nil {
		t.Fatal("dropped a pair another attempt was using")
	}
	c.dropSabrCredential(7)
	if c.sabrCred != nil {
		t.Fatal("the pair a refusal used stayed")
	}
}
