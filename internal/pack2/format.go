package pack2

import "errors"

const (
	fixedHeaderSize = 0x29
	payloadPrefix   = uint32(0x6080)

	magic0 = 0x96a5
	magic1 = 0xfffd

	methodFTCOMP = "FTCOMP"
)

var (
	ErrInvalidArchive     = errors.New("invalid PACK2 archive")
	ErrUnsupportedArchive = errors.New("unsupported PACK2 archive")
)

// Archive is the parsed metadata for a PACK2/COMPRESS bundle.
type Archive struct {
	Files []File

	data []byte
}

// File describes one file stored in a PACK2/COMPRESS bundle.
type File struct {
	Name         string
	Method       string
	MethodType   uint16
	PackedSize   int64
	UnpackedSize int64
	DOSDate      uint16
	DOSTime      uint16
	Attrs        uint16

	payloadOffset int64
}

// UnpackOptions controls archive extraction behavior.
type UnpackOptions struct {
	Destination string
	CreateDirs  bool
	FileName    string
}

// PackOptions controls archive creation behavior.
type PackOptions struct {
	SourcePaths []string
}
