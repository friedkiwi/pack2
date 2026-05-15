# UNPACK2 Notes

`UNPACK2` is the DOS unpacking tool in this repository:

```text
original/dos/UNPACK2.EXE
original/dos/UNPACK2_unpacked.exe
```

The original DOS executable was distributed with a Microsoft EXEPACK runtime wrapper. Reverse engineering should use `original/dos/UNPACK2_unpacked.exe`; see [EXEPACK Unpacking Notes](exepack.md) for the unpacking process and validation details.

The compressed member payloads use IBM FTCOMP; see [FTCOMP Compression Notes](ftcomp.md) for the reverse-engineered decompression algorithm.

The outer member/container layout is documented in [PACK2 File Format Notes](pack2_file_format.md).

## Purpose

`UNPACK2` unpacks IBM PACK2/COMPRESS bundle files. The executable contains IBM copyright strings and the archive marker:

```text
XID=COMPRESS VL=NM000
```

This marker appears to be part of the packed-file format that `UNPACK2` reads, not the EXEPACK wrapper.

## Command Line

The embedded usage text documents these forms:

```text
[drive][path]UNPACK2 [drive][path]SOURCE_FILE [drive][destination_path]
    [/V] [/P] [/C] [/N:specific_file]

[drive][path]UNPACK2 [drive][path]SOURCE_FILE [drive][destination_path]
    [/SHOW] [/SIZES]

[drive][path]UNPACK2 /?
```

Observed flags:

- `/V`: write with DOS verify enabled.
- `/P`: prepend the command-line path to the packed file path.
- `/C`: create the target directory if it does not exist.
- `/?`: show usage information.
- `/N:specific_file`: unpack one file from the bundle.
- `/SHOW`: show packed files in the bundle.
- `/SIZES`: show packed-file sizes.

## User-Facing Output

The program includes these status/error strings:

```text
%6d Files have been copied.
%6d Files have been unpacked.
Error: Not enough memory to continue.
```

The executable also carries Microsoft C runtime error messages such as stack overflow, divide-by-zero, missing floating-point support, and null pointer assignment.

## Current Understanding

At this point, the tool appears to:

- Open a PACK2/COMPRESS source bundle.
- Optionally list bundle contents with `/SHOW`.
- Optionally list packed sizes with `/SIZES`.
- Extract all files or a single named file with `/N:specific_file`.
- Write extracted files under an optional destination path.
- Optionally create missing destination directories with `/C`.

Open questions for further reversing:

- Exact on-disk layout of the `XID=COMPRESS VL=NM000` bundle format.
- Compression algorithm used inside the bundle.
- How `/P` maps source paths into destination paths.
- Whether `/SHOW` and `/SIZES` parse all metadata without decompressing file bodies.
