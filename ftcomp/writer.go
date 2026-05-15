package ftcomp

import (
	"encoding/binary"
	"fmt"
	"io"
)

const maxRawBlockSize = 0xffff

type Writer struct {
	w      io.Writer
	err    error
	closed bool
}

// NewWriter creates a new FTCOMP writer.
func NewWriter(w io.Writer) *Writer {
	z, err := NewWriterLevel(w, DefaultCompression)
	if err != nil {
		panic(err)
	}
	return z
}

// NewWriterLevel creates a new FTCOMP writer using level.
func NewWriterLevel(w io.Writer, level int) (*Writer, error) {
	return NewWriterLevelDict(w, level, nil)
}

// NewWriterLevelDict creates a new FTCOMP writer. FTCOMP does not support preset dictionaries.
func NewWriterLevelDict(w io.Writer, level int, dict []byte) (*Writer, error) {
	if len(dict) != 0 {
		return nil, ErrDictionary
	}
	if level != HuffmanOnly && level != DefaultCompression && (level < NoCompression || level > BestCompression) {
		return nil, fmt.Errorf("ftcomp: invalid compression level: %d", level)
	}

	z := &Writer{w: w}
	if err := z.writeHeader(); err != nil {
		return nil, err
	}
	return z, nil
}

func (z *Writer) Write(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}
	if z.closed {
		return 0, fmt.Errorf("ftcomp: write after close")
	}

	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > maxRawBlockSize {
			n = maxRawBlockSize
		}

		if err := writeRawBlock(z.w, p[:n]); err != nil {
			z.err = err
			return written, z.err
		}

		written += n
		p = p[n:]
	}

	return written, nil
}

func (z *Writer) Flush() error {
	if z.err != nil {
		return z.err
	}
	if flusher, ok := z.w.(interface{ Flush() error }); ok {
		z.err = flusher.Flush()
	}
	return z.err
}

func (z *Writer) Close() error {
	if z.closed {
		return nil
	}
	z.closed = true
	return z.Flush()
}

func (z *Writer) Reset(w io.Writer) {
	z.w = w
	z.err = nil
	z.closed = false
	z.err = z.writeHeader()
}

func (z *Writer) writeHeader() error {
	_, err := io.WriteString(z.w, tag19)
	return err
}

func writeRawBlock(w io.Writer, p []byte) error {
	var header [4]byte
	binary.LittleEndian.PutUint16(header[0:2], 0xffff)
	binary.LittleEndian.PutUint16(header[2:4], uint16(len(p)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(p)
	return err
}
