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

		return nil, fmt.Errorf("%w: compressed FTCOMP blocks are not implemented", ErrUnsupportedData)
	}

	return out, nil
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
