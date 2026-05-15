package ftcomp

import "fmt"

type bitReader struct {
	data []byte
	pos  int
	buf  uint16
	nb   int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (b *bitReader) fill() error {
	for b.nb <= 8 {
		b.appendByte()
	}
	return nil
}

func (b *bitReader) appendByte() {
	if b.pos >= len(b.data) {
		b.nb += 8
		return
	}
	b.buf |= uint16(b.data[b.pos]) << (8 - b.nb)
	b.pos++
	b.nb += 8
}

func (b *bitReader) peek16() (uint16, error) {
	if err := b.fill(); err != nil {
		return 0, err
	}
	return b.buf, nil
}

func (b *bitReader) readBits(n int) (uint16, error) {
	if n < 0 || n > 16 {
		return 0, fmt.Errorf("%w: invalid bit count", ErrInvalidData)
	}
	if n == 0 {
		return 0, nil
	}
	for b.nb < n {
		b.appendByte()
	}
	v := b.buf >> (16 - n)
	b.buf <<= n
	b.nb -= n
	return v, nil
}

func (b *bitReader) consume(n int) error {
	_, err := b.readBits(n)
	return err
}

func (b *bitReader) consumedBytes() int {
	return b.pos - b.nb/8
}
