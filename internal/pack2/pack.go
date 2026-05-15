package pack2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/friedkiwi/pack2/ftcomp"
)

// Write creates a PACK2/COMPRESS archive on w.
func Write(w io.Writer, opts PackOptions) error {
	if len(opts.SourcePaths) == 0 {
		return fmt.Errorf("pack archive: no input files")
	}

	members := make([]packedMember, 0, len(opts.SourcePaths))
	for _, path := range opts.SourcePaths {
		member, err := buildMember(path)
		if err != nil {
			return err
		}
		members = append(members, member)
	}

	offset := 0
	for i := range members {
		members[i].offset = offset
		offset += fixedHeaderSize + len(members[i].nameBytes) + len(members[i].payload)
		if offset > int(^uint32(0)) {
			return fmt.Errorf("pack archive: archive is too large")
		}
	}

	for i, member := range members {
		next := uint32(0)
		if i+1 < len(members) {
			next = uint32(members[i+1].offset)
		}
		if err := writeMember(w, member, next); err != nil {
			return err
		}
	}

	_, err := w.Write([]byte{0, 0, 0, 0})
	return err
}

// Pack creates a PACK2/COMPRESS archive at archivePath.
func Pack(archivePath string, opts PackOptions) error {
	if archivePath == "" {
		return fmt.Errorf("pack archive: output path is required")
	}

	f, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	if err := Write(f, opts); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

type packedMember struct {
	name       string
	nameBytes  []byte
	payload    []byte
	size       uint32
	attrs      uint16
	dosDate    uint16
	dosTime    uint16
	methodArg0 uint16
	offset     int
}

func buildMember(path string) (packedMember, error) {
	info, err := os.Stat(path)
	if err != nil {
		return packedMember{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return packedMember{}, fmt.Errorf("pack archive: %s is a directory", path)
	}
	if info.Size() > int64(^uint32(0)) {
		return packedMember{}, fmt.Errorf("pack archive: %s is too large", path)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return packedMember{}, fmt.Errorf("read %s: %w", path, err)
	}

	var stream bytes.Buffer
	zw := ftcomp.NewWriter(&stream)
	if _, err := zw.Write(body); err != nil {
		return packedMember{}, fmt.Errorf("compress %s: %w", path, err)
	}
	if err := zw.Close(); err != nil {
		return packedMember{}, fmt.Errorf("compress %s: %w", path, err)
	}

	payload := make([]byte, 4, stream.Len()+4)
	binary.LittleEndian.PutUint32(payload[:4], payloadPrefix)
	payload = append(payload, stream.Bytes()...)

	date, tm := dosDateTime(info.ModTime())
	name := archiveName(path)
	nameBytes := append([]byte(name), 0)
	if len(nameBytes) > int(^uint16(0)) {
		return packedMember{}, fmt.Errorf("pack archive: filename too long: %s", name)
	}

	return packedMember{
		name:       name,
		nameBytes:  nameBytes,
		payload:    payload,
		size:       uint32(len(body)),
		attrs:      dosAttrs(info.Mode()),
		dosDate:    date,
		dosTime:    tm,
		methodArg0: 0,
	}, nil
}

func writeMember(w io.Writer, member packedMember, next uint32) error {
	var h [fixedHeaderSize]byte
	binary.LittleEndian.PutUint16(h[0x00:0x02], magic0)
	binary.LittleEndian.PutUint16(h[0x02:0x04], magic1)
	binary.LittleEndian.PutUint16(h[0x04:0x06], member.dosDate)
	binary.LittleEndian.PutUint16(h[0x06:0x08], member.dosTime)
	binary.LittleEndian.PutUint16(h[0x08:0x0a], member.attrs)
	binary.LittleEndian.PutUint32(h[0x10:0x14], member.size)
	binary.LittleEndian.PutUint32(h[0x14:0x18], next)
	copy(h[0x18:0x1f], []byte(methodFTCOMP))
	binary.LittleEndian.PutUint16(h[0x1f:0x21], member.methodArg0)
	binary.LittleEndian.PutUint16(h[0x21:0x23], 1)
	binary.LittleEndian.PutUint32(h[0x23:0x27], 4)
	binary.LittleEndian.PutUint16(h[0x27:0x29], uint16(len(member.nameBytes)))

	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	if _, err := w.Write(member.nameBytes); err != nil {
		return err
	}
	_, err := w.Write(member.payload)
	return err
}

func archiveName(path string) string {
	return strings.ToUpper(filepath.Base(path))
}

func dosAttrs(mode os.FileMode) uint16 {
	if mode&0o200 == 0 {
		return 0x21
	}
	return 0x20
}

func dosDateTime(t time.Time) (uint16, uint16) {
	t = t.Local()
	year, month, day := t.Date()
	if year < 1980 {
		year = 1980
		month = time.January
		day = 1
	}
	if year > 2107 {
		year = 2107
		month = time.December
		day = 31
	}
	hour, min, sec := t.Clock()
	date := uint16((year-1980)<<9 | int(month)<<5 | day)
	tm := uint16(hour<<11 | min<<5 | sec/2)
	return date, tm
}
