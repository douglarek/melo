package player

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func fullBox(typ string, flags uint32, body []byte) []byte {
	payload := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(payload, flags)
	copy(payload[4:], body)
	return box(typ, payload)
}

func TestFindASC(t *testing.T) {
	esds, _ := hex.DecodeString("000000000327000100041f40150000000000000000000000051012100000000000000000000000000000060102")
	asc := findASC(esds)
	if len(asc) < 2 || asc[0] != 0x12 || asc[1] != 0x10 {
		t.Fatalf("asc = %x", asc)
	}
}

func TestParseMoof(t *testing.T) {
	tfhd := fullBox("tfhd", 0x18, []byte{0, 0, 0, 1, 0, 0, 4, 0, 0, 0x04, 0})
	// tfhd body: track_ID=1, default_duration=1024, default_size=4
	tfhdBody := make([]byte, 12)
	binary.BigEndian.PutUint32(tfhdBody[0:], 1)
	binary.BigEndian.PutUint32(tfhdBody[4:], 1024)
	binary.BigEndian.PutUint32(tfhdBody[8:], 4)
	tfhd = fullBox("tfhd", 0x18, tfhdBody)
	tfdt := fullBox("tfdt", 0, []byte{0, 0, 0, 0})
	trunBody := make([]byte, 12)
	binary.BigEndian.PutUint32(trunBody[0:], 2) // sample count
	binary.BigEndian.PutUint32(trunBody[4:], 0) // data offset, relative to the moof
	trun := fullBox("trun", 0x1, trunBody)
	traf := box("traf", append(append(tfhd, tfdt...), trun...))
	moof := box("moof", traf)

	samples, err := parseMoof(moof, 100, 200, 44100)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d", len(samples))
	}
	if samples[0].offset != 100 || samples[1].offset != 104 {
		t.Fatalf("offsets = %d %d", samples[0].offset, samples[1].offset)
	}
	if samples[0].time != 0 || samples[1].time != 1024 {
		t.Fatalf("times = %d %d", samples[0].time, samples[1].time)
	}
	if samples[0].size != 4 {
		t.Fatalf("size = %d", samples[0].size)
	}
}
