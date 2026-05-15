# FTCOMP Compression Notes

This document describes the FTCOMP decompression algorithm used by IBM `UNPACK2`. It is intended to be detailed enough to guide a clean-room implementation.

The notes are derived from:

- The unpacked DOS `UNPACK2` binary in IDA Pro, especially the renamed `ftcomp_*` functions.
- The OS/2 `UNPACK2` NE binary in IDA Pro. This independently confirms the unpack-side FTCOMP block decoder, Huffman builder, framed intermediate pass, and marker expansion path with different code and data addresses.
- The OS/2 `PACK2` utility, which includes FTCOMP producer-side code and an embedded FTCOMP decode path used when handling existing packed inputs.
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

In the OS/2 binary, this selection happens in the member extraction path before dispatching to `unpack_ftcomp_member`. Older local labels for the DOS method check should be treated as tentative reverse-engineering names, not portable algorithm names.

The member-level PACK2 container format is separate from FTCOMP. FTCOMP begins at the compressed member payload.

## Implementation Address Maps

The algorithm is shared by the DOS and OS/2 builds, but their addresses and DGROUP layouts are not. Treat addresses as reverse-engineering anchors only; clean implementations should use the byte-level structures and constants in this document.

### OS/2 UNPACK2 Function Map

The attached OS/2 `UNPACK2.EXE` IDB currently maps to these FTCOMP concepts:

| Documentation concept | OS/2 function | Address | Notes |
| --- | --- | ---: | --- |
| Buffer/tag decode | `ftcomp_decompress_buffer` | `0x00a96e` | Checks `fT19`/`fT21`, raw fallback, block decode, and framed post-pass. |
| Tagged-buffer wrapper | `ftcomp_decode_tagged_buffer` | `0x00a592` | Wrapper around the buffer-level decoder. |
| Static/fixed initialization | `ftcomp_init_static_tables` | `0x00a714` | Initializes static and fixed decompressor tables. |
| Raw/compressed block decode | `ftcomp_expand_block_to_buffer` | `0x0091aa` | Reads block marker/target, four weight bytes, model, adaptive tables, and first-stage intermediate bytes. |
| Adaptive rebuild | `ftcomp_rebuild_adaptive_huffman_tables` | `0x008fe2` | Builds first and optional second adaptive tables from the decoded model. |
| Huffman builder | `ftcomp_build_huffman_decode_tables` | `0x008cbe` | Builds tree nodes and 9-bit fast decode tables. |
| Model sorter | `ftcomp_sort_model_symbols` | `0x008a7c` | Non-stable quicksort/insertion-sort helper for model symbols. |
| Framed intermediate pass | `ftcomp_expand_framed_intermediate` | `0x00a69c` | Reads `uint16 segment_len` records from the intermediate stream. |
| Segment expansion | `ftcomp_expand_segment_to_output` | `0x00a622` | Treats byte zero of each segment as the mode byte. |
| Marker expansion | `ftcomp_expand_marker_stream` | `0x0075fe` | Expands `0x9e` marker records. |
| Marker-model encoder | `ftcomp_build_marker_model_and_encode_block` | `0x00993e` | Producer-side/model-building path, not the simple marker expander. |
| Far copy helper | `fast_fmemcpy` | `0x0078ee` | Signature is `fast_fmemcpy(len, src, dst)`. |

### OS/2 UNPACK2 Data Anchors

These are OS/2 implementation offsets observed in the loaded binary. They map to the same concepts as the DOS work-area table below, but should not be copied into a standalone decoder.

| OS/2 address | Meaning |
| ---: | --- |
| `0x6002` | Huffman node work area used by the OS/2 builder. |
| `0x7b32` | Queue/work area during table build and generated fast symbol table area. |
| `0x7f32` | Generated fast bit-length table area. |
| `0x06c4` | Internal-node threshold, shared algorithmically with the DOS build. |
| `0x6d8c` | End of node scan in the OS/2 builder. |
| `0xa262` | Per-block decoded model table target. |
| `0xaa38` | Marker-record history area initialized at compressed-block start. |
| `0xa5d6` | Two-byte history area initialized at compressed-block start. |
| `0x0628` | Marker-history cursor, initialized to `0x20`. |
| `0x062a` | Two-byte-history cursor, initialized to `0x20`. |
| `0xa636` | Compressed input byte cursor; set to `6` after a compressed block header. |

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

Implementation anchor:

```text
DOS/OS2: ftcomp_decompress_buffer
```

## High-Level Pipeline

For tagged compressed data, the decompressor performs these stages:

1. Initialize static decode tables.
2. Decode one FTCOMP block into a framed intermediate stream.
3. Split the intermediate stream into marker-expansion segments.
4. Expand marker/back-reference records in each segment, or copy stored segments directly.
5. For `fT21`, run an additional RLE-like expansion pass.
6. Return the number of compressed bytes consumed and output bytes produced.

At the streaming API level, `UNPACK2` maintains pending output because one FTCOMP decode can produce more bytes than the caller requested.

Implementation anchors:

```text
DOS: ftcomp_begin_decode
DOS: ftcomp_decode_chunk
DOS/OS2: ftcomp_decompress_buffer
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
uint8  marker_weight_b;
uint8  literal_weight_b;
uint8  bitstream[];
```

Observed field usage:

- `intermediate_target` is the number of bytes to generate in the first intermediate stream.
- The four one-byte weight fields are used when rebuilding adaptive Huffman tables.
- The bitstream starts at block offset `6`.
- `compressed_bytes_consumed` is computed from the internal input cursor after decoding, plus the 4-byte stream tag at the top level.

The compressed-byte count is corrected for whole bytes that were prefetched into the bit buffer but not consumed:

```c
compressed_bytes_consumed = input_cursor - (bits_available / 8);
```

The OS/2 block decoder confirms this at `0x98f8..0x98fd`: it shifts the remaining bit count down by three and subtracts that count from the byte cursor before returning.

The OS/2 `UNPACK2` block decoder at `0x0091aa` confirms this layout directly. On the compressed path it stores:

```text
block byte +2 -> literal_weight_a
block byte +3 -> marker_weight_a
block byte +4 -> marker_weight_b
block byte +5 -> literal_weight_b
input cursor  -> 6
```

`original/examples/EVALUATE.LI_` exercises this path. After the PACK2 member prefix, its FTCOMP stream begins:

```text
66 54 31 39 e3 01 e0 9d 61 1e ...
```

Parsed as:

```text
tag                   fT19
intermediate_target   0x01e3 / 483 bytes
literal_weight_a      0xe0
marker_weight_a       0x9d
marker_weight_b       0x61
literal_weight_b      0x1e
final output size     683 bytes, from the PACK2 member header
```

This is the first required negative test for incomplete readers: if a decoder accepts only raw blocks (`ff ff`) and rejects this sample with an error such as "compressed FTCOMP blocks are not implemented", the missing part is not the PACK2 container parser. The missing part is the compressed-block pipeline: model Huffman decode, adaptive table rebuild, main intermediate stream decode, and marker expansion.

The broader sample set now includes many `fT19` compressed blocks with both uniform and non-uniform block weight bytes:

| Sample/member | First block target | Weight bytes |
| --- | ---: | --- |
| `USING.IN_` / `USING.INF` | `0x084f` | `dc ae 50 22` |
| `EVALUATE.LI_` / `EVALUATE.LIC` | `0x01e3` | `e0 9d 61 1e` |
| `fontutil.pk2` / `BINCTRL.DLL` | `0x11d4` | `01 01 01 01` |
| `fontutil.pk2` / `BLKCRINK.BMP` | `0x2542` | `5a 0e f0 a4` |
| `fontutil.pk2` / `OS2FS.EXE` | `0x1400` | `ce af 4f 30` |
| `fontutil.pk2` / `radioa2.bmp` | `0x00a6` | `01 01 01 01` |
| `os2drv.pk2` / `\os2\dll\bvhsvga.dll` | `0x46d0` | `01 01 01 01` |
| `os2drv.pk2` / `\os2\MONITOR.DIF` | `0x066f` | `b8 54 aa 46` |
| `os2drv.pk2` / `\os2\ddc.cmd` | `0x01d6` | `d5 95 69 29` |
| `dvxp.pk2` / `\os2\dll\ibms332.dll` | `0x59a4` | `01 01 01 01` |

This matters for implementation because many real members use the all-ones weighting case, but not all do. A decoder that only happens to work for `01 01 01 01` blocks is not complete. The non-uniform examples exercise the first-table and second-table adaptive rebuild paths described below.

Some PACK2 auxiliary metadata streams also use FTCOMP. For example, `os2drv.pk2` stores a 61-byte auxiliary stream after selected primary payloads:

```text
80 60 00 00 66 54 31 39 ff ff 31 00 ...
```

After the PACK2-level prefix, this is `fT19` followed by a raw block marker `0xffff` and byte count `0x0031`. These auxiliary streams decode separately from primary file data; see [PACK2 File Format Notes](pack2_file_format.md).

Implementation anchor:

```text
DOS/OS2: ftcomp_expand_block_to_buffer
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
while (bits_available < n) {
    bitbuf |= next_byte << (8 - bits_available);
    bits_available += 8;
}

value = top n bits of bitbuf;
bitbuf <<= n;
bits_available -= n;
```

The Huffman fast path indexes tables with the top 9 bits of the 16-bit bit buffer.

Do not use the `<= 8` refill rule inside the slow one-bit Huffman walk. OS/2 appends exactly one byte only when the slow path has no available bits left; prefetching a second byte there changes the byte cursor and the returned consumed count.

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

Implementation anchors:

```text
DOS: sort_model_symbols_by_weight
DOS/OS2: build_huffman_decode_tables
DOS/OS2: rebuild_adaptive_huffman_tables
```

### Table Work Areas

The DOS implementation stores FTCOMP state in global data. The names below are descriptive runtime offsets from the analyzed DOS implementation. OS/2 offsets for the same concepts are listed in [Implementation Address Maps](#implementation-address-maps).

| Address | Size | Meaning |
| --- | ---: | --- |
| `0x528e` | `0x1b30` | Huffman node work area, 870 records of 8 bytes. |
| `0x6dbe` | `0x0400` | Generated fast symbol table, 512 `uint16` entries indexed by the top 9 bits. |
| `0x71be` | `0x0200` | Generated fast bit-length table, 512 `uint8` entries. |
| `0x73be` | `0x0400` | Snapshot of the first adaptive fast symbol table. |
| `0x77be` | `0x0200` | Snapshot of the first adaptive fast length table. |
| `0x79be` | `0x0362` | Per-block model weights, 433 `uint16` entries. |
| `0x82aa` | `0x0060` | Marker-record control-byte offset history, 48 `uint16` slots. |
| `0x870e` | `0x0060` | Recent two-byte literal/history table, 48 `uint16` slots. |
| `0x13f2` | `0x0100` | Marker-control class table indexed by marker control byte. |
| `0x14f2` | `0x01b1` | Symbol class table; non-zero symbols use the second adaptive table. |
| `0x16a4` | `0x0400` | Static/model seed weights at program load; later overwritten with a fast symbol table snapshot. |
| `0x1aa4` | `0x0400` | Tagged-format suffix-decoder seed weights at program load; later overwritten with a fast symbol table snapshot. |
| `0x2574` | `0x1588` | Static/model Huffman node snapshot. |
| `0x3afc` | `0x1588` | Active adaptive Huffman node snapshot. |
| `0x830e` | `0x0200` | Static/model fast bit-length table snapshot. |
| `0x850e` | `0x0200` | Active adaptive fast bit-length table snapshot used by suffix decodes. |

The 8-byte node records at `0x528e` and in the snapshots are:

```c
struct huff_node {
    uint16_t weight;       // scaled frequency/weight
    uint16_t parent;       // parent node id, or 0 while unlinked
    uint16_t child0;       // first merged child for internal nodes
    uint16_t child1;       // second merged child for internal nodes
};
```

The child names are descriptive only. The OS/2 decoder's bit polarity is easy to read backwards: a set input bit selects the first merged child (`child0`), and a clear input bit selects the second merged child (`child1`).

Node ids are stored as byte offsets into the node table, not as dense indexes. Leaf symbol `n` has node id `n * 4`. Internal node ids start at `0x06c4`, which is `433 * 4`.

### Static Huffman Initialization

`ftcomp_init_decompressor(mode, use_static_tables)` records:

```text
byte_29BB2 = mode
byte_2C8CC = use_static_tables
word_27A4C:len = far pointer to an alternate adaptive node snapshot area
```

There are two static initialization phases.

The process-level initializer, `ftcomp_init_decompressor`, builds the model decoder used while reading each compressed block's 433-entry model. When `mode` requires generated static tables, it:

1. Clears `huff_node[870]` at `0x528e`.
2. Copies 433 initial static weights from `0x16a4` into `node[i].weight`.
3. Calls `build_huffman_decode_tables`.
4. Copies the resulting node table and fast tables into the static/model snapshots:

```text
0x528e -> 0x2574, length 0x1588
0x6dbe -> 0x16a4, length 0x0400
0x71be -> 0x830e, length 0x0200
```

The tag-level initializer in `ftcomp_decompress_buffer` runs when the stream switches into `fT19` or `fT21` mode. It builds the static suffix-decoder table set from the seed weights at `0x1aa4`:

```text
0x528e -> 0x3afc, length 0x1588
0x6dbe -> 0x1aa4, length 0x0400
0x71be -> 0x850e, length 0x0200
```

Do not treat runtime `0x1aa4` as already containing a fast table unless this tag-level initialization has run. In the original executable image, `0x1aa4` is another seed-weight array. After initialization it is overwritten with the generated fast symbol table.

Do not use the generated `0x1aa4` table as the model decoder or the primary adaptive decoder. It is used by the pending-suffix paths after a marker control byte requests more bytes. The 433-entry block model is decoded with the static/model table generated from `0x16a4`; primary symbols after the model are decoded with the per-block adaptive tables.

Important call-convention note: IDA's comments around `fast_fmemcpy` can be misleading. Both the DOS and OS/2 implementations consume stack arguments as:

```c
fast_fmemcpy(uint16_t len, void far *src, void far *dst);
```

So the assembly sequence that pushes `dst`, then `src`, then `len` is copying `src -> dst`, even if IDA labels the pushes differently.

The OS/2 function at `0x0078ee` makes this explicit: it loads `len` into `cx`, loads `src` through `lds si`, loads `dst` through `les di`, copies with string operations, and returns with `retf 0x0a`.

`ftcomp_init_static_tables` then resets the decompression byte counter and initializes fixed lookup tables:

- `word_2F8E0 = 0`.
- If `use_static_tables != 0 && mode == 1`, it selects static table block `0xc022`.
- Otherwise it selects static table block `0xbc62`, resets the output byte counter to `0x0000137a`, and fills three fixed arrays:
  - `0xbc64`, 0x140 bytes, value `0x20`.
  - `0xbda4`, 0x140 bytes, value `0xff`.
  - `0xbee4`, 0x140 bytes, value `0x00`.
- It copies 0x0fba bytes from `0x0438` to `0xc024`.
- It stores the selected static table pointer in `word_27A6E`.
- It records the initialized mode in `byte_2F8DA`.

For a clean implementation, these fixed arrays are constants. They are not derived from the compressed input. The generated static/model Huffman tables can be reproduced from the 433 static weights and the table-builder below.

The `fT19` sample `EVALUATE.LI_` needs both generated static table sets: the `0x16a4` seed set for model decode and the `0x1aa4` seed set for pending suffix decode. The required standalone constants are included in [Standalone Constants](#standalone-constants).

### Standalone Constants

The following constants are sufficient to seed the `fT19` compressed path from this document alone. Hex strings are byte streams. Weight arrays are little-endian `uint16` values and contain 433 entries after decoding.

```text
marker_control_class[256] =
0101010101010101010101010101010101010101010101010101010101010101
0101010101010101010101010101010101010101010101010101010101010101
0002020202020202020202020202020202020202020202020202020202020202
0202020202020202020202020202020202020202020202020202020202020202
0300000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
```

```text
symbol_class[433] =
0x000..0x0ff = 0
0x100..0x13f = 1
0x140        = 0
0x141..0x180 = 1
0x181..0x1a0 = 0
0x1a1..0x1b0 = 1
```

This table is an easy source of false first-frame failures. The explicit marker symbols begin at `0x100`, not `0x108`; the two-byte pair/history symbols `0x181..0x1a0` are class zero; marker-history replay symbols `0x1a1..0x1b0` are class one.

There is one important `fT19` exception inside the explicit marker range: symbol `0x140` emits the literal marker escape record `9e 40`, and its class is zero. The next primary symbol must therefore be decoded with the first adaptive table, not the marker/second adaptive table. A QEMU trace of DOS `UNPACK2` on `os2drv.pk2` member `\os2\mdos\vsvga.sys` confirms this: treating `0x140` as class one makes the block-6 intermediate stream diverge at offset `0x12ca`; treating it as class zero matches the DOS-produced framed intermediate and lets the member decode.

```text
static_model_seed_weights_le16[433] =
000458022c010401e600d400c000ac009400840078006c005c00540050004c00
4800440040003c003800340030002c002800240020001c001800160014001300
1200110010000f000e000e000d000d000c000c000b000b000a000a0009000900
0900080008000800070007000700060006000600060005000500050005000400
0400040004000400040003000300030003000300020002000200020002000200
0200020001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000100
0100010001000100010001000100010001000100010001000100010001000400
0f00000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000
```

```text
tagged_model_seed_weights_le16[433] =
2800270027002600260025002500240024002300230022002200210021002000
20001f001f001e001e001d001d001c001c001b001a0019001800180017001700
1600160015001500140014001300130013001200120012001100110011001100
10001000100010001000100010000f000f000f000f000f000f000f000f000e00
0e000e000e000e000e000d000d000d000d000d000d000c000c000c000c000c00
0b000b000b000b000b000a000a000a000a000a000a0009000900090009000900
0900090009000900090009000900090009000900090009000900090009000900
0800080008000800080008000800080008000800080008000800080008000800
0700070007000700070007000700070007000700070007000700070007000700
0600060006000600060006000600060006000600060006000600060006000600
0600060006000600060006000600050005000500050005000500050005000500
0500050005000500050005000500050005000500050005000500050005000500
0500050005000500050005000500050005000500050005000500050005000500
0500050005000500050005000500050005000500040004000400040004000400
0400040004000400040004000400040004000400040004000400040004000400
0400040004000400040004000400040004000400040004000500070007000700
ff00000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000
0000
```

### Generic Huffman Table Builder

`build_huffman_decode_tables` consumes `node[0..432].weight` and rebuilds:

- Parent/child links in the node table.
- A 512-entry fast symbol table.
- A 512-entry fast bit-length table.

The builder treats zero weights as absent symbols. Its initial queue construction has an important special case for weight `1` symbols.

During the scan over 433 leaves:

- `parent` is cleared for every leaf.
- zero-weight leaves are omitted and the most recent zero leaf is remembered.
- weight-1 leaves are inserted into the front portion of the queue.
- weights greater than 1 are appended after the weight-1 region and sorted later.

The weight-1 insertion is not equivalent to collecting all non-zero leaves and stable-sorting them. It uses the queue array itself:

```c
queue_len = 0;
one_count = 0;

for symbol = 0; symbol < 0x1b1; symbol++ {
    node_id = symbol * 4;
    node[symbol].parent = 0;

    if (node[symbol].weight == 0) {
        last_zero_node = node_id;
        continue;
    }

    if (node[symbol].weight == 1) {
        displaced = queue[one_count];
        queue[queue_len++] = displaced;
        queue[one_count++] = node_id;
        continue;
    }

    queue[queue_len++] = node_id;
}
```

After this scan, only `queue[one_count..queue_len-1]` is sorted by weight. The weight-1 prefix `queue[0..one_count-1]` is already in final order. This detail affects Huffman tie ordering and therefore the decoded bitstream.

The DOS sorter is not stable. It is a non-recursive quicksort with insertion sort for partitions of 16 entries or fewer:

- The quicksort partition compares only weights.
- It advances the left side while `weight < pivot`.
- It advances the right side while `weight > pivot`.
- Equal-weight entries can be swapped when the two scans meet.
- The insertion-sort fallback inserts the current entry before the first entry whose weight is greater than or equal to the current weight.

Do not replace this with a stable sort. Stable ordering gives different tie resolution for equal-weight symbols and can produce a different Huffman tree.

Special case: if there is only one non-zero symbol, the builder adds the last zero-weight symbol as a synthetic second leaf with weight `1`.

Implementation-level outline:

```c
if queue_len == 1 {
    node[last_zero_node / 4].weight = 1
    queue[queue_len++] = last_zero_node
}

sort_queue_dos_order(queue[one_count:queue_len])

next_internal = 0x06c4
queue_read = 0
remaining = queue_len

if queue_len != 2 {
    while true {
        remaining--

        left = queue[queue_read]
        right = queue[queue_read + 1]
        search_start = queue_read + 2
        queue_read++

        parent_weight = node[left / 4].weight + node[right / 4].weight

        // Find the first remaining queue entry whose weight is greater than
        // or equal to the parent weight. The search window is
        // queue[search_start:queue_len].
        insert = lower_bound_by_weight(queue, search_start, queue_len, parent_weight)

        // DOS uses fast_fmemcpy to move queue[queue_read + 1:insert] down by
        // one slot, then writes the parent into queue[insert - 1].
        memmove(&queue[queue_read],
                &queue[queue_read + 1],
                (insert - 1 - queue_read) * sizeof(uint16))

        parent = next_internal
        queue[insert - 1] = parent
        next_internal += 4

        node[parent / 4].weight = parent_weight
        node[parent / 4].parent = 0
        node[parent / 4].child0 = left
        node[parent / 4].child1 = right
        node[left / 4].parent = parent
        node[right / 4].parent = parent

        if remaining == 2
            break
    }
}

left = queue[queue_read]
right = queue[queue_read + 1]
root = next_internal
node[root / 4].weight = node[left / 4].weight + node[right / 4].weight
node[root / 4].parent = 0
node[root / 4].child0 = left
node[root / 4].child1 = right
node[left / 4].parent = root
node[right / 4].parent = root
word_2F8D6 = root
word_2F8D8 = 0
```

This is not a normal priority queue and not equivalent to repeatedly popping two entries from a slice and reinserting the parent into the shortened slice. The DOS builder keeps the queue array length fixed, treats `queue_read` as the front of the live window, consumes only one slot per merge, and overlays the generated parent into the slot before the lower-bound insertion point. That mutation order affects ties among generated internal nodes and can change the compressed bitstream immediately.

After the tree is built, the DOS implementation fills the 9-bit fast tables when the active global table mode requests generated fast tables. Otherwise the same table shape is supplied from the prebuilt static snapshots.

```c
for prefix = 0; prefix < 512; prefix++ {
    bits = prefix << 7;       // top 9 bits in a 16-bit bit buffer
    nbits = 0;
    node_id = root;

    while node_id >= 0x06c4 && nbits < 9 {
        if (bits & 0x8000)
            node_id = node[node_id / 4].child0;
        else
            node_id = node[node_id / 4].child1;
        bits <<= 1;
        nbits++;
    }

    fast_symbol[prefix] = node_id;
    fast_nbits[prefix] = nbits;
}
```

If `fast_symbol[prefix]` is still an internal node, the decoder consumes the fast bits and continues walking the tree one bit at a time.

### Adaptive Table Rebuild

Compressed blocks begin by decoding 433 model weights into `model[0x1b1]` at `0x79be`. `rebuild_adaptive_huffman_tables` then builds one or two adaptive tables from that model.

The first adaptive table uses the first two block weight bytes:

```c
for symbol = 0; symbol < 0x1b1; symbol++ {
    m = model[symbol];
    if (m == 0) {
        node[symbol].weight = 0;
        continue;
    }

    if (symbol_class[symbol] != 0)
        w = m * marker_weight_a;       // byte at block offset +3
    else
        w = m * literal_weight_a;      // byte at block offset +2

    node[symbol].weight = w;
    max_weight = max(max_weight, w);
}

scale_weights_to_byte_range(node, max_weight);
build_huffman_decode_tables();
```

Scaling is exact:

```c
if (max_weight > 0xff) {
    scale = 0xffff / max_weight;
    for each node weight {
        old = weight;
        weight = low16(weight * scale) >> 8;
        if (old != 0 && weight == 0)
            weight = 1;
    }
}
```

The `low16` detail follows the 16-bit OS/2 code: the multiply instruction leaves the low 16-bit product in `AX`, then the scaler stores `AH` as the new byte-sized weight. A clean implementation should not use the high bits of the full mathematical product, even though the selected scale normally keeps the product inside 16 bits.

The generated first table is saved:

```text
0x528e -> adaptive_node_snapshot_0, length 0x1b30
0x6dbe -> 0x73be, length 0x0400
0x71be -> 0x77be, length 0x0200
```

If the second pair of block weight bytes is identical to the first pair, or if both second-pair bytes are zero, the first table is reused. Otherwise the second adaptive table is built the same way, but the second pair is stored in marker/literal order:

```text
marker_weight_b  = block byte +4
literal_weight_b = block byte +5
```

This ordering is easy to get wrong because the first pair is literal/marker while the second pair is marker/literal. In the DOS rebuild routine, the first table uses byte `+2` for `symbol_class == 0` and byte `+3` for `symbol_class != 0`; the second table uses byte `+5` for `symbol_class == 0` and byte `+4` for `symbol_class != 0`.

The OS/2 `UNPACK2` binary and the OS/2 `PACK2` utility contain the same FTCOMP decode-side rebuild sequence: they read compressed block bytes `+2`, `+3`, `+4`, and `+5` into four globals, then apply the first pair as literal/marker and the second pair as marker/literal when rebuilding the two adaptive tables. This independently confirms the byte order above.

The second generated table remains in the active work areas:

```text
0x528e, 0x6dbe, 0x71be
```

The main symbol decoder chooses between the first and second adaptive tables using `symbol_class[symbol]`.

## Per-Block Model Decode

Compressed blocks begin by decoding `0x1b1` model entries into a frequency/model table.

Pseudocode:

```c
for (i = 0; i < 0x1b1; ) {
    sym = decode_symbol(
        static_fast_symbol,       // 0x16a4
        static_fast_nbits,        // 0x830e
        static_nodes              // 0x2574
    );

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

The zero-run symbol always emits exactly 16 zero model entries unless fewer than 16 entries remain in the 433-entry model.

For `fT19`, this model decode uses the process-level static/model table generated from the `0x16a4` seed weights. The separate tag-level table generated from `0x1aa4` is used later for pending suffix symbols, not for this 433-symbol model decode.

The adaptive table rebuild multiplies non-zero model entries by one of two weight bytes. Which weight byte is used depends on an auxiliary symbol-class table. `fT21` has two sets of weight bytes; if both sets are equal or the second set is zero, it reuses the first generated table.

Implementation guidance:

- Preserve model entries exactly as decoded.
- When scaling weights, if the maximum scaled weight exceeds `0xff`, scale all weights down so the maximum fits in one byte.
- If a non-zero source weight scales to zero, clamp it back to `1`.

## Main Intermediate Stream Decode

After the model has been decoded and adaptive tables rebuilt, the block bitstream produces a framed intermediate stream. The loop runs until `intermediate_target` bytes have been written.

The intermediate stream is not passed to marker expansion as one flat byte slice. It is a sequence of framed segments:

```text
uint16 segment_len;       // little-endian, number of bytes following
uint8  segment_mode;      // included in segment_len
uint8  segment_data[segment_len - 1];
```

`UNPACK2` consumes exactly `2 + segment_len` bytes per segment until the block's `intermediate_target` byte count is exhausted.

Segment behavior:

```c
while intermediate bytes remain:
    segment_len = read_le16(p);
    p += 2;

    segment = p[0:segment_len];
    p += segment_len;

    if segment[0] == 0:
        append segment[1:] directly to block output;
    else:
        append expand_marker_stream(segment[1:]) to block output;
```

This framing is important. If a decoder feeds the whole `intermediate_target` bytes directly to the marker expander, the segment length and mode bytes will be misinterpreted as compressed marker data. `EVALUATE.LI_` then fails early with a bogus back-reference such as "marker offset 3 length 52 distance 97 output 0". That error means the segment framing was skipped or the segment boundary is wrong, not that a valid marker may copy before the start of output.

The OS/2 `UNPACK2` binary has the same framing post-pass: after the first-stage block expansion, `ftcomp_expand_framed_intermediate` repeatedly reads a little-endian `uint16` length, copies exactly that many bytes to a temporary buffer, and calls `ftcomp_expand_segment_to_output`. The segment expander decrements the length by one and treats byte zero as the mode flag before either copying the segment data or calling `ftcomp_expand_marker_stream`.

The OS/2 `PACK2` utility's embedded decode path follows the same structure, which gives a second confirmation that framing is part of FTCOMP itself rather than an artifact of one executable.

The first-stage block loop stops when the produced intermediate byte count is greater than or equal to `intermediate_target`. It does not treat a final suffix or two-byte replay that crosses the target by one byte as an immediate decoder error; the actual produced byte count is passed to the framing pass.

If segment framing is implemented but the first `segment_len` is larger than the remaining intermediate buffer, the mismatch is earlier than marker expansion. For `EVALUATE.LI_`, errors such as `truncated intermediate segment at offset 0 length 31224 remaining 481`, `truncated intermediate segment at offset 0 length 51192 remaining 481`, `truncated intermediate segment at offset 0 length 36088 remaining 481`, or `truncated intermediate segment at offset 0 length 13129 remaining 482` mean the first two intermediate bytes were generated incorrectly. The most likely causes are:

- using the primary adaptive table for pending suffix symbols instead of the generated `0x1aa4` suffix table,
- trying to use the generated `0x1aa4` suffix table as the model decoder or primary decoder,
- using a shifted `symbol_class` table when scaling adaptive weights; explicit marker symbols `0x100..0x180` are normally class one, but `0x140` is a required zero-class literal-marker escape exception,
- skipping the tag-level static table initialization after seeing `fT19`,
- copying the generated Huffman tables in the wrong direction because of misleading `fast_fmemcpy` argument comments,
- diverging from the exact in-place `build_huffman_decode_tables` merge loop. In particular, a builder that repeatedly removes two queue entries and reinserts one parent is not equivalent to the DOS queue mutation order documented above,
- or using stable sorting for equal-weight Huffman leaves. The DOS sorter is not stable; changing only this ordering has been observed to change the `EVALUATE.LI_` intermediate prefix.

The DOS binary has been validated as a runtime oracle under FreeDOS in QEMU. Running `UNPACK2` against `original/examples/EVALUATE.LI_` produces `EVALUATE.LIC` byte-for-byte equal to the checked-in decompressed sample. This confirms the fixture and the unpacked DOS binary, and rules out Wine/host-execution artifacts.

A targeted Huffman variant sweep against this fixture ruled out several tempting fixes:

- Swapping the two adaptive table selections changes the prefix but still produces an invalid first frame.
- Swapping the first or second block weight pair changes the prefix but still produces an invalid first frame.
- Changing parent insertion to occur after equal weights can produce a superficially plausible first segment length (`0x00fb` or `0x00e9`), but the segment mode/data are invalid and the following segment length is impossible.
- Reversing child order or using stable whole-queue sorting diverges even earlier.

The DOS `build_huffman_decode_tables` merge loop stops parent insertion at the first queued node whose weight is greater than or equal to the new parent weight. That matches the current documented queue rule; the plausible-frame result from "insert after equal" is a false lead, not an IBM-compatible fix.

Within a marker-expanded segment, `segment_data` contains literal bytes and `0x9e` marker records.

Important marker byte:

```text
0x9e
```

For normal literals:

```c
emit literal byte;
if literal == 0x9e and version == fT21:
    emit 0xff;   // escape literal marker for later marker-expansion stage
```

For encoded references, the decoder emits `0x9e` followed by one to four control bytes. These are expanded in the marker stage described below.

### Primary Symbol Decode

The primary symbol decoder uses a 9-bit fast lookup followed by bit-by-bit tree walking:

```c
symbol_node = fast_symbol[bitbuf >> 7];
nbits = fast_nbits[bitbuf >> 7];
consume(nbits);

while (symbol_node >= 0x06c4) {
    bit = read_bit();
    if (bit)
        symbol_node = node[symbol_node / 4].child0;
    else
        symbol_node = node[symbol_node / 4].child1;
}

symbol = symbol_node >> 2;
```

At the start of a primary decode, `UNPACK2` chooses the first or second adaptive table:

- If the current sub-state flag is zero, it decodes through the first adaptive snapshot (`0x73be`, `0x77be`, and `adaptive_node_snapshot_0`).
- If the sub-state flag is non-zero, it decodes through the active second table (`0x6dbe`, `0x71be`, and `0x528e`).

After a symbol is decoded, `symbol_class[symbol]` from `0x14f2` becomes the next sub-state flag.

### Primary Symbol Ranges

The decoded primary symbol is interpreted by range:

| Symbol range | Meaning |
| --- | --- |
| `0x000..0x0ff` | Literal byte. |
| `0x100..0x180` | Begin an explicit marker record; emit `0x9e`, then control byte `symbol - 0x100`. Symbol `0x140` is the `fT19` literal-marker escape and is zero-class for adaptive table selection. |
| `0x181..0x190` | Copy a two-byte pair from the recent output window. |
| `0x191..0x1a0` | Copy a two-byte pair from the recent two-byte history table and move it to the front. |
| `0x1a1..` | Copy a complete previous marker record from the marker-record history and move it to the front. |

Literal handling:

```c
emit byte(symbol);
produced_final_estimate += 1;

if (symbol == 0x9e && version == fT21)
    emit 0xff;          // escape literal marker for marker expansion
```

Explicit marker handling:

```c
control = symbol - 0x100;
emit 0x9e;
emit control;
pending_state = marker_control_class[control];   // table at 0x13f2
```

If `pending_state` is zero, the marker record is complete. If it is non-zero, later suffix decodes append the remaining marker bytes.

The decoder keeps two move-to-front histories:

- A 48-entry two-byte history at `0x870e`.
- A 48-entry marker-record control-offset history at `0x82aa`.

These histories are not simple zero-based front-insert arrays. Each has a cursor:

```text
word_27A76  two-byte history cursor, initialized to 0x20
word_27A74  marker history cursor, initialized to 0x20
```

Both history arrays are zero-filled when the block decoder starts. `UNPACK2` does not maintain a separate validity bitmap and does not reject unwritten slots; an unwritten history slot reads as offset/value zero.

A history symbol indexes from the current cursor:

```c
slot = cursor + (symbol - history_base);
```

When a new entry is inserted from outside the history, the cursor is decremented and the entry is written at the new cursor. If the cursor underflows from zero, `UNPACK2` preserves the visible 16-entry window by copying slots `0..15` to slots `32..47`, then resets the cursor to `0x1f` before writing the new entry.

```c
insert_history(history, cursor, value):
    old = cursor;
    cursor--;

    if old == 0 {
        memcpy(&history[32], &history[0], 16 * sizeof(uint16_t));
        cursor = 0x1f;
    }

    history[cursor] = value;
```

This matters for symbols like `0x1a1 + 12`: it does not mean absolute `history[12]`; it means `history[marker_cursor + 12]`.

When an existing history entry is replayed, the cursor is not decremented. Instead, the selected entry is moved to `history[cursor]` by shifting entries `cursor..cursor+idx-1` one slot to the right.

```c
promote_history(history, cursor, idx, value):
    memmove(&history[cursor + 1], &history[cursor], idx * sizeof(uint16_t));
    history[cursor] = value;
```

### Pending Suffix States

Some explicit marker controls need extra bytes after the control byte. `UNPACK2` stores this in the high byte of a local state word. The suffix decoder runs before returning to normal primary-symbol mode.

The OS/2 `UNPACK2` block decoder implements the suffix paths inline in `ftcomp_expand_block_to_buffer`:

```text
0x934b..0x951f  pending states 1 and 2
0x9526..0x95e7  pending states 3, 4, and 5
```

These pending states come from `marker_control_class[control]` after an explicit marker control byte:

| Control range | Initial pending state | Suffix bytes appended | Marker form |
| --- | ---: | ---: | --- |
| `0x00..0x3f` | `1` | 1 | Short marker: one encoded distance byte. |
| `0x40..0x7f` | `2` | 2 | Medium marker: one encoded distance word. |
| `0x80` | `3` | 3 | Long marker: one length byte and one distance word. |
| other controls | `0` | 0 | Marker record is complete. |

#### Pending State 1: Direct Suffix Byte

Pending state `1` decodes one symbol from the tag-level static suffix table generated from the `tagged_model_seed_weights_le16` constants. It does not read a raw prefix and does not use the per-block adaptive tables.

OS/2 address anchors:

```text
0x934b..0x935f  dispatch state 1
0x9425..0x948c  decode one tagged suffix symbol
0x9493..0x94b4  direct-byte reuse/update and emit
```

The decoded symbol has one remembered value:

```c
value = decode_tagged_suffix_symbol();

if (value == 0x100) {
    value = recent_direct_byte;
} else {
    recent_direct_byte = value;
}

emit byte(value);
pending_state = 0;
```

The OS/2 implementation initializes this remembered value to zero at the start of the compressed block.

#### Pending State 2: Encoded Distance Word

Pending state `2` first reads a raw prefix from the bitstream, then decodes one symbol from the tag-level static suffix table. OS/2 `UNPACK2` uses fixed prefix sizes in this path; no output-size thresholds are present in `0x9366..0x93e5`.

The DOS `UNPACK2` binary contains an additional thresholded path at raw addresses `0x3cc2..0x3d65` (IDA rebased `0x13cc2..0x13d65`). This path checks the 32-bit final-output estimate at `0x82a4:0x82a6` before choosing class-1 and class-2 prefix widths. The thresholded DOS path is version/output-size dependent; it must not be collapsed into the OS/2 `fT19` fixed path.

OS/2 address anchors:

```text
0x9366..0x93e5  raw prefix decode
0x93ea..0x948c  tagged suffix symbol decode
0x94b6..0x951f  class-specific reuse/update, word construction, and emit
```

Prefix decode:

```c
if ((bitbuf & 0x8000) == 0) {
    suffix_class = 0;
    suffix_low_bits = (bitbuf >> 11) & 0x0f;
    consume(5);
} else if ((bitbuf & 0x4000) == 0) {
    suffix_class = 1;
    suffix_low_bits = (bitbuf >> 8) & 0x3f;
    consume(8);
} else {
    suffix_class = 2;
    suffix_low_bits = (bitbuf >> 7) & 0x7f;
    consume(9);
}
```

Each class has one remembered suffix symbol, initialized to zero at the start of the compressed block:

```c
value = decode_tagged_suffix_symbol();

if (value == 0x100) {
    value = recent_word_class[suffix_class];
} else {
    recent_word_class[suffix_class] = value;
}

switch suffix_class {
case 0:
    word = ((value + 0x10) << 4) | suffix_low_bits;
case 1:
    word = ((value + 0x44) << 6) | suffix_low_bits;
case 2:
    word = ((value + 0x0a2) << 7) | suffix_low_bits;
}

emit low_byte(word);
emit high_byte(word);
pending_state = 0;
```

The thresholded class-1/class-2 decode previously described here is not present in the OS/2 `UNPACK2` `fT19` path. If that logic exists in another build or another version path, keep it separate from this OS/2-confirmed decoder.

DOS thresholded variant:

```c
if ((bitbuf & 0x8000) == 0) {
    suffix_class = 0;
    suffix_low_bits = (bitbuf >> 11) & 0x0f;
    consume(5);
} else if (final_output_estimate < 0x5100) {
    suffix_class = 1;
    suffix_low_bits = (bitbuf >> 9) & 0x3f;
    consume(7);
} else if ((bitbuf & 0x4000) == 0) {
    suffix_class = 1;
    suffix_low_bits = (bitbuf >> 8) & 0x3f;
    consume(8);
} else if (final_output_estimate < 0x9100) {
    suffix_class = 2;
    suffix_low_bits = (bitbuf >> 8) & 0x3f;
    consume(8);
    word = ((value + 0x144) << 6) | suffix_low_bits;
} else {
    suffix_class = 2;
    suffix_low_bits = (bitbuf >> 7) & 0x7f;
    consume(9);
    word = ((value + 0x0a2) << 7) | suffix_low_bits;
}
```

For the `EVALUATE.LI_` fixture, enabling this DOS threshold path does not resolve the bad first frame header. The current mismatch occurs before marker expansion and before any useful conclusion can be drawn from the marker dictionary.

#### Pending States 3, 4, and 5: Long Marker Suffix

Pending state `3` is used by control byte `0x80`. It appends three bytes after the control byte. OS/2 handles this as a rolling state path:

```text
state 3 -> decode and emit byte 0, set state 4
state 4 -> decode and emit byte 1, set state 5
state 5 -> decode and emit byte 2, clear pending state
```

OS/2 address anchors:

```text
0x9526..0x95ba  decode one tagged suffix symbol
0x95be..0x95e7  rolling-state transform and emit
```

Pseudocode:

```c
value = decode_tagged_suffix_symbol();
pending_state++;

if (pending_state == 6) {
    pending_state = 0;

    if (value == 0x100) {
        value = recent_long_final_byte;
    } else {
        recent_long_final_byte = value;
    }
} else {
    if (value == 0x100)
        value = 0;
    else
        value++;
}

emit byte(value);
```

For a long marker record this means the first two suffix bytes use `0x100` as an alias for zero and all other decoded values are incremented by one. The third suffix byte uses one remembered value, also initialized to zero at compressed-block start.

#### Tagged Suffix Table

All three pending suffix paths above decode their Huffman symbol from the tag-level static suffix table generated from `tagged_model_seed_weights_le16`. They do not use the per-block adaptive primary tables.

The two-entry MTF transform and output-size threshold rules previously inferred for this area do not match the loaded OS/2 `UNPACK2` implementation. Treat them as unconfirmed for FTCOMP until another binary path proves where they apply.

### Two-Byte History

Symbols `0x181..0x190` copy a two-byte pair from the already-produced intermediate stream and insert that pair into the two-byte history:

```c
distance = symbol - 0x17f;        // 0x181 => 2, 0x190 => 17
pair = read_le16(&intermediate[len(intermediate) - distance]);
emit low_byte(pair);
emit high_byte(pair);

insert_history(two_byte_history, word_27A76, pair);
```

Symbols `0x191..0x1a0` copy a two-byte word from the recent two-byte history and promote the selected entry within the current 16-entry window:

```c
idx = symbol - 0x191;
slot = word_27A76 + idx;
pair = two_byte_history[slot];
emit low_byte(pair);
emit high_byte(pair);

promote_history(two_byte_history, word_27A76, idx, pair);
```

The copied pair is also used to update the estimated final output length. If the low byte is not `0x9e`, the estimate is incremented by two; otherwise it is incremented by one because the pair starts a marker record in the intermediate stream.

### Marker Record History

When the decoder emits a new explicit marker record with at least one suffix byte, it also records the offset of that marker record's control byte. It does not record the offset of the leading `0x9e`.

For an explicit marker:

```c
marker_start = len(intermediate);
emit 0x9e;
control_offset = len(intermediate);
emit control;
pending_state = marker_control_class[control];

if (pending_state != 0)
    insert_history(marker_history, word_27A74, control_offset);
```

Controls with `marker_control_class[control] == 0` are not inserted into marker history.

Symbols `0x1a1` and above replay one of these remembered marker records. The symbol index is relative to the marker-history cursor. Replay promotes the new copied marker's control offset within the current 16-entry history window; it does not decrement the cursor.

```c
emit 0x9e;
new_control_offset = len(intermediate);

idx = symbol - 0x1a1;
slot = word_27A74 + idx;
control_offset = marker_history[slot];

control = intermediate[control_offset];
emit control;

// Every remembered marker has at least one suffix byte.
emit intermediate[control_offset + 1];

if (control == 0x80) {
    emit intermediate[control_offset + 2];
    emit intermediate[control_offset + 3];
} else if (control & 0x40) {
    emit intermediate[control_offset + 2];
}

update produced_final_estimate by the marker-expanded length;
promote_history(marker_history, word_27A74, idx, new_control_offset);
```

The marker-expanded length estimate is the same as the marker expansion pass:

```c
if control == 0x80:
    length = suffix_byte0 + 0x43;
else if control & 0x40:
    length = (control & 0x3f) + 3;
else:
    length = control + 3;
```

The estimate is used only to adjust distance-code thresholds while decoding the current block.

## Marker Expansion

The marker expansion pass converts one intermediate segment's `0x9e` records into final bytes. It operates on `segment_data`, not on the full framed intermediate block.

Implementation anchors:

```text
DOS: ftcomp_expand_marker_runs
OS/2: ftcomp_expand_segment_to_output
DOS/OS2: ftcomp_expand_marker_stream
```

The segment-level wrapper receives a single segment including its one-byte mode flag:

```c
expand_segment(segment):
    segment_len = len(segment);

    if segment[0] == 0:
        return segment[1:segment_len];

    return ftcomp_expand_marker_stream(segment[1:segment_len]);
```

`ftcomp_expand_marker_stream` expands only the bytes after the mode flag.

Important wrapper detail: the OS/2 segment expander does not expand into a zero-based scratch buffer. For the normal `fT19` mode-2 path it expands in place at offset `0xcfdc` in a preinitialized work segment. The bytes before that offset form a preset history window:

```text
0xbc62..0xbda1  0x140 bytes of 0x20
0xbda2..0xbee1  0x140 bytes of 0xff
0xbee2..0xc021  0x140 bytes of 0x00
0xc022..0xcfdb  0x0fba bytes copied from the executable's fixed table
0xcfdc          segment expansion starts here
```

The logical preset dictionary length is therefore `0x137a` bytes. Marker distances are checked against the absolute destination offset, so early marker records may legally copy from this preset window before any bytes from the current segment have been emitted. A clean implementation can model this by prepending the preset dictionary to the segment output during marker expansion, then returning only bytes appended after the preset dictionary.

The complete standalone preset dictionary is included in [FTCOMP Preset Dictionary](ftcomp_preset_dictionary.hex.md). For `EVALUATE.LI_`, the first expanded segment begins:

```text
e1 01 01 5b 9e 44 31 0c ...
```

The first marker follows the literal `[` and has encoded distance `0x0c31`. It copies `License` from the preset window, which is why a zero-based marker expander fails with an invalid distance even when the Huffman stage is correct.

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

The destination is the current block-output window plus the preset dictionary described above. A marker back-reference must resolve against bytes already available in that combined window. In a simplified decoder without the preset dictionary, early marker records from real PACK2 streams can look like invalid back-references even when they are valid for IBM `UNPACK2`.

## Version-2 RLE Pass

For `fT21`, after marker expansion, `UNPACK2` runs an additional RLE-like pass.

Implementation anchor:

```text
DOS: ftcomp_expand_rle_runs
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
           decode framed intermediate stream
           for each intermediate segment:
               read uint16 segment_len
               if segment[0] == 0:
                   append segment[1:] directly
               else:
                   expand 0x9e marker records from segment[1:]
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
4. Add `original/examples/EVALUATE.LI_` as a compressed `fT19` fixture. The first block has intermediate target `0x01e3` and must expand to the member header's 683-byte final output.
5. Add at least one all-ones weighted block fixture, such as `fontutil.pk2` / `BINCTRL.DLL` or `os2drv.pk2` / `\os2\dll\bvhsvga.dll`.
6. Add at least one non-uniform weighted block fixture with a larger output, such as `fontutil.pk2` / `OS2FS.EXE` or `os2drv.pk2` / `\os2\MONITOR.DIF`.
7. Add an auxiliary-stream raw-block fixture from `os2drv.pk2` or `dvxp.pk2`; it should decode as metadata and must not be appended to primary file output.
8. Compare full decompression output against IBM `UNPACK2` for known PACK2/FTCOMP samples.
9. Add trace logging for:
   - block start offset
   - block type
   - compressed bytes consumed
   - intermediate bytes produced
   - intermediate segment lengths and mode bytes
   - marker-expanded bytes produced
   - final bytes produced

## Known Open Questions

These items are not yet fully confirmed:

- The exact field name for `intermediate_target`; behavior says it bounds the first-stage intermediate output.
- The exact version-2 RLE side-stream split field name and all edge cases.

The `EVALUATE.LI_` test case is `fT19`, so it does not require the version-2 RLE pass. It now decodes byte-for-byte to the DOS/OS2 `UNPACK2` output when the corrected `symbol_class` table and the preset marker dictionary are used.
