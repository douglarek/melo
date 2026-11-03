package ytmusic

import (
	"bytes"
	"encoding/base64"
	"testing"
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
