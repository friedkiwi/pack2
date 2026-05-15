# UNPACK2 Notes

`UNPACK2` is present in two forms in this repository:

```text
original/dos/UNPACK2.EXE
original/dos/UNPACK2_unpacked.exe
original/os2/UNPACK2.EXE
```

The original DOS executable was distributed with a Microsoft EXEPACK runtime wrapper. Reverse engineering should use `original/dos/UNPACK2_unpacked.exe`; see [EXEPACK Unpacking Notes](exepack.md) for the unpacking process and validation details.

The OS/2 executable is a separate NE binary. It is not EXEPACK-wrapped, uses OS/2 APIs such as `DOSCALLS`, `KBDCALLS`, `VIOCALLS`, `NLS`, and `MSG`, and implements the same PACK2/FTCOMP archive extraction path. Its function addresses and global-data offsets differ from the DOS build, so addresses in the FTCOMP notes are labeled by binary when they are implementation-specific.

The compressed member payloads use IBM FTCOMP; see [FTCOMP Compression Notes](ftcomp.md) for the reverse-engineered decompression algorithm.

The outer member/container layout is documented in [PACK2 File Format Notes](pack2_file_format.md).

## Purpose

`UNPACK2` unpacks IBM PACK2/COMPRESS bundle files. The executable contains IBM copyright strings and the archive marker:

```text
XID=COMPRESS VL=NM000
```

This marker appears to be part of the packed-file format that `UNPACK2` reads, not the EXEPACK wrapper.

## OS/2 UNPACK2 Code Map

The attached OS/2 `UNPACK2.EXE` IDB has been renamed and typed to match the current documentation. These names are descriptive, not original symbols.

| Address | Function | Documentation role |
| ---: | --- | --- |
| `0x001aa6` | `unpack2_main(int argc, char far **argv, char far **envp)` | Program entry and top-level dispatch. |
| `0x00003d` | `parse_unpack2_command_line(...)` | Command-line parser for source, destination, and switches. |
| `0x0002b8` | `parse_unpack_options(...)` | Option parser for `/V`, `/P`, `/C`, `/N`, `/SHOW`, `/SIZES`, and `/?`. |
| `0x000b40` | `process_archive_members(...)` | Outer member-processing loop. |
| `0x002154` | `read_pack2_member_header(...)` | Reads the fixed header and filename. |
| `0x002314` | `validate_pack2_member_magic(...)` | Checks the PACK2 magic words. |
| `0x002397` | `compute_pack2_payload_bounds(...)` | Computes primary payload bounds from `data_end_offset`, `next_member_offset`, and file size. |
| `0x002556` | `unpack_ftcomp_member(...)` | FTCOMP member extraction path. |
| `0x00282e` | `copy_stored_member(...)` | Stored/non-FTCOMP copy path. |
| `0x0040a9` | `extract_pack2_member_payload(...)` | Reads member payload bytes and sends them to the selected output path. |
| `0x00465e` | `scan_pack2_archive(...)` | Follows `next_member_offset` to enumerate an archive. |

The FTCOMP implementation inside this OS/2 binary maps to [FTCOMP Compression Notes](ftcomp.md) as follows:

| Address | Function | FTCOMP role |
| ---: | --- | --- |
| `0x00a96e` | `ftcomp_decompress_buffer(...)` | Tag handling, raw fallback, compressed-block decode, and framed post-processing. |
| `0x00a592` | `ftcomp_decode_tagged_buffer(...)` | Buffer-level FTCOMP decode wrapper. |
| `0x00a714` | `ftcomp_init_static_tables(void)` | Static table and fixed-state initialization. |
| `0x0091aa` | `ftcomp_expand_block_to_buffer(...)` | Raw/compressed block decoder. Confirms compressed-block weight byte order and bitstream start offset. |
| `0x008fe2` | `ftcomp_rebuild_adaptive_huffman_tables(void)` | Per-block adaptive table rebuild from decoded model weights. |
| `0x008cbe` | `ftcomp_build_huffman_decode_tables(void)` | Huffman tree and 9-bit fast table builder. |
| `0x008a7c` | `ftcomp_sort_model_symbols(...)` | Non-stable model-symbol sorter used by the builder. |
| `0x00a69c` | `ftcomp_expand_framed_intermediate(...)` | Reads `uint16 segment_len` frames from the intermediate stream. |
| `0x00a622` | `ftcomp_expand_segment_to_output(...)` | Handles segment mode byte and dispatches marker expansion. |
| `0x0075fe` | `ftcomp_expand_marker_stream(...)` | Expands `0x9e` marker records to final output bytes. |
| `0x00993e` | `ftcomp_build_marker_model_and_encode_block(...)` | Producer-side/model-building path, not the simple marker expander. |
| `0x0078ee` | `fast_fmemcpy(uint16_t len, void far *src, void far *dst)` | Far memory copy helper; confirms argument order used in the notes. |
| `0x00787e` | `far_memset(void far *dst, int value, uint16_t len)` | Far memory fill helper. |

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

At this point, the DOS and OS/2 tools are understood to:

- Open a PACK2/COMPRESS source bundle.
- Optionally list bundle contents with `/SHOW`.
- Optionally list packed sizes with `/SIZES`.
- Extract all files or a single named file with `/N:specific_file`.
- Write extracted files under an optional destination path.
- Optionally create missing destination directories with `/C`.
- Read PACK2 member headers and dispatch FTCOMP members through the decompressor documented in [FTCOMP Compression Notes](ftcomp.md).

Open questions for further reversing:

- How `/P` maps source paths into destination paths.
- Whether `/SHOW` and `/SIZES` parse all metadata without decompressing file bodies.
- Exact meaning of the non-payload metadata and auxiliary streams in some OS/2 `.pk2` archives.
