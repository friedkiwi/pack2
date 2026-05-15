package ftcomp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	tag19 = "fT19"
	tag21 = "fT21"

	markerByte = 0x9e

	Version1 = 1
	Version2 = 2

	NoCompression      = 0
	BestSpeed          = 1
	BestCompression    = 9
	DefaultCompression = -1
	HuffmanOnly        = -2
)

var (
	ErrChecksum        = errors.New("ftcomp: invalid checksum")
	ErrDictionary      = errors.New("ftcomp: invalid dictionary")
	ErrHeader          = errors.New("ftcomp: invalid header")
	ErrInvalidData     = errors.New("invalid FTCOMP data")
	ErrUnsupportedData = errors.New("unsupported FTCOMP data")
)

type Resetter interface {
	Reset(r io.Reader, dict []byte) error
}

type reader struct {
	*bytes.Reader
}

func (r *reader) Close() error {
	return nil
}

func (r *reader) Reset(src io.Reader, dict []byte) error {
	if len(dict) != 0 {
		return ErrDictionary
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	out, err := Decode(data)
	if err != nil {
		return err
	}

	r.Reader.Reset(out)
	return nil
}

// NewReader creates a new FTCOMP reader.
func NewReader(r io.Reader) (io.ReadCloser, error) {
	return NewReaderDict(r, nil)
}

// NewReaderDict creates a new FTCOMP reader. FTCOMP does not support preset dictionaries.
func NewReaderDict(r io.Reader, dict []byte) (io.ReadCloser, error) {
	if len(dict) != 0 {
		return nil, ErrDictionary
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	out, err := Decode(data)
	if err != nil {
		return nil, err
	}

	return &reader{Reader: bytes.NewReader(out)}, nil
}

// Decode decompresses an FTCOMP payload.
func Decode(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return append([]byte(nil), data...), nil
	}

	switch string(data[:4]) {
	case tag19:
		return decodeTagged(data[4:], Version1)
	case tag21:
		return decodeTagged(data[4:], Version2)
	default:
		return append([]byte(nil), data...), nil
	}
}

func decodeTagged(data []byte, version int) ([]byte, error) {
	var out []byte

	for len(data) > 0 {
		if len(data) < 2 {
			return nil, fmt.Errorf("%w: truncated block header", ErrInvalidData)
		}

		target := binary.LittleEndian.Uint16(data[:2])
		data = data[2:]

		if target == 0xffff {
			if len(data) < 2 {
				return nil, fmt.Errorf("%w: truncated raw block size", ErrInvalidData)
			}

			n := int(binary.LittleEndian.Uint16(data[:2]))
			data = data[2:]
			if len(data) < n {
				return nil, fmt.Errorf("%w: truncated raw block data", ErrInvalidData)
			}

			out = append(out, data[:n]...)
			data = data[n:]
			continue
		}

		if len(data) < 4 {
			return nil, fmt.Errorf("%w: truncated compressed block header", ErrInvalidData)
		}
		block := compressedBlock{
			intermediateTarget: int(target),
			literalWeightA:     data[0],
			markerWeightA:      data[1],
			literalWeightB:     data[2],
			markerWeightB:      data[3],
			bitstream:          data[4:],
			version:            version,
			bytesProduced:      len(out),
		}
		decoded, consumed, err := decodeCompressedBlock(block)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded...)
		data = data[4+consumed:]
		if len(data) > 0 && isPadding(data) {
			break
		}
	}

	return out, nil
}

func isPadding(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

type compressedBlock struct {
	intermediateTarget int
	literalWeightA     byte
	markerWeightA      byte
	literalWeightB     byte
	markerWeightB      byte
	bitstream          []byte
	version            int
	bytesProduced      int
}

func decodeCompressedBlock(block compressedBlock) ([]byte, int, error) {
	br := newBitReader(block.bitstream)
	staticTable, err := buildHuffTable(staticWeights)
	if err != nil {
		return nil, 0, err
	}

	model := make([]uint16, modelSymbolCount)
	for i := 0; i < modelSymbolCount; {
		sym, err := staticTable.decode(br)
		if err != nil {
			return nil, 0, fmt.Errorf("decode FTCOMP model: %w", err)
		}
		if sym == 0x100 {
			count := 16
			if remaining := modelSymbolCount - i; remaining < count {
				count = remaining
			}
			i += count
			continue
		}
		model[i] = uint16(byte(sym))
		i++
	}

	tableA, err := buildAdaptiveTable(model, block.literalWeightA, block.markerWeightA)
	if err != nil {
		return nil, 0, err
	}
	tableB := tableA
	if (block.literalWeightB != block.literalWeightA || block.markerWeightB != block.markerWeightA) &&
		(block.literalWeightB != 0 || block.markerWeightB != 0) {
		tableB, err = buildAdaptiveTable(model, block.literalWeightB, block.markerWeightB)
		if err != nil {
			return nil, 0, err
		}
	}

	intermediate, err := decodeIntermediate(br, tableA, tableB, block)
	if err != nil {
		return nil, 0, err
	}

	out, err := expandMarkerStream(intermediate, block.version)
	if err != nil {
		return nil, 0, err
	}
	if block.version == Version2 {
		out, err = expandRLE(out)
		if err != nil {
			return nil, 0, err
		}
	}
	return out, br.pos, nil
}

func buildAdaptiveTable(model []uint16, literalWeight, markerWeight byte) (*huffTable, error) {
	weights := make([]uint16, modelSymbolCount)
	maxWeight := 0
	for sym, m := range model {
		if m == 0 {
			continue
		}
		scale := literalWeight
		if symbolClass[sym] != 0 {
			scale = markerWeight
		}
		w := int(m) * int(scale)
		weights[sym] = uint16(w)
		if w > maxWeight {
			maxWeight = w
		}
	}
	if maxWeight > 0xff {
		scale := 0xffff / maxWeight
		for i, w := range weights {
			if w == 0 {
				continue
			}
			scaled := (int(w) * scale) >> 8
			if scaled == 0 {
				scaled = 1
			}
			weights[i] = uint16(scaled)
		}
	}
	return buildHuffTable(weights)
}

type mtfPair struct {
	recent0 int
	recent1 int
}

func (m *mtfPair) decode(value int, version int) int {
	if value == 0x100 {
		return m.recent0
	}
	if version == Version2 && value == 0x101 {
		return m.recent1
	}
	if m.recent1 < m.recent0 {
		if m.recent1 <= value {
			value++
		}
		if m.recent0 <= value {
			value++
		}
	} else {
		if m.recent0 <= value {
			value++
		}
		if m.recent1 <= value {
			value++
		}
	}
	m.recent1 = m.recent0
	m.recent0 = value
	return value
}

func decodeIntermediate(br *bitReader, tableA, tableB *huffTable, block compressedBlock) ([]byte, error) {
	out := make([]byte, 0, block.intermediateTarget)
	var twoByteHistory history
	var markerHistory history
	twoByteCursor := 0x20
	markerCursor := 0x20
	var mtf [4]mtfPair
	subState := 0
	pendingState := 0
	pendingRecord := -1
	finalEstimate := block.bytesProduced

	for len(out) < block.intermediateTarget {
		if pendingState != 0 {
			nextState, err := appendSuffix(br, tableB, &out, &mtf, pendingState, block.version, finalEstimate)
			if err != nil {
				return nil, err
			}
			pendingState = nextState
			if pendingState == 0 && pendingRecord >= 0 {
				if pendingRecord+1 < len(out) {
					finalEstimate += markerLengthEstimate(out[pendingRecord+1], out[pendingRecord+2:])
				}
				pendingRecord = -1
			}
			continue
		}

		table := tableA
		if subState != 0 {
			table = tableB
		}
		sym, err := table.decode(br)
		if err != nil {
			return nil, fmt.Errorf("decode FTCOMP symbol at intermediate offset %d, input byte %d: %w", len(out), br.pos, err)
		}
		if sym < 0 || sym >= modelSymbolCount {
			return nil, fmt.Errorf("%w: invalid symbol %d", ErrInvalidData, sym)
		}
		subState = int(symbolClass[sym])

		switch {
		case sym <= 0xff:
			out = append(out, byte(sym))
			finalEstimate++
			if sym == markerByte && block.version == Version2 {
				out = append(out, 0xff)
			}
		case sym <= 0x180:
			control := byte(sym - 0x100)
			out = append(out, markerByte)
			controlOffset := len(out)
			out = append(out, control)
			pendingState = int(markerControlClass[control])
			if pendingState == 0 {
				if control == 0x40 && block.version == Version1 {
					finalEstimate++
				}
				pendingRecord = -1
			} else {
				pendingRecord = controlOffset - 1
				markerHistory.insert(&markerCursor, uint16(controlOffset))
			}
		case sym <= 0x190:
			distance := sym - 0x17f
			if distance < 2 || distance > 17 || len(out) < distance {
				return nil, fmt.Errorf("%w: invalid recent output pair", ErrInvalidData)
			}
			pair := uint16(out[len(out)-distance]) | uint16(out[len(out)-distance+1])<<8
			out = append(out, byte(pair), byte(pair>>8))
			twoByteHistory.insert(&twoByteCursor, pair)
			finalEstimate += pairLengthEstimate(pair)
		case sym <= 0x1a0:
			idx := sym - 0x191
			if idx < 0 || idx >= 16 {
				return nil, fmt.Errorf("%w: invalid two-byte history index", ErrInvalidData)
			}
			pair := twoByteHistory.at(twoByteCursor, idx)
			out = append(out, byte(pair), byte(pair>>8))
			twoByteHistory.promote(twoByteCursor, idx, pair)
			finalEstimate += pairLengthEstimate(pair)
		default:
			idx := sym - 0x1a1
			if idx < 0 || idx >= 16 {
				return nil, fmt.Errorf("%w: invalid marker history index %d at intermediate offset %d", ErrInvalidData, idx, len(out))
			}
			controlOffset := int(markerHistory.at(markerCursor, idx))
			if controlOffset < 0 || controlOffset >= len(out) {
				return nil, fmt.Errorf("%w: invalid marker history record", ErrInvalidData)
			}
			control := out[controlOffset]
			recordLen := 1 + int(markerControlClass[control])
			if controlOffset+recordLen > len(out) {
				return nil, fmt.Errorf("%w: truncated marker history record", ErrInvalidData)
			}
			out = append(out, markerByte)
			newControlOffset := len(out)
			record := append([]byte(nil), out[controlOffset:controlOffset+recordLen]...)
			out = append(out, record...)
			finalEstimate += markerLengthEstimate(record[0], record[1:])
			markerHistory.promote(markerCursor, idx, uint16(newControlOffset))
		}
	}

	if len(out) != block.intermediateTarget {
		return nil, fmt.Errorf("%w: FTCOMP intermediate overflow", ErrInvalidData)
	}
	return out, nil
}

type history [48]uint16

func (h *history) insert(cursor *int, value uint16) {
	old := *cursor
	*cursor--
	if old == 0 {
		copy(h[32:48], h[0:16])
		*cursor = 0x1f
	}
	h[*cursor] = value
}

func (h *history) at(cursor int, idx int) uint16 {
	return h[cursor+idx]
}

func (h *history) promote(cursor int, idx int, value uint16) {
	copy(h[cursor+1:cursor+idx+1], h[cursor:cursor+idx])
	h[cursor] = value
}

func pairLengthEstimate(pair uint16) int {
	if byte(pair) == markerByte {
		return 1
	}
	return 2
}

func appendSuffix(br *bitReader, table *huffTable, out *[]byte, mtf *[4]mtfPair, state int, version int, finalEstimate int) (int, error) {
	if state == 1 || state == 3 {
		value, err := table.decode(br)
		if err != nil {
			return 0, fmt.Errorf("decode FTCOMP suffix: %w", err)
		}
		value = mtf[0].decode(value, version)
		*out = append(*out, byte(value))
		if state == 3 {
			return 2, nil
		}
		return 0, nil
	}

	suffixClass, low, err := readSuffixPrefix(br, finalEstimate)
	if err != nil {
		return 0, err
	}
	value, err := table.decode(br)
	if err != nil {
		return 0, fmt.Errorf("decode FTCOMP suffix: %w", err)
	}
	classIdx := suffixClass + 1
	if classIdx >= len(mtf) {
		classIdx = len(mtf) - 1
	}
	value = mtf[classIdx].decode(value, version)

	var word int
	switch suffixClass {
	case 0:
		word = ((value + 0x10) << 4) | low
	case 1:
		word = ((value + 0x44) << 6) | low
	case 2:
		if finalEstimate < 0x9100 {
			word = ((value + 0x144) << 6) | low
		} else {
			word = ((value + 0x0a2) << 7) | low
		}
	default:
		return 0, fmt.Errorf("%w: invalid suffix class", ErrInvalidData)
	}
	*out = append(*out, byte(word), byte(word>>8))
	return 0, nil
}

func readSuffixPrefix(br *bitReader, finalEstimate int) (class int, low int, err error) {
	buf, err := br.peek16()
	if err != nil {
		return 0, 0, err
	}
	switch {
	case buf&0x8000 == 0:
		v, err := br.readBits(5)
		return 0, int(v & 0x0f), err
	case buf&0x4000 == 0:
		if finalEstimate < 0x5100 {
			v, err := br.readBits(7)
			return 1, int(v & 0x3f), err
		}
		v, err := br.readBits(8)
		return 1, int(v & 0x3f), err
	default:
		if finalEstimate < 0x9100 {
			v, err := br.readBits(8)
			return 2, int(v & 0x3f), err
		}
		v, err := br.readBits(9)
		return 2, int(v & 0x7f), err
	}
}

func markerLengthEstimate(control byte, suffix []byte) int {
	switch {
	case control == 0x80:
		if len(suffix) == 0 {
			return 0x43
		}
		return int(suffix[0]) + 0x43
	case control&0x40 != 0:
		return int(control&0x3f) + 3
	default:
		return int(control) + 3
	}
}

func expandMarkerStream(src []byte, version int) ([]byte, error) {
	escape := byte(0x40)
	if version == Version2 {
		escape = 0xff
	}

	dst := make([]byte, 0, len(src))
	for i := 0; i < len(src); {
		b := src[i]
		i++

		if b != markerByte {
			dst = append(dst, b)
			continue
		}

		if i >= len(src) {
			return nil, fmt.Errorf("%w: truncated marker record", ErrInvalidData)
		}

		code := src[i]
		i++

		if code == escape {
			dst = append(dst, markerByte)
			continue
		}

		var length int
		var distance int

		switch {
		case code == 0x80:
			if len(src)-i < 3 {
				return nil, fmt.Errorf("%w: truncated long marker record", ErrInvalidData)
			}
			length = int(src[i]) + 0x43
			distance = int(binary.LittleEndian.Uint16(src[i+1 : i+3]))
			i += 3
		case code&0x40 != 0:
			if len(src)-i < 2 {
				return nil, fmt.Errorf("%w: truncated medium marker record", ErrInvalidData)
			}
			length = int(code&0x3f) + 3
			distance = int(binary.LittleEndian.Uint16(src[i : i+2]))
			i += 2
		default:
			if len(src)-i < 1 {
				return nil, fmt.Errorf("%w: truncated short marker record", ErrInvalidData)
			}
			length = int(code) + 3
			distance = int(src[i])
			i++
		}

		copyFrom := len(dst) - distance - 1
		if copyFrom < 0 {
			return nil, fmt.Errorf("%w: invalid marker distance", ErrInvalidData)
		}
		for range length {
			dst = append(dst, dst[copyFrom])
			copyFrom++
		}
	}

	return dst, nil
}

func expandRLE(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}
	if src[0] == 0xff {
		return append([]byte(nil), src[1:]...), nil
	}

	return nil, fmt.Errorf("%w: fT21 RLE side-stream expansion is not implemented", ErrUnsupportedData)
}
