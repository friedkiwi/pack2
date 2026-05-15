# FTCOMP Compression Notes

This document describes the FTCOMP decompression algorithm used by IBM `UNPACK2`. It is intended to be detailed enough to guide a clean-room implementation.

The notes are derived from:

- The unpacked DOS `UNPACK2` binary in IDA Pro, especially the renamed `ftcomp_*` functions.
- Public OS/2 references that identify `*.??_` files as IBM FTCOMP files unpacked by `UNPACK2`.
- Public OS/2 PACK2/UNPACK2 usage examples.

See also:

- [UNPACK2 Notes](unpack2.md)
- [EXEPACK Unpacking Notes](exepack.md)

## External References

Public format documentation is sparse. The useful external references found so far are usage-level, not byte-level specs:

- OS/2 FAQ mirror: identifies `????????.??_` files as IBM FTCOMP files and says to unpack them with `UNPACK2 ????????.??_ .`
  - <https://olddos.narod.ru/doc/OS2GEN.HTM>
- OS2World discussion: documents PACK/UNPACK and PACK2/UNPACK2 usage in IBM installer packages.
  - <https://www.os2world.com/forum/index.php?topic=2690.0>
- eCSoft OS/2 Warp Update Kit: shows real `unpack2` usage with `/c`.
  - <https://ecsoft2.org/ibm-os2-warp-update-kit>

No public byte-level FTCOMP specification was found. Treat this document as a reverse-engineered spec.

## Scope

`UNPACK2` handles two cases:

- Stored members: copied directly.
- FTCOMP members: decompressed by the FTCOMP path.

The member is selected as FTCOMP when:

- The member compression/name marker string compares case-insensitively equal to `FTCOMP`.
- The associated member type/value checked by `UNPACK2` is `1`.

IDA function:

```text
is_ftcomp_member
```

The member-level PACK2 container format is separate from FTCOMP. FTCOMP begins at the compressed member payload.

## Stream Tags

The FTCOMP payload may start with a 4-byte tag:

```text
fT19
fT21
```

Observed behavior:

- `fT21` selects FTCOMP format version `2`.
- `fT19` selects FTCOMP format version `1`.
- If the current decompressor mode expects FTCOMP and the first four bytes do not match either tag, the buffer is treated as uncompressed.
- For tagged streams, block decoding starts after the 4-byte tag.

IDA function:

```text
ftcomp_decompress_buffer
```

## High-Level Pipeline

For tagged compressed data, the decompressor performs these stages:

1. Initialize static decode tables.
2. Decode one FTCOMP block into an intermediate stream.
3. Expand marker/back-reference records in the intermediate stream.
4. For `fT21`, run an additional RLE-like expansion pass.
5. Return the number of compressed bytes consumed and output bytes produced.

At the streaming API level, `UNPACK2` maintains pending output because one FTCOMP decode can produce more bytes than the caller requested.

IDA functions:

```text
ftcomp_begin_decode
ftcomp_decode_chunk
ftcomp_decompress_buffer
```

## Block Format

After the optional `fT19`/`fT21` stream tag, the payload is a sequence of blocks. The IDA code decodes one block per call.

Each block begins with a little-endian 16-bit word:

```text
uint16 block_output_target;
```

There are two block forms.

### Raw Block

If `block_output_target == 0xffff`, the block is stored/raw:

```text
uint16 marker;      // 0xffff
uint16 byte_count;
uint8  data[byte_count];
```

Decoder behavior:

```c
if (marker == 0xffff) {
    copy data[0:byte_count] to output;
    compressed_bytes_consumed = byte_count + 4;
}
```

### Compressed Block

If `block_output_target < 0xffff`, the block is compressed:

```text
uint16 intermediate_target;
uint8  literal_weight_a;
uint8  marker_weight_a;
uint8  literal_weight_b;
uint8  marker_weight_b;
uint8  bitstream[];
```

Observed field usage:

- `intermediate_target` is the number of bytes to generate in the first intermediate stream.
- The four one-byte weight fields are used when rebuilding adaptive Huffman tables.
- The bitstream starts at block offset `6`.
- `compressed_bytes_consumed` is computed from the internal input cursor after decoding, plus the 4-byte stream tag at the top level.

IDA function:

```text
ftcomp_expand_block_to_buffer
```

## Bit Order

Bits are consumed MSB-first.

The decoder keeps a 16-bit bit buffer and a count of available bits. When the count drops to 8 or fewer bits, it appends whole input bytes into the low side by shifting the byte into position:

```c
while (bits_available <= 8) {
    bitbuf |= next_byte << (8 - bits_available);
    bits_available += 8;
}
```

To consume `n` bits:

```c
value = top n bits of bitbuf;
bitbuf <<= n;
bits_available -= n;
```

The Huffman fast path indexes tables with the top 9 bits or top 7 bits depending on the table being used.

## Huffman Tables

FTCOMP uses generated binary Huffman decode tables. The implementation in `UNPACK2` stores:

- Symbol weights/frequencies.
- Sorted symbol indexes.
- Tree nodes.
- Fast decode lookup tables for the high bits of the bit buffer.

Important constants:

```text
0x6c4  internal node threshold
0x1b1  number of model symbols decoded for the block model
0x100  zero-run symbol while decoding the model
```

The decoder treats table entries below `0x6c4` as leaf symbols. Internal nodes are `>= 0x6c4` and require additional bit walking.

Generic decode operation:

```c
symbol = fast_table[bitbuf >> 7];
nbits = fast_len_table[bitbuf >> 7];
consume(nbits);

while (symbol >= 0x6c4) {
    bit = read_bit();
    symbol = tree_child[symbol + bit];
}

symbol >>= 2;
```

There are multiple table families:

- A static table used to decode the per-block model.
- A dynamic/adaptive table rebuilt from the per-block model.
- Saved table snapshots used for the second-level state-dependent decode path.

IDA functions:

```text
sort_model_symbols_by_weight
build_huffman_decode_tables
rebuild_adaptive_huffman_tables
```

## Per-Block Model Decode

Compressed blocks begin by decoding `0x1b1` model entries into a frequency/model table.

Pseudocode:

```c
for (i = 0; i < 0x1b1; ) {
    sym = decode_symbol_with_static_model();

    if (sym == 0x100) {
        // Zero run. Up to 16 zero entries are emitted, bounded by 0x1b1.
        count = min(16, 0x1b1 - i);
        repeat count times:
            model[i++] = 0;
    } else {
        model[i++] = low_byte(sym);
    }
}

rebuild_adaptive_huffman_tables(model, block_weight_fields);
```

The adaptive table rebuild multiplies non-zero model entries by one of two weight bytes. Which weight byte is used depends on an auxiliary symbol-class table. `fT21` has two sets of weight bytes; if both sets are equal or the second set is zero, it reuses the first generated table.

Implementation guidance:

- Preserve model entries exactly as decoded.
- When scaling weights, if the maximum scaled weight exceeds `0xff`, scale all weights down so the maximum fits in one byte.
- If a non-zero source weight scales to zero, clamp it back to `1`.

## Main Intermediate Stream Decode

After the model has been decoded and adaptive tables rebuilt, the block bitstream produces an intermediate stream. The loop runs until `intermediate_target` bytes have been written.

The intermediate stream contains:

- Literal bytes.
- `0x9e` marker records.
- Two-byte records used later by marker expansion.
- State-dependent references to recent values.

Important marker:

```text
0x9e
```

For normal literals:

```c
emit literal byte;
if literal == 0x9e and version == fT21:
    emit 0xff;   // escape literal marker for later marker-expansion stage
```

For encoded references, the decoder emits `0x9e` followed by control bytes. These are expanded in the marker stage described below.

The main decoder keeps recent-value state for several classes. Encoded symbols `0x100` and, in `fT21`, `0x101`, refer to recent values instead of directly carrying a new value. This is similar to move-to-front coding:

- `0x100` means repeat the most recent value for that class.
- `0x101` means use the second-most-recent value for that class.
- Other values are adjusted around the recent values so the code space does not waste symbols on the two recency aliases.

The exact classes are selected by short prefix codes from the bitstream. In implementation terms, the decoder has separate recent-value slots for:

- Single-byte literal class.
- Several short-distance/length classes.
- A six-symbol rolling class used by the version-2 path.

The IDA names for these local slots are still synthetic, so a clean implementation should validate this part against known compressed samples.

## Marker Expansion

The marker expansion pass converts the intermediate `0x9e` records into final bytes.

IDA functions:

```text
ftcomp_expand_marker_runs
ftcomp_expand_marker_stream
```

The inner marker-stream format is clear.

Marker byte:

```text
0x9e
```

Escape value:

```text
fT19: 0x40
fT21: 0xff
```

Expansion rules:

```c
while input remains:
    b = *src++;

    if b != 0x9e:
        *dst++ = b;
        continue;

    code = *src++;

    if code == escape_value:
        *dst++ = 0x9e;
        continue;

    if code == 0x80:
        length = src[0] + 0x43;
        distance = read_le16(src + 1);
        src += 3;
    } else if (code & 0x40) {
        length = (code & 0x3f) + 3;
        distance = read_le16(src);
        src += 2;
    } else {
        length = code + 3;
        distance = src[0];
        src += 1;
    }

    copy_from = dst - distance - 1;
    repeat length times:
        *dst++ = *copy_from++;
}
```

Distance is therefore encoded as "distance minus one" in the stream.

This pass is LZSS-like and must allow overlap during the copy.

## Version-2 RLE Pass

For `fT21`, after marker expansion, `UNPACK2` runs an additional RLE-like pass.

IDA function:

```text
ftcomp_expand_rle_runs
```

The pass begins by reading the first byte from the post-marker stream:

```text
uint8 rle_marker;
```

If `rle_marker == 0xff`, the pass degenerates into a direct copy of the remaining bytes.

Otherwise:

1. Read a little-endian 16-bit offset/count field after the marker.
2. Build a frequency table over part of the source stream.
3. Sort byte values by frequency.
4. Use the sorted order to map marker escapes to run lengths.

The core expansion logic is:

```c
while input remains:
    b = *src++;

    if b != rle_marker:
        *dst++ = b;
        continue;

    rank_source_byte = byte from a side stream;
    rank = rank_table[rank_source_byte];

    if rank == 0xff:
        *dst++ = rle_marker;
    } else {
        value = *src++;
        count = rank + 4;
        repeat count times:
            *dst++ = value;
    }
```

Important implementation note: the exact split between the main stream and the side stream is controlled by the 16-bit field read immediately after `rle_marker`. In IDA, the decompressor computes:

```c
side_stream = input_start - side_offset + output_base;
main_stream = input_start + 3;
```

and consumes bytes from both during expansion. This area should be validated with sample `fT21` files before treating the implementation as final.

## Output Chunking

The public decoder used by `UNPACK2` is streaming. It may produce more decompressed bytes than the caller requested. It therefore stores extra bytes in a pending-output buffer and drains that buffer first on the next call.

Observed states returned by `ftcomp_decode_chunk`:

```text
0 = success / finished
1 = caller output buffer too small
2 = more input remains after this output buffer
3 = no input and no pending output
```

The exact numeric meaning is inferred from control flow and should be kept internal to a reimplementation.

## Clean-Room Implementation Plan

A clean implementation should be structured as:

```text
Pack2 member parser
  -> detects FTCOMP member payload
  -> calls ftcomp_decode_stream(payload)

ftcomp_decode_stream
  -> read optional fT19/fT21 tag
  -> loop over FTCOMP blocks
  -> for each block:
       if raw block:
           append raw bytes
       else:
           decode model
           rebuild Huffman tables
           decode intermediate stream
           expand 0x9e marker records
           if fT21:
               apply RLE pass
           append output
```

Suggested modules:

```text
bitreader.go / bitreader.c
huffman.go / huffman.c
ftcomp_model.go / ftcomp_model.c
ftcomp_marker.go / ftcomp_marker.c
ftcomp_rle.go / ftcomp_rle.c
ftcomp.go / ftcomp.c
```

Validation strategy:

1. Add tests for marker expansion independently.
2. Add tests for raw FTCOMP blocks.
3. Add tests for `fT19` and `fT21` tag detection.
4. Compare full decompression output against IBM `UNPACK2` for known PACK2/FTCOMP samples.
5. Add trace logging for:
   - block start offset
   - block type
   - compressed bytes consumed
   - intermediate bytes produced
   - marker-expanded bytes produced
   - final bytes produced

## Known Open Questions

These items are not yet fully confirmed:

- The semantic names of the four compressed-block weight bytes.
- The exact field name for `intermediate_target`; behavior says it bounds the first-stage intermediate output.
- The exact main-stream recent-value classes. The behavior is visible in IDA, but the cleanest written spec needs sample-driven confirmation.
- The exact version-2 RLE side-stream split field name and all edge cases.

The `0x9e` marker expansion format is the most certain part of the algorithm and should be implemented first.
