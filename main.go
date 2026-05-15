package main

import (
	"fmt"
	"os"

	"github.com/friedkiwi/pack2/internal/pack2"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pack2: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		usage()
		return nil
	case "list":
		return list(args[1:])
	case "unpack":
		return unpack(args[1:])
	case "pack":
		return pack(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func list(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: pack2 list <archive>")
	}

	archive, err := pack2.Open(args[0])
	if err != nil {
		return err
	}

	for _, file := range archive.Files {
		fmt.Fprintln(os.Stdout, file.Name)
	}

	return nil
}

func unpack(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: pack2 unpack <archive> [destination]")
	}

	opts := pack2.UnpackOptions{
		CreateDirs: true,
	}
	if len(args) == 2 {
		opts.Destination = args[1]
	}

	return pack2.Unpack(args[0], opts)
}

func pack(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: pack2 pack <archive> <file> [file...]")
	}

	return pack2.Pack(args[0], pack2.PackOptions{
		SourcePaths: args[1:],
	})
}

func usage() {
	fmt.Fprintf(os.Stdout, "pack2 extracts and creates OS/2 PACK2 archives.\n\n")
	fmt.Fprintf(os.Stdout, "Usage:\n")
	fmt.Fprintf(os.Stdout, "  pack2 list <archive>\n")
	fmt.Fprintf(os.Stdout, "  pack2 unpack <archive> [destination]\n")
	fmt.Fprintf(os.Stdout, "  pack2 pack <archive> <file> [file...]\n")
}
