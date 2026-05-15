# PACK2 File Format Notes

This document describes the outer PACK2/FTCOMP file structure used by IBM `PACK2` and `UNPACK2`.

It covers the archive/member container. The compressed payload format is documented separately in [FTCOMP Compression Notes](ftcomp.md).

## Sources

These notes are based on:

- IDA Pro analysis of `original/dos/UNPACK2_unpacked.exe`.
- IDA Pro analysis of the OS/2 NE `original/os2/UNPACK2.EXE`, which confirms the same member scanner, header reader, payload-bound calculation, and FTCOMP extraction path with different implementation addresses.
- The sample files:
  - `original/examples/DUMMY.TX_`
  - `original/examples/USING.IN_`
  - `original/examples/EVALUATE.LI_`
  - `original/examples/dvxp.pk2`
  - `original/examples/fontutil.pk2`
  - `original/examples/os2drv.pk2`
- `file(1)`/libmagic recognition of FTCOMP files.
- Public usage-level PACK2/UNPACK2 documentation and examples.

The public `file(1)` magic database recognizes FTCOMP by:

- `FTCOMP` at offset `0x18`.
- Probable magic value `A5 96 FD FF` at offset `0`.
- Original filename string at offset `0x29`.

## OS/2 UNPACK2 Code Map

The loaded OS/2 `UNPACK2.EXE` binary maps to the container-level documentation as follows. Function names are descriptive IDB names assigned during analysis.

| Address | Function | File-format role |
| ---: | --- | --- |
| `0x000b40` | `process_archive_members(...)` | Main member-processing loop for list and extract operations. |
| `0x002154` | `read_pack2_member_header(...)` | Reads the 0x29-byte fixed header and variable filename. |
| `0x002314` | `validate_pack2_member_magic(...)` | Checks `magic0 == 0x96a5` and `magic1 == 0xfffd`. |
| `0x002397` | `compute_pack2_payload_bounds(...)` | Chooses the primary payload end from `data_end_offset`, `next_member_offset`, or the final-member file-size rule. |
| `0x002556` | `unpack_ftcomp_member(...)` | Sends an FTCOMP member payload through the FTCOMP decoder. |
| `0x00282e` | `copy_stored_member(...)` | Copies a stored/non-FTCOMP member payload. |
| `0x0040a9` | `extract_pack2_member_payload(...)` | Performs the member payload read/extract step. |
| `0x00465e` | `scan_pack2_archive(...)` | Enumerates members by following absolute `next_member_offset` values. |

This OS/2 binary confirms that the outer PACK2 container logic is separate from the FTCOMP payload decoder. The same member header fields drive both raw/stored extraction and FTCOMP extraction.

## Terminology

PACK2 is used in two related forms:

- A single compressed file, often named with a final underscore such as `USING.IN_`.
- A bundle/archive containing one or more member records.

The `*.??_` samples in `original/examples/` are single-member FTCOMP files. The `.pk2` samples are multi-member bundles. The same member header layout is used in both cases.

## Byte Order

All multi-byte fields observed in `UNPACK2` are little-endian.

The first four bytes are often displayed by magic databases as big-endian `0xA596FDFF`, but `UNPACK2` checks them as two little-endian words:

```text
offset 0x00: 0x96a5
offset 0x02: 0xfffd
```

## Member Layout

Each member record has:

```text
fixed header       0x29 bytes
filename           filename_len bytes, includes terminating NUL
compressed payload variable
```

The next member begins at `next_member_offset` when that field is non-zero. A zero next offset marks the last member.

## Fixed Header

The fixed header is 41 bytes.

```c
struct pack2_member_header {
    uint16_t magic0;              // 0x96a5
    uint16_t magic1;              // 0xfffd
    uint16_t dos_date;            // DOS file date
    uint16_t dos_time;            // DOS file time
    uint16_t file_attrs;          // DOS file attributes
    uint16_t reserved_0a;         // observed 0
    uint32_t data_end_offset;     // absolute end of primary file-data stream, or 0
    uint32_t unpacked_size;       // original output size
    uint32_t next_member_offset;  // absolute offset of next member, or 0
    char     method[7];           // NUL-terminated, e.g. "FTCOMP"
    uint16_t method_arg0;         // method-specific; observed 0xcb2b/0x82ea
    uint16_t method_type;         // FTCOMP path requires 1
    uint32_t method_arg1;         // method/auxiliary-data value; often 4
    uint16_t filename_len;        // bytes at offset 0x29, includes NUL
};
```

Field table:

| Offset | Size | Name | Meaning |
| --- | ---: | --- | --- |
| `0x00` | 2 | `magic0` | Must be `0x96a5`. |
| `0x02` | 2 | `magic1` | Must be `0xfffd`. |
| `0x04` | 2 | `dos_date` | DOS date passed to `_dos_setftime`. |
| `0x06` | 2 | `dos_time` | DOS time passed to `_dos_setftime`. |
| `0x08` | 2 | `file_attrs` | DOS file attributes passed to `_dos_setfileattr`. |
| `0x0a` | 2 | `reserved_0a` | Observed zero. |
| `0x0c` | 4 | `data_end_offset` | Absolute end offset of the primary file-data payload when non-zero. |
| `0x10` | 4 | `unpacked_size` | Original file size. Used by `/SIZES` and output open/allocation logic. |
| `0x14` | 4 | `next_member_offset` | Absolute offset of next member. Zero for final member. |
| `0x18` | 7 | `method` | NUL-terminated method string. `FTCOMP` in samples. |
| `0x1f` | 2 | `method_arg0` | Unknown method-specific value. |
| `0x21` | 2 | `method_type` | `UNPACK2` requires `1` for FTCOMP decompression. |
| `0x23` | 4 | `method_arg1` | Method-specific value. Observed `4`, `0x31`, and `0x14d`; non-`4` values correlate with auxiliary metadata streams in current samples. |
| `0x27` | 2 | `filename_len` | Filename byte length including trailing NUL. |
| `0x29` | n | `filename` | Stored output path/name, NUL-terminated. |

## Filename

The filename starts immediately after the fixed header:

```text
filename_offset = 0x29
payload_offset = 0x29 + filename_len
```

`filename_len` includes the terminating NUL.

Examples:

```text
DUMMY.TXT\0  -> filename_len = 10
USING.INF\0  -> filename_len = 10
```

The stored name may include a directory or drive-like path in real IBM installer bundles. `UNPACK2` can list it directly with `/SHOW`, use its basename for `/N`, and create directories when `/C` is enabled.

## Payload

The member payload begins at:

```text
payload_offset = 0x29 + filename_len
```

For FTCOMP members, the first four payload bytes are a little-endian control/prefix value. In all current FTCOMP samples:

```text
80 60 00 00
```

`UNPACK2` reads this as a 32-bit value before processing the FTCOMP stream. The value is used to size internal pending-output storage in the streaming decoder.

The actual FTCOMP stream starts four bytes later:

```text
ftcomp_stream_offset = payload_offset + 4
```

The FTCOMP stream may start with:

- `fT19`
- `fT21`
- no tag, in which case the FTCOMP decoder treats the data as stored/uncompressed fallback bytes.

See [FTCOMP Compression Notes](ftcomp.md) for the payload decompression algorithm.

## Primary and Auxiliary Payloads

Each member has a primary FTCOMP payload that decompresses to the file bytes named by the member header. Some archive members also carry an auxiliary FTCOMP payload after the primary one. The new `.pk2` bundle samples show this pattern in OS/2 driver archives.

When `data_end_offset == 0`, the primary payload runs to:

- `next_member_offset`, for non-final members.
- `file_size - 4`, for final members.

When `data_end_offset != 0`, the primary payload runs only to `data_end_offset`. If there are bytes after that before the next member or final trailer, those bytes form an auxiliary FTCOMP stream. The auxiliary stream has the same 4-byte `80 60 00 00` prefix and then usually begins with `fT19`.

Observed auxiliary streams contain OS/2 metadata-like records such as `.TYPE`, `.APP`, `.ICON`, and `CHECKSUM`. These are not part of the primary file content and should not be appended to the extracted file bytes.

Examples:

```text
os2drv.pk2 member 0:
  payload_offset       0x00003e
  data_end_offset      0x0047c6
  next_member_offset   0x004803
  primary payload      [0x00003e, 0x0047c6)
  auxiliary payload    [0x0047c6, 0x004803)

dvxp.pk2 member 0:
  payload_offset       0x00003e
  data_end_offset      0x027a29
  next_member_offset   0
  primary payload      [0x00003e, 0x027a29)
  auxiliary payload    [0x027a29, file_size - 4)
```

The `os2drv.pk2` auxiliary payload starts:

```text
80 60 00 00 66 54 31 39 ff ff 31 00 ...
```

That is a normal member-level FTCOMP prefix, an `fT19` tag, and a raw FTCOMP block of `0x31` bytes. The decoded bytes include OS/2 metadata records, not primary file data.

## Payload Size

`UNPACK2` computes the number of bytes passed to the primary FTCOMP streaming layer from header offsets and current stream position. In the OS/2 binary this logic is concentrated in `compute_pack2_payload_bounds` at `0x002397`.

Observed logic:

1. If `data_end_offset != 0`, use it as the primary file-data end offset.
2. Else if `next_member_offset != 0`, use it as the end offset.
3. Else use the physical file size as the end offset.

Then subtract:

- current/previous member offset state,
- `filename_len`,
- the fixed header size,
- and, in the final-member/file-size case, an additional 4-byte adjustment.

For final members without a following `next_member_offset`, the computed FTCOMP byte count excludes four trailing bytes from the physical file length and includes the 4-byte FTCOMP prefix.

Practical implementation guidance:

```c
payload_offset = 0x29 + filename_len;

if (data_end_offset != 0)
    payload_end = data_end_offset;
else if (next_member_offset != 0)
    payload_end = next_member_offset;
else
    payload_end = file_size - 4;       // final-member trailer adjustment

primary_payload_size = payload_end - payload_offset;
```

This formula matches the current samples:

| File/member | Size | `filename_len` | `payload_offset` | Primary payload end | Primary payload size |
| --- | ---: | ---: | ---: | ---: | ---: |
| `DUMMY.TX_` | 62 | 10 | 51 | 58 | 7 |
| `USING.IN_` | 1739 | 10 | 51 | 1735 | 1684 |
| `EVALUATE.LI_` | 487 | 13 | 54 | 483 | 429 |
| `fontutil.pk2` / `BINCTRL.DLL` | 32114 | 12 | 53 | 3709 | 3656 |
| `fontutil.pk2` / `radioa2.bmp` | 32114 | 12 | 31937 | 32110 | 173 |
| `os2drv.pk2` / `\os2\dll\bvhsvga.dll` | 230665 | 21 | 62 | 18374 | 18312 |
| `dvxp.pk2` / `\os2\dll\ibms332.dll` | 162574 | 21 | 62 | 162345 | 162283 |

For final members, the four physical bytes after the final effective data region are still not fully identified. Treat them as a final-member trailer. If `data_end_offset != 0`, the final effective data region can include an auxiliary FTCOMP stream between `data_end_offset` and `file_size - 4`.

## End-of-Archive and Member Chaining

For bundles, `next_member_offset` is the primary way to locate the next member.

`UNPACK2` behavior:

- Reads `0x29` bytes for the fixed header.
- Validates `magic0 == 0x96a5`.
- Validates `magic1 == 0xfffd`.
- Reads `filename_len` more bytes to obtain the stored filename.
- Processes or lists the member.
- Seeks to `next_member_offset` if it is non-zero.
- Stops when `next_member_offset == 0`.

For `/N:specific_file`, `UNPACK2` scans members by following `next_member_offset`, compares the basename case-insensitively, and extracts only the selected member.

## Sample Validation

### `original/examples/DUMMY.TX_`

Hex header:

```text
00000000: a5 96 fd ff 7f 1e 8d 78 20 00 00 00 00 00 00 00
00000010: 03 00 00 00 00 00 00 00 46 54 43 4f 4d 50 00 2b
00000020: cb 01 00 04 00 00 00 0a 00 44 55 4d 4d 59 2e 54
00000030: 58 54 00 ...
```

Parsed:

```text
magic0            0x96a5
magic1            0xfffd
dos_date          0x1e7f -> 1995-03-31
dos_time          0x788d -> 15:04:26
file_attrs        0x0020
unpacked_size     3
next_member       0
method            FTCOMP
method_arg0       0xcb2b
method_type       1
method_arg1       4
filename_len      10
filename          DUMMY.TXT
payload_offset    51
effective payload 7 bytes
```

Payload:

```text
80 60 00 00 0d 0a 1a
```

The 4-byte prefix is followed by three stored fallback bytes. There is no `fT19`/`fT21` tag.

### `original/examples/USING.IN_`

Hex header:

```text
00000000: a5 96 fd ff 78 24 40 19 20 00 00 00 00 00 00 00
00000010: 86 0a 00 00 00 00 00 00 46 54 43 4f 4d 50 00 ea
00000020: 82 01 00 04 00 00 00 0a 00 55 53 49 4e 47 2e 49
00000030: 4e 46 00 ...
```

Parsed:

```text
magic0            0x96a5
magic1            0xfffd
dos_date          0x2478 -> 1998-03-24
dos_time          0x1940 -> 03:10:00
file_attrs        0x0020
unpacked_size     2694
next_member       0
method            FTCOMP
method_arg0       0x82ea
method_type       1
method_arg1       4
filename_len      10
filename          USING.INF
payload_offset    51
effective payload 1684 bytes
```

Payload starts with:

```text
80 60 00 00 66 54 31 39 ...
```

The FTCOMP stream after the 4-byte prefix starts with `fT19`.

### `original/examples/EVALUATE.LI_`

`EVALUATE.LI_` validates the same container layout, but unlike the earlier samples, its FTCOMP stream uses a compressed block.

Hex header and stream start:

```text
00000000: a5 96 fd ff 78 24 40 19 20 00 00 00 00 00 00 00
00000010: ab 02 00 00 00 00 00 00 46 54 43 4f 4d 50 00 00
00000020: 00 01 00 04 00 00 00 0d 00 45 56 41 4c 55 41 54
00000030: 45 2e 4c 49 43 00 80 60 00 00 66 54 31 39 e3 01
00000040: e0 9d 61 1e ...
```

Parsed:

```text
magic0            0x96a5
magic1            0xfffd
dos_date          0x2478 -> 1998-03-24
dos_time          0x1940 -> 03:10:00
file_attrs        0x0020
unpacked_size     683
next_member       0
method            FTCOMP
method_arg0       0x0000
method_type       1
method_arg1       4
filename_len      13
filename          EVALUATE.LIC
payload_offset    54
effective payload 429 bytes
```

Payload interpretation:

```text
80 60 00 00    member-level FTCOMP prefix
66 54 31 39    FTCOMP stream tag, "fT19"
e3 01          first block compressed intermediate target, 0x01e3 bytes
e0 9d 61 1e    compressed-block weight bytes
```

This sample proves that listing a member and extracting stored/raw FTCOMP blocks is not sufficient for general PACK2 extraction. A complete reader must implement the FTCOMP compressed-block path described in [FTCOMP Compression Notes](ftcomp.md).

### `original/examples/fontutil.pk2`

`fontutil.pk2` is a 15-member bundle. All members use FTCOMP method type `1`, `data_end_offset == 0`, and absolute `next_member_offset` chaining. The final member ends at `file_size - 4`.

Representative members:

| Member | Offset | Name | Unpacked | Next | Payload | First block |
| ---: | ---: | --- | ---: | ---: | ---: | --- |
| 0 | `0x000000` | `BINCTRL.DLL` | 6176 | `0x000e7d` | 3656 | `target=0x11d4`, weights `01 01 01 01` |
| 1 | `0x000e7d` | `BLKCRINK.BMP` | 20118 | `0x002470` | 5565 | `target=0x2542`, weights `5a 0e f0 a4` |
| 10 | `0x00687b` | `OS2FS.EXE` | 7600 | `0x0078b4` | 4102 | `target=0x1400`, weights `ce af 4f 30` |
| 14 | `0x007c8c` | `radioa2.bmp` | 406 | `0` | 173 | `target=0x00a6`, weights `01 01 01 01` |

This bundle is useful for validating archive scanning because it mixes DLLs, BMPs, an EXE, and an INI file while keeping the primary payload-bound rule simple.

### `original/examples/os2drv.pk2`

`os2drv.pk2` is a 14-member bundle. It validates absolute `next_member_offset` chaining, stored names with leading OS/2 path separators, and non-zero `data_end_offset` handling.

Representative members:

| Member | Offset | Name | `data_end_offset` | Next | Primary payload | Auxiliary payload |
| ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 0 | `0x000000` | `\os2\dll\bvhsvga.dll` | `0x0047c6` | `0x004803` | 18312 | 61 |
| 1 | `0x004803` | `\os2\mdos\vsvga.sys` | `0x0130fa` | `0x013137` | 59578 | 61 |
| 3 | `0x0140eb` | `\os2\dll\s3pmi.dll` | `0x01bb9c` | `0x01bbd9` | 31349 | 61 |
| 11 | `0x03193a` | `\os2\install\create.exe` | `0x035b04` | `0x035b41` | 16777 | 61 |
| 13 | `0x03800f` | `\os2\help\s3help.hlp` | `0` | `0` | 1208 | 0 |

The 61-byte auxiliary streams in this sample decode from an `fT19` raw block. Their plaintext includes metadata labels such as `.TYPE` and `.APP`. They are separate from the primary file output.

### `original/examples/dvxp.pk2`

`dvxp.pk2` is a single-member archive with a large primary payload and a larger auxiliary stream:

```text
name                 \os2\dll\ibms332.dll
unpacked_size        362709
payload_offset       0x00003e
data_end_offset      0x027a29
primary payload      162283 bytes
auxiliary payload    225 bytes, ending at file_size - 4
method_arg1          0x014d
```

The auxiliary stream starts with an `fT19` raw block of `0x00d5` bytes. Its decoded records include `.ICON` and `CHECKSUM`, so it is a useful sample for later auxiliary-metadata decoding.

## Minimal Parser Pseudocode

```c
parse_pack2_file(buf, file_size):
    off = 0
    members = []

    while true:
        if off + 0x29 > file_size:
            error "truncated member header"

        h = parse_fixed_header(buf + off)

        if h.magic0 != 0x96a5 || h.magic1 != 0xfffd:
            error "bad PACK2 magic"

        name_off = off + 0x29
        payload_off = name_off + h.filename_len

        if payload_off > file_size:
            error "truncated filename"

        name = read_nul_terminated_string(buf + name_off, h.filename_len)

        if h.data_end_offset != 0:
            payload_end = h.data_end_offset
        else if h.next_member_offset != 0:
            payload_end = h.next_member_offset
        else:
            payload_end = file_size - 4

        if payload_end < payload_off || payload_end > file_size:
            error "bad payload bounds"

        if h.next_member_offset != 0:
            member_end = h.next_member_offset
        else:
            member_end = file_size - 4

        if member_end < payload_end || member_end > file_size:
            error "bad member bounds"

        auxiliary_payload = empty
        if h.data_end_offset != 0 && h.data_end_offset < member_end:
            auxiliary_payload = buf[h.data_end_offset : member_end]

        members.append({
            name: name,
            method: h.method,
            method_type: h.method_type,
            attrs: h.file_attrs,
            dos_date: h.dos_date,
            dos_time: h.dos_time,
            unpacked_size: h.unpacked_size,
            payload_offset: payload_off,
            payload_size: payload_end - payload_off,
            auxiliary_payload_offset: h.data_end_offset if auxiliary_payload is not empty,
            auxiliary_payload_size: len(auxiliary_payload),
        })

        if h.next_member_offset == 0:
            break

        if h.next_member_offset <= off:
            error "non-forward next member offset"

        off = h.next_member_offset

    return members
```

## Minimal Extraction Pseudocode

```c
extract_member(member):
    payload = file[member.payload_offset : member.payload_offset + member.payload_size]

    if member.method == "FTCOMP" && member.method_type == 1:
        prefix = read_le32(payload[0:4])
        ftcomp_stream = payload[4:]
        output = ftcomp_decode(ftcomp_stream, prefix, member.unpacked_size)
    else:
        output = payload

    if len(output) != member.unpacked_size:
        warn or error depending on strictness

    write output to member.name
    set DOS timestamp from member.dos_date/member.dos_time
    set DOS attributes from member.file_attrs

    // If auxiliary_payload_size is non-zero, decode it separately as metadata.
    // Do not append it to output.
```

## Confidence and Open Questions

High confidence:

- Fixed header size is `0x29`.
- Magic words are `0x96a5` and `0xfffd`.
- `FTCOMP` method string is at `0x18`.
- Filename starts at `0x29`.
- `filename_len` includes the trailing NUL.
- `unpacked_size`, DOS date/time, attributes, and `next_member_offset` meanings are confirmed by IDA use.
- The primary FTCOMP payload begins with a 4-byte prefix before `fT19`/`fT21` or fallback bytes.
- Multi-member bundles use absolute `next_member_offset` values from the start of the archive.
- Non-zero `data_end_offset` marks the end of the primary file-data payload in the current `.pk2` samples.
- Bytes between non-zero `data_end_offset` and the next member/final trailer are a separate FTCOMP stream and should be decoded separately from file content.

Medium confidence:

- Final-member payload sizing subtracts a 4-byte trailer from physical file size. Current samples match this behavior.
- Auxiliary streams carry OS/2 extended attributes or installer metadata. The decoded records strongly suggest this, but the exact record grammar is not yet documented.

Unknown:

- Exact meaning of `method_arg0` at `0x1f`.
- Exact meaning of `method_arg1` at `0x23`; values other than `4` correlate with auxiliary payloads but are not yet fully decoded.
- Exact contents and purpose of the final 4-byte trailer in single-member files.
