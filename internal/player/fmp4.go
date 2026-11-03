package player

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var errFMP4 = errors.New("fmp4: malformed box")

// fmp4Init is what the init segment (ftyp + moov) says about the audio.
type fmp4Init struct {
	asc        []byte
	timescale  uint32
	sampleRate int
	channels   int
}

func walkBoxes(data []byte, start, end int, fn func(typ string, payload, boxEnd int) error) error {
	for pos := start; pos+8 <= end; {
		size := int(binary.BigEndian.Uint32(data[pos:]))
		typ := string(data[pos+4 : pos+8])
		hdr := 8
		if size == 1 {
			if pos+16 > end {
				return errFMP4
			}
			size = int(binary.BigEndian.Uint64(data[pos+8:]))
			hdr = 16
		} else if size == 0 {
			size = end - pos
		}
		if size < hdr || pos+size > end {
			return errFMP4
		}
		if err := fn(typ, pos+hdr, pos+size); err != nil {
			return err
		}
		pos += size
	}
	return nil
}

func parseInitSegment(data []byte) (*fmp4Init, error) {
	out := &fmp4Init{}
	var findTrack func([]byte) error
	findTrack = func(trak []byte) error {
		return walkBoxes(trak, 0, len(trak), func(typ string, payload, end int) error {
			switch typ {
			case "mdia":
				return walkBoxes(trak, payload, end, func(t2 string, p2, e2 int) error {
					switch t2 {
					case "mdhd":
						if e2-p2 < 20 {
							return errFMP4
						}
						version := trak[p2]
						if version == 1 {
							if e2-p2 < 32 {
								return errFMP4
							}
							out.timescale = binary.BigEndian.Uint32(trak[p2+20:])
						} else {
							out.timescale = binary.BigEndian.Uint32(trak[p2+12:])
						}
					case "minf":
						return walkBoxes(trak, p2, e2, func(t3 string, p3, e3 int) error {
							if t3 != "stbl" {
								return nil
							}
							return walkBoxes(trak, p3, e3, func(t4 string, p4, e4 int) error {
								if t4 != "stsd" {
									return nil
								}
								// version/flags + entry count
								return walkBoxes(trak, p4+8, e4, func(t5 string, p5, e5 int) error {
									if t5 != "mp4a" {
										return nil
									}
									if e5-p5 >= 28 {
										out.channels = int(binary.BigEndian.Uint16(trak[p5+16:]))
										out.sampleRate = int(binary.BigEndian.Uint16(trak[p5+24:]))
									}
									return walkBoxes(trak, p5+28, e5, func(t6 string, p6, e6 int) error {
										if t6 != "esds" {
											return nil
										}
										out.asc = findASC(trak[p6:e6])
										return nil
									})
								})
							})
						})
					}
					return nil
				})
			}
			return nil
		})
	}
	// find the first audio trak
	var hasAudio bool
	if err := walkBoxes(data, 0, len(data), func(typ string, payload, end int) error {
		if typ != "moov" {
			return nil
		}
		return walkBoxes(data, payload, end, func(t2 string, p2, e2 int) error {
			if t2 != "trak" || hasAudio {
				return nil
			}
			// check handler before committing
			isAudio := false
			_ = walkBoxes(data, p2, e2, func(t3 string, p3, e3 int) error {
				if t3 == "mdia" {
					_ = walkBoxes(data, p3, e3, func(t4 string, p4, e4 int) error {
						if t4 == "hdlr" && e4-p4 >= 12 && string(data[p4+8:p4+12]) == "soun" {
							isAudio = true
						}
						return nil
					})
				}
				return nil
			})
			if !isAudio {
				return nil
			}
			hasAudio = true
			return findTrack(data[p2:e2])
		})
	}); err != nil {
		return nil, err
	}
	if !hasAudio || len(out.asc) == 0 {
		return nil, errors.New("fmp4: no AAC track")
	}
	if out.timescale == 0 {
		out.timescale = 44100
	}
	if out.sampleRate == 0 {
		out.sampleRate = 44100
	}
	if out.channels == 0 {
		out.channels = 2
	}
	return out, nil
}

func descriptorLength(data []byte) (int, int) {
	var n, size int
	for i := 0; i < len(data) && i < 4; i++ {
		size = (size << 7) | int(data[i]&0x7f)
		n++
		if data[i]&0x80 == 0 {
			return size, n
		}
	}
	return 0, n
}

func findASC(data []byte) []byte {
	return scanDescriptors(data, 4) // skip the full box version and flags
}

func scanDescriptors(data []byte, pos int) []byte {
	for pos < len(data) {
		tag := data[pos]
		pos++
		size, n := descriptorLength(data[pos:])
		pos += n
		if size < 0 || pos+size > len(data) {
			return nil
		}
		body := data[pos : pos+size]
		pos += size
		switch tag {
		case 0x03:
			if asc := scanDescriptors(body, 3); asc != nil { // ES_ID and flags
				return asc
			}
		case 0x04:
			if asc := scanDescriptors(body, 13); asc != nil { // object type through average bitrate
				return asc
			}
		case 0x05:
			return append([]byte(nil), body...)
		}
	}
	return nil
}

type fmp4Sample struct {
	offset int64
	size   uint32
	time   int64
	dur    uint32
}

func parseMoof(data []byte, moofOff, mdatPayload int64, timescale uint32) ([]fmp4Sample, error) {
	var (
		baseTime       int64
		baseDataOffset int64
		hasBase        bool
		defDur         uint32
		defSize        uint32
		trunOff        int64
		hasTrun        bool
		count          uint32
		sizes          []uint32
		durs           []uint32
	)
	parseTraf := func(start, end int) error {
		return walkBoxes(data, start, end, func(t string, pp, ee int) error {
			body := data[pp:ee]
			switch t {
			case "tfhd":
				if len(body) < 8 {
					return errFMP4
				}
				flags := binary.BigEndian.Uint32(body[0:]) & 0xffffff
				pos := 8
				if flags&0x1 != 0 {
					if len(body) < pos+8 {
						return errFMP4
					}
					baseDataOffset = int64(binary.BigEndian.Uint64(body[pos:]))
					hasBase = true
					pos += 8
				}
				if flags&0x2 != 0 {
					pos += 4
				}
				if flags&0x8 != 0 && len(body) >= pos+4 {
					defDur = binary.BigEndian.Uint32(body[pos:])
					pos += 4
				}
				if flags&0x10 != 0 && len(body) >= pos+4 {
					defSize = binary.BigEndian.Uint32(body[pos:])
					pos += 4
				}
			case "tfdt":
				if len(body) < 8 {
					return errFMP4
				}
				if body[0] == 1 {
					if len(body) < 12 {
						return errFMP4
					}
					baseTime = int64(binary.BigEndian.Uint64(body[4:]))
				} else {
					baseTime = int64(binary.BigEndian.Uint32(body[4:]))
				}
			case "trun":
				if len(body) < 8 {
					return errFMP4
				}
				flags := binary.BigEndian.Uint32(body[0:]) & 0xffffff
				count = binary.BigEndian.Uint32(body[4:])
				pos := 8
				if flags&0x1 != 0 {
					if len(body) < pos+4 {
						return errFMP4
					}
					trunOff = int64(int32(binary.BigEndian.Uint32(body[pos:])))
					hasTrun = true
					pos += 4
				}
				if flags&0x4 != 0 {
					pos += 4
				}
				for i := uint32(0); i < count; i++ {
					var sz, du uint32
					if flags&0x100 != 0 {
						if len(body) < pos+4 {
							return errFMP4
						}
						du = binary.BigEndian.Uint32(body[pos:])
						pos += 4
					}
					if flags&0x200 != 0 {
						if len(body) < pos+4 {
							return errFMP4
						}
						sz = binary.BigEndian.Uint32(body[pos:])
						pos += 4
					}
					if flags&0x400 != 0 {
						pos += 4
					}
					if flags&0x800 != 0 {
						pos += 4
					}
					sizes = append(sizes, sz)
					durs = append(durs, du)
				}
			}
			return nil
		})
	}
	err := walkBoxes(data, 0, len(data), func(typ string, p, e int) error {
		switch typ {
		case "moof":
			return walkBoxes(data, p, e, func(t string, pp, ee int) error {
				if t == "traf" {
					return parseTraf(pp, ee)
				}
				return nil
			})
		case "traf":
			return parseTraf(p, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if count == 0 || len(sizes) == 0 {
		return nil, errors.New("fmp4: empty trun")
	}
	base := mdatPayload
	if hasBase {
		base = baseDataOffset
	} else if hasTrun {
		base = moofOff + trunOff
	}
	out := make([]fmp4Sample, 0, len(sizes))
	off := base
	at := baseTime
	for i := range sizes {
		sz := sizes[i]
		if sz == 0 {
			sz = defSize
		}
		du := durs[i]
		if du == 0 {
			du = defDur
		}
		out = append(out, fmp4Sample{offset: off, size: sz, time: at, dur: du})
		off += int64(sz)
		at += int64(du)
	}
	_ = timescale
	return out, nil
}

// fmp4Reader turns a growing fragmented MP4 into the length-prefixed AAC
// access-unit stream aacpcm.NewDecoder consumes.
type fmp4Reader struct {
	src        io.ReaderAt
	total      int64
	pos        int64
	init       *fmp4Init
	pending    []byte
	seekPoint  func(ms int64) (offset, startMs int64, ok bool)
	targetTime int64
}

func newFMP4Reader(src io.ReaderAt, total, initOff, initLen int64, seek func(ms int64) (int64, int64, bool)) (*fmp4Reader, error) {
	buf := make([]byte, initLen)
	if _, err := src.ReadAt(buf, initOff); err != nil {
		return nil, err
	}
	init, err := parseInitSegment(buf)
	if err != nil {
		return nil, err
	}
	return &fmp4Reader{
		src:       src,
		total:     total,
		pos:       initOff + initLen,
		init:      init,
		seekPoint: seek,
	}, nil
}

func (r *fmp4Reader) ASC() []byte { return r.init.asc }

func (r *fmp4Reader) readBoxHeader(pos int64) (typ string, size int64, hdr int64, err error) {
	buf := make([]byte, 16)
	n, err := r.src.ReadAt(buf, pos)
	if err != nil && n < 8 {
		return "", 0, 0, err
	}
	size = int64(binary.BigEndian.Uint32(buf[0:]))
	typ = string(buf[4:8])
	hdr = 8
	if size == 1 {
		if n < 16 {
			return "", 0, 0, errFMP4
		}
		size = int64(binary.BigEndian.Uint64(buf[8:16]))
		hdr = 16
	} else if size == 0 {
		size = r.total - pos
	}
	return typ, size, hdr, nil
}

func (r *fmp4Reader) fill() error {
	for len(r.pending) == 0 {
		typ, size, hdr, err := r.readBoxHeader(r.pos)
		if err != nil {
			return io.EOF
		}
		if size <= 0 {
			return io.EOF
		}
		switch typ {
		case "moof":
			moof := make([]byte, size-hdr)
			if _, err := r.src.ReadAt(moof, r.pos+hdr); err != nil {
				return err
			}
			mdatType, mdatSize, mdatHdr, err := r.readBoxHeader(r.pos + size)
			if err != nil || mdatType != "mdat" {
				r.pos += size
				continue
			}
			mdatPayload := r.pos + size + mdatHdr
			samples, err := parseMoof(moof, r.pos, mdatPayload, r.init.timescale)
			if err != nil {
				return err
			}
			for _, s := range samples {
				if r.targetTime > 0 && s.time+int64(s.dur) <= r.targetTime {
					continue
				}
				au := make([]byte, s.size)
				if _, err := r.src.ReadAt(au, s.offset); err != nil {
					return err
				}
				r.pending = binary.BigEndian.AppendUint16(r.pending, uint16(len(au)))
				r.pending = append(r.pending, au...)
			}
			r.pos += size + mdatSize
		default:
			r.pos += size
		}
	}
	return nil
}

func (r *fmp4Reader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if err := r.fill(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *fmp4Reader) SeekTime(ms int64) error {
	if r.seekPoint == nil {
		return errors.New("fmp4: no seek index")
	}
	offset, startMs, ok := r.seekPoint(ms)
	if !ok {
		return fmt.Errorf("fmp4: no fragment for %dms", ms)
	}
	r.pos = offset
	r.pending = nil
	if ms > startMs {
		r.targetTime = ms * int64(r.init.timescale) / 1000
	} else {
		r.targetTime = 0
	}
	return nil
}
