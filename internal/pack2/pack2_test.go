package pack2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndExtractStoredSample(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "original", "examples", "DUMMY.TX_"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	archive, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(archive.Files))
	}

	file := archive.Files[0]
	if file.Name != "DUMMY.TXT" {
		t.Fatalf("name = %q, want DUMMY.TXT", file.Name)
	}
	if file.Method != methodFTCOMP {
		t.Fatalf("method = %q, want %q", file.Method, methodFTCOMP)
	}
	if file.UnpackedSize != 3 {
		t.Fatalf("unpacked size = %d, want 3", file.UnpackedSize)
	}

	out, err := archive.Extract(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x0d, 0x0a, 0x1a}; !bytes.Equal(out, want) {
		t.Fatalf("out = %x, want %x", out, want)
	}
}

func TestReadAndExtractCompressedSample(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "original", "examples", "EVALUATE.LI_"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	archive, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(archive.Files))
	}
	file := archive.Files[0]
	if file.Name != "EVALUATE.LIC" {
		t.Fatalf("name = %q, want EVALUATE.LIC", file.Name)
	}
	if file.UnpackedSize != 683 {
		t.Fatalf("unpacked size = %d, want 683", file.UnpackedSize)
	}

	out, err := archive.Extract(file)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "original", "examples", "EVALUATE.LIC"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("decompressed EVALUATE.LIC does not match OS/2 UNPACK2 output")
	}
}

func TestReadAndExtractOS2DrvSamples(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "original", "examples", "os2drv.pk2"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	archive, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{`\os2\mdos\vsvga.sys`, `\os2\dll\VIDEOPMI.DLL`} {
		var file File
		found := false
		for _, candidate := range archive.Files {
			if candidate.Name == name {
				file = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("file %q not found", name)
		}
		out, err := archive.Extract(file)
		if err != nil {
			t.Fatalf("extract %s: %v", name, err)
		}
		if int64(len(out)) != file.UnpackedSize {
			t.Fatalf("extract %s len = %d, want %d", name, len(out), file.UnpackedSize)
		}
	}
}

func TestPackReadExtractRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("hello pack2"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Write(&buf, PackOptions{SourcePaths: []string{src}}); err != nil {
		t.Fatal(err)
	}

	archive, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(archive.Files))
	}
	if got, want := archive.Files[0].Name, "HELLO.TXT"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}

	out, err := archive.Extract(archive.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "hello pack2"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestUnpackWritesSelectedFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("hello pack2"), 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "sample.p2")
	if err := Pack(archivePath, PackOptions{SourcePaths: []string{src}}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(archivePath, UnpackOptions{
		Destination: outDir,
		CreateDirs:  true,
		FileName:    "hello.txt",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(filepath.Join(outDir, "HELLO.TXT"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "hello pack2"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}
