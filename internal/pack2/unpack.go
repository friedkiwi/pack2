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

// Read parses a PACK2/COMPRESS archive from r.
func Read(r io.Reader) (*Archive, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	return parse(data)
}

func parse(data []byte) (*Archive, error) {
	var files []File
	for off := 0; ; {
		if off+fixedHeaderSize > len(data) {
			return nil, fmt.Errorf("%w: truncated member header", ErrInvalidArchive)
		}

		h := data[off : off+fixedHeaderSize]
		if binary.LittleEndian.Uint16(h[0x00:0x02]) != magic0 || binary.LittleEndian.Uint16(h[0x02:0x04]) != magic1 {
			return nil, fmt.Errorf("%w: bad member magic at offset %d", ErrInvalidArchive, off)
		}

		dataEndOffset := binary.LittleEndian.Uint32(h[0x0c:0x10])
		unpackedSize := binary.LittleEndian.Uint32(h[0x10:0x14])
		nextMemberOffset := binary.LittleEndian.Uint32(h[0x14:0x18])
		filenameLen := int(binary.LittleEndian.Uint16(h[0x27:0x29]))
		nameOff := off + fixedHeaderSize
		payloadOff := nameOff + filenameLen

		if filenameLen == 0 || payloadOff > len(data) {
			return nil, fmt.Errorf("%w: truncated filename", ErrInvalidArchive)
		}

		nameBytes := data[nameOff:payloadOff]
		nul := bytes.IndexByte(nameBytes, 0)
		if nul < 0 {
			return nil, fmt.Errorf("%w: filename is not NUL-terminated", ErrInvalidArchive)
		}

		payloadEnd := len(data) - 4
		switch {
		case dataEndOffset != 0:
			payloadEnd = int(dataEndOffset)
		case nextMemberOffset != 0:
			payloadEnd = int(nextMemberOffset)
		}
		if payloadEnd < payloadOff || payloadEnd > len(data) {
			return nil, fmt.Errorf("%w: bad payload bounds", ErrInvalidArchive)
		}

		files = append(files, File{
			Name:          string(nameBytes[:nul]),
			Method:        cString(h[0x18:0x1f]),
			MethodType:    binary.LittleEndian.Uint16(h[0x21:0x23]),
			PackedSize:    int64(payloadEnd - payloadOff),
			UnpackedSize:  int64(unpackedSize),
			DOSDate:       binary.LittleEndian.Uint16(h[0x04:0x06]),
			DOSTime:       binary.LittleEndian.Uint16(h[0x06:0x08]),
			Attrs:         binary.LittleEndian.Uint16(h[0x08:0x0a]),
			payloadOffset: int64(payloadOff),
		})

		if nextMemberOffset == 0 {
			break
		}
		if int(nextMemberOffset) <= off {
			return nil, fmt.Errorf("%w: non-forward next member offset", ErrInvalidArchive)
		}
		off = int(nextMemberOffset)
	}

	return &Archive{Files: files, data: data}, nil
}

// Open parses a PACK2/COMPRESS archive from path.
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	return Read(f)
}

// Unpack extracts a PACK2/COMPRESS archive from archivePath.
func Unpack(archivePath string, opts UnpackOptions) error {
	archive, err := Open(archivePath)
	if err != nil {
		return err
	}

	matched := false
	for _, file := range archive.Files {
		if opts.FileName != "" && !sameBaseName(file.Name, opts.FileName) {
			continue
		}
		matched = true

		body, err := archive.Extract(file)
		if err != nil {
			return fmt.Errorf("extract %s: %w", file.Name, err)
		}

		path, err := outputPath(opts.Destination, file.Name)
		if err != nil {
			return err
		}
		if opts.CreateDirs {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
		}
		if err := os.WriteFile(path, body, fileMode(file.Attrs)); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		modTime := timeFromDOS(file.DOSDate, file.DOSTime)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			return fmt.Errorf("set timestamp for %s: %w", path, err)
		}
	}

	if !matched && opts.FileName != "" {
		return fmt.Errorf("%w: file %q not found", ErrInvalidArchive, opts.FileName)
	}

	return nil
}

// Extract returns the unpacked bytes for file.
func (a *Archive) Extract(file File) ([]byte, error) {
	start := int(file.payloadOffset)
	end := start + int(file.PackedSize)
	if start < 0 || end < start || end > len(a.data) {
		return nil, fmt.Errorf("%w: bad payload bounds", ErrInvalidArchive)
	}

	payload := a.data[start:end]
	if strings.EqualFold(file.Method, methodFTCOMP) && file.MethodType == 1 {
		if len(payload) < 4 {
			return nil, fmt.Errorf("%w: truncated FTCOMP payload prefix", ErrInvalidArchive)
		}
		r, err := ftcomp.NewReader(bytes.NewReader(payload[4:]))
		if err != nil {
			return nil, err
		}
		defer r.Close()

		out, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		if int64(len(out)) != file.UnpackedSize {
			return nil, fmt.Errorf("%w: unpacked size mismatch for %s", ErrInvalidArchive, file.Name)
		}
		return out, nil
	}

	return append([]byte(nil), payload...), nil
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func sameBaseName(name, want string) bool {
	return strings.EqualFold(filepath.Base(filepath.FromSlash(name)), filepath.Base(filepath.FromSlash(want)))
}

func outputPath(destination, name string) (string, error) {
	name = filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(name) || name == "." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || name == ".." {
		return "", fmt.Errorf("%w: unsafe member path %q", ErrInvalidArchive, name)
	}
	if destination == "" {
		return name, nil
	}
	return filepath.Join(destination, name), nil
}

func fileMode(attrs uint16) os.FileMode {
	if attrs&0x01 != 0 {
		return 0o444
	}
	return 0o644
}

func timeFromDOS(date, tm uint16) time.Time {
	year := int(date>>9) + 1980
	month := time.Month((date >> 5) & 0x0f)
	day := int(date & 0x1f)
	hour := int(tm >> 11)
	min := int((tm >> 5) & 0x3f)
	sec := int(tm&0x1f) * 2

	if month < time.January || month > time.December || day < 1 || day > 31 {
		return time.Unix(0, 0)
	}
	return time.Date(year, month, day, hour, min, sec, 0, time.Local)
}
