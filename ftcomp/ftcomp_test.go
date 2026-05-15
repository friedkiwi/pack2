package ftcomp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestNewReaderUntaggedPassthrough(t *testing.T) {
	in := []byte("plain data")

	r, err := NewReader(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("out = %q, want %q", out, in)
	}
}

func TestNewReaderRawTaggedBlocks(t *testing.T) {
	in := []byte{
		'f', 'T', '1', '9',
		0xff, 0xff, 0x05, 0x00, 'h', 'e', 'l', 'l', 'o',
		0xff, 0xff, 0x06, 0x00, ' ', 'w', 'o', 'r', 'l', 'd',
	}

	r, err := NewReader(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "hello world"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestNewReaderVersion2RawBlock(t *testing.T) {
	in := []byte{
		'f', 'T', '2', '1',
		0xff, 0xff, 0x04, 0x00, 0xff, 'r', 'a', 'w',
	}

	r, err := NewReader(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xff, 'r', 'a', 'w'}; !bytes.Equal(out, want) {
		t.Fatalf("out = %x, want %x", out, want)
	}
}

func TestWriterRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if _, err := w.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "hello world"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestReaderReset(t *testing.T) {
	r, err := NewReader(bytes.NewReader([]byte("first")))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	resetter := r.(Resetter)
	if err := resetter.Reset(bytes.NewReader([]byte("second")), nil); err != nil {
		t.Fatal(err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "second"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestCompressedBlockUnsupported(t *testing.T) {
	in := []byte{'f', 'T', '1', '9', 0x01, 0x00, 0, 0, 0, 0}

	_, err := NewReader(bytes.NewReader(in))
	if !errors.Is(err, ErrUnsupportedData) {
		t.Fatalf("err = %v, want ErrUnsupportedData", err)
	}
}

func TestExpandMarkerStream(t *testing.T) {
	in := []byte{'a', 'b', 'c', markerByte, 0x00, 0x02}

	out, err := expandMarkerStream(in, Version1)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "abcabc"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestExpandMarkerStreamEscape(t *testing.T) {
	in := []byte{'a', markerByte, 0x40, 'b'}

	out, err := expandMarkerStream(in, Version1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{'a', markerByte, 'b'}; !bytes.Equal(out, want) {
		t.Fatalf("out = %x, want %x", out, want)
	}
}
