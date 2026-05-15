# PACK2 File Format Notes

This document describes the outer PACK2/FTCOMP file structure used by IBM `PACK2` and `UNPACK2`.

It covers the archive/member container. The compressed payload format is documented separately in [FTCOMP Compression Notes](ftcomp.md).

## Sources

These notes are based on:

- IDA Pro analysis of `original/dos/UNPACK2_unpacked.exe`.
- The sample files:
  - `original/examples/DUMMY.TX_`
  - `original/examples/USING.IN_`
- `file(1)`/libmagic recognition of FTCOMP files.
- Public usage-level PACK2/UNPACK2 documentation and examples.

The public `file(1)` magic database recognizes FTCOMP by:

- `FTCOMP` at offset `0x18`.
- Probable magic value `A5 96 FD FF` at offset `0`.
- Original filename string at offset `0x29`.

## Terminology

PACK2 is used in two related forms:

- A single compressed file, often named with a final underscore such as `USING.IN_`.
- A bundle/archive containing one or more member records.

The samples in `original/examples/` are single-member FTCOMP files. The same member header layout is used by `UNPACK2` when scanning bundles.

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
    uint32_t data_end_offset;     // inferred; 0 in current samples
    uint32_t unpacked_size;       // original output size
    uint32_t next_member_offset;  // absolute offset of next member, or 0
    char     method[7];           // NUL-terminated, e.g. "FTCOMP"
    uint16_t method_arg0;         // method-specific; observed 0xcb2b/0x82ea
    uint16_t method_type;         // FTCOMP path requires 1
    uint32_t method_arg1;         // observed 4
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
| `0x0c` | 4 | `data_end_offset` | Inferred alternate absolute end offset for payload sizing. Zero in samples. |
| `0x10` | 4 | `unpacked_size` | Original file size. Used by `/SIZES` and output open/allocation logic. |
| `0x14` | 4 | `next_member_offset` | Absolute offset of next member. Zero for final member. |
| `0x18` | 7 | `method` | NUL-terminated method string. `FTCOMP` in samples. |
| `0x1f` | 2 | `method_arg0` | Unknown method-specific value. |
| `0x21` | 2 | `method_type` | `UNPACK2` requires `1` for FTCOMP decompression. |
| `0x23` | 4 | `method_arg1` | Unknown method-specific value; observed `4`. |
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

For FTCOMP members, the first four payload bytes are a little-endian control/prefix value. In both samples:

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

## Payload Size

`UNPACK2` computes the number of bytes passed to the FTCOMP streaming layer from header offsets and current stream position.

Observed logic:

1. If `data_end_offset != 0`, use it as the end offset.
2. Else if `next_member_offset != 0`, use it as the end offset.
3. Else use the physical file size as the end offset.

Then subtract:

- current/previous member offset state,
- `filename_len`,
- the fixed header size,
- and, in the final-member/file-size case, an additional 4-byte adjustment.

For the single-member samples, the computed FTCOMP byte count excludes four trailing bytes from the physical file length and includes the 4-byte FTCOMP prefix.

Practical implementation guidance:

```c
payload_offset = 0x29 + filename_len;

if (data_end_offset != 0)
    payload_end = data_end_offset;
else if (next_member_offset != 0)
    payload_end = next_member_offset;
else
    payload_end = file_size - 4;       // final-member trailer adjustment

payload_size = payload_end - payload_offset;
```

This formula matches the current single-member samples:

| File | Size | `filename_len` | `payload_offset` | Effective `payload_end` | `payload_size` |
| --- | ---: | ---: | ---: | ---: | ---: |
| `DUMMY.TX_` | 62 | 10 | 51 | 58 | 7 |
| `USING.IN_` | 1739 | 10 | 51 | 1735 | 1684 |

The four physical bytes after `payload_end` are currently not fully identified. In the samples they are present after the effective FTCOMP data region. Treat them as a final-member trailer until more multi-member samples are available.

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
```

## Confidence and Open Questions

High confidence:

- Fixed header size is `0x29`.
- Magic words are `0x96a5` and `0xfffd`.
- `FTCOMP` method string is at `0x18`.
- Filename starts at `0x29`.
- `filename_len` includes the trailing NUL.
- `unpacked_size`, DOS date/time, attributes, and `next_member_offset` meanings are confirmed by IDA use.
- FTCOMP payload begins with a 4-byte prefix before `fT19`/`fT21` or fallback bytes.

Medium confidence:

- `data_end_offset` at `0x0c` is an alternate payload/member end offset. It is used by `UNPACK2` before `next_member_offset` when non-zero, but the current samples do not exercise it.
- Final-member payload sizing subtracts a 4-byte trailer from physical file size. Current samples match this behavior.

Unknown:

- Exact meaning of `method_arg0` at `0x1f`.
- Exact meaning of `method_arg1` at `0x23`.
- Exact contents and purpose of the final 4-byte trailer in single-member files.
- Whether all multi-member bundles use absolute offsets from file start for both `data_end_offset` and `next_member_offset`; IDA behavior strongly suggests this, but the current samples are single-member only.
