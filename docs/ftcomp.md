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
literal_weight_b      0x61
marker_weight_b       0x1e
final output size     683 bytes, from the PACK2 member header
```

This is the first required negative test for incomplete readers: if a decoder accepts only raw blocks (`ff ff`) and rejects this sample with an error such as "compressed FTCOMP blocks are not implemented", the missing part is not the PACK2 container parser. The missing part is the compressed-block pipeline: model Huffman decode, adaptive table rebuild, main intermediate stream decode, and marker expansion.

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

The Huffman fast path indexes tables with the top 9 bits of the 16-bit bit buffer.

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

### Table Work Areas

The DOS implementation stores FTCOMP state in global data. The names below are descriptive; the addresses are the IDA offsets from `UNPACK2_unpacked.exe`.

| Address | Size | Meaning |
| --- | ---: | --- |
| `0x528e` | `0x1b30` | Huffman node work area, 870 records of 8 bytes. |
| `0x6dbe` | `0x0400` | Generated fast symbol table, 512 `uint16` entries indexed by the top 9 bits. |
| `0x71be` | `0x0200` | Generated fast bit-length table, 512 `uint8` entries. |
| `0x73be` | `0x0400` | Snapshot of the first adaptive fast symbol table. |
| `0x77be` | `0x0200` | Snapshot of the first adaptive fast length table. |
| `0x79be` | `0x0362` | Per-block model weights, 433 `uint16` entries. |
| `0x82aa` | `0x0060` | Recent marker-record offset history, 48 `uint16` slots. |
| `0x870e` | `0x0060` | Recent two-byte literal/history table, 48 `uint16` slots. |
| `0x13f2` | `0x0100` | Marker-control class table indexed by marker control byte. |
| `0x14f2` | `0x01b1` | Symbol class table; non-zero symbols use the second adaptive table. |
| `0x16a4` | `0x0400` | Static/model fast symbol table snapshot. |
| `0x1aa4` | `0x0400` | Active adaptive fast symbol table snapshot used by suffix decodes. |
| `0x2574` | `0x1588` | Static/model Huffman node snapshot. |
| `0x3afc` | `0x1588` | Active adaptive Huffman node snapshot. |
| `0x830e` | `0x0200` | Static/model fast bit-length table snapshot. |
| `0x850e` | `0x0200` | Active adaptive fast bit-length table snapshot used by suffix decodes. |

The 8-byte node records at `0x528e` and in the snapshots are:

```c
struct huff_node {
    uint16_t weight;       // scaled frequency/weight
    uint16_t parent;       // parent node id, or 0 while unlinked
    uint16_t child0;       // left/zero child for internal nodes
    uint16_t child1;       // right/one child for internal nodes
};
```

Node ids are stored as byte offsets into the node table, not as dense indexes. Leaf symbol `n` has node id `n * 4`. Internal node ids start at `0x06c4`, which is `433 * 4`.

### Static Huffman Initialization

`ftcomp_init_decompressor(mode, use_static_tables)` records:

```text
byte_29BB2 = mode
byte_2C8CC = use_static_tables
word_27A4C:len = far pointer to an alternate adaptive node snapshot area
```

When `mode` requires generated static tables, it:

1. Clears `huff_node[870]` at `0x528e`.
2. Copies 433 initial static weights from `0x16a4` into `node[i].weight`.
3. Calls `build_huffman_decode_tables`.
4. Copies the resulting node table and fast tables into the static/model snapshots:

```text
0x528e -> 0x2574, length 0x1588
0x6dbe -> 0x16a4, length 0x0400
0x71be -> 0x830e, length 0x0200
```

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

For a clean implementation, these fixed arrays are constants from `UNPACK2`. They are not derived from the compressed input. The generated static/model Huffman tables can be reproduced from the 433 static weights and the table-builder below.

In the unpacked DOS executable, the initialized DGROUP data used by these offsets is present in `original/dos/UNPACK2_unpacked.exe` at:

```text
file_offset = dgroup_offset + 0x17840
```

Constants needed for the `fT19` compressed path can therefore be extracted from:

| Runtime offset | File offset | Size | Contents |
| --- | ---: | ---: | --- |
| `0x13f2` | `0x18c32` | `0x0100` | Marker-control class table. |
| `0x14f2` | `0x18d32` | `0x01b1` | Symbol class table. |
| `0x16a4` | `0x18ee4` | `0x0362` | Initial static/model Huffman weights, 433 `uint16` values. |
| `0x0438` | `0x17c78` | `0x0fba` | Fixed static table block copied to `0xc024` by `ftcomp_init_static_tables`. |

The `fT19` sample `EVALUATE.LI_` needs the first three rows. The `0x0438` block is part of the original runtime state setup; keep it when cloning the DOS implementation's table layout exactly.

### Generic Huffman Table Builder

`build_huffman_decode_tables` consumes `node[0..432].weight` and rebuilds:

- Parent/child links in the node table.
- A 512-entry fast symbol table.
- A 512-entry fast bit-length table.

The builder treats zero weights as absent symbols. Non-zero leaves are inserted into a sorted queue keyed by `weight`; ties preserve the existing order in the queue.

Special case: if there is only one non-zero symbol, the builder adds the last zero-weight symbol as a synthetic second leaf with weight `1`.

Implementation-level outline:

```c
// Gather leaves.
queue = []
last_zero_node = 0
for symbol = 0; symbol < 0x1b1; symbol++ {
    node_id = symbol * 4
    node[symbol].parent = 0

    if node[symbol].weight == 0 {
        last_zero_node = node_id
        continue
    }

    queue.push(node_id)
}

if len(queue) == 1 {
    node[last_zero_node / 4].weight = 1
    queue.push(last_zero_node)
}

stable_sort_by_weight(queue)

next_internal = 0x06c4
while len(queue) > 1 {
    left = queue.pop_front()
    right = queue.pop_front()

    parent = next_internal
    next_internal += 4

    node[parent / 4].weight = node[left / 4].weight + node[right / 4].weight
    node[parent / 4].parent = 0
    node[parent / 4].child0 = left
    node[parent / 4].child1 = right
    node[left / 4].parent = parent
    node[right / 4].parent = parent

    insert parent back into queue before the first entry whose weight is
    greater than or equal to node[parent].weight
}

root = queue[0]
word_2F8D6 = root
word_2F8D8 = 0
```

After the tree is built, the DOS implementation fills the 9-bit fast tables when the active global table mode requests generated fast tables. Otherwise the same table shape is supplied from the prebuilt static snapshots.

```c
for prefix = 0; prefix < 512; prefix++ {
    bits = prefix << 7;       // top 9 bits in a 16-bit bit buffer
    nbits = 0;
    node_id = root;

    while node_id >= 0x06c4 && nbits < 9 {
        if (bits & 0x8000)
            node_id = node[node_id / 4].child1;
        else
            node_id = node[node_id / 4].child0;
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
        weight = (weight * scale) >> 8;
        if (old != 0 && weight == 0)
            weight = 1;
    }
}
```

The generated first table is saved:

```text
0x528e -> adaptive_node_snapshot_0, length 0x1b30
0x6dbe -> 0x73be, length 0x0400
0x71be -> 0x77be, length 0x0200
```

If the second pair of block weight bytes is identical to the first pair, or if both second-pair bytes are zero, the first table is reused. Otherwise the second adaptive table is built the same way using:

```text
literal_weight_b = block byte +4
marker_weight_b  = block byte +5
```

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

The adaptive table rebuild multiplies non-zero model entries by one of two weight bytes. Which weight byte is used depends on an auxiliary symbol-class table. `fT21` has two sets of weight bytes; if both sets are equal or the second set is zero, it reuses the first generated table.

Implementation guidance:

- Preserve model entries exactly as decoded.
- When scaling weights, if the maximum scaled weight exceeds `0xff`, scale all weights down so the maximum fits in one byte.
- If a non-zero source weight scales to zero, clamp it back to `1`.

## Main Intermediate Stream Decode

After the model has been decoded and adaptive tables rebuilt, the block bitstream produces an intermediate stream. The loop runs until `intermediate_target` bytes have been written.

The intermediate stream contains literal bytes and `0x9e` marker records. It is not the final output. It is later passed to marker expansion.

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
        symbol_node = node[symbol_node / 4].child1;
    else
        symbol_node = node[symbol_node / 4].child0;
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
| `0x100..0x180` | Begin an explicit marker record; emit `0x9e`, then control byte `symbol - 0x100`. |
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
- A 48-entry marker-record offset history at `0x82aa`.

When a history symbol is used, the selected record is emitted and moved to the front by shifting the older entries right with `memmove`.

### Pending Suffix States

Some explicit marker controls need extra bytes after the control byte. `UNPACK2` stores this in the high byte of a local state word. The suffix decoder runs before returning to normal primary-symbol mode.

The suffix decoder first reads a small raw prefix from the bitstream. It returns:

```text
suffix_low_bits  // called var_C in IDA
suffix_class     // called var_14 in IDA; values 0, 1, or 2
```

For the normal `fT19` path:

```c
if ((bitbuf & 0x8000) == 0) {
    suffix_class = 0;
    suffix_low_bits = (bitbuf >> 11) & 0x0f;
    consume(5);
} else if ((bitbuf & 0x4000) == 0) {
    suffix_class = 1;
    if (bytes_produced_so_far < 0x5100) {
        suffix_low_bits = (bitbuf >> 9) & 0x3f;
        consume(7);
    } else {
        suffix_low_bits = (bitbuf >> 8) & 0x3f;
        consume(8);
    }
} else {
    suffix_class = 2;
    if (bytes_produced_so_far < 0x9100) {
        suffix_low_bits = (bitbuf >> 8) & 0x3f;
        consume(8);
    } else {
        suffix_low_bits = (bitbuf >> 7) & 0x7f;
        consume(9);
    }
}
```

There is a version-2 special case after control byte `0x40`: it sets a flag and uses a 2-bit prefix instead of the normal prefix tree.

After the raw prefix, the decoder reads one adaptive Huffman symbol and applies a small move-to-front transform. Symbols `0x100` and, for `fT21`, `0x101`, mean "reuse the most recent" and "reuse the second-most-recent" value for the current suffix class. Other values are adjusted upward so the aliases do not consume code space:

```c
decode_mtf(value, recent0, recent1):
    if value == 0x100:
        value = recent0;
    else if version == fT21 && value == 0x101:
        value = recent1;
    else {
        if (recent1 < recent0) {
            if (recent1 <= value) value++;
            if (recent0 <= value) value++;
        } else {
            if (recent0 <= value) value++;
            if (recent1 <= value) value++;
        }
        recent1 = recent0;
        recent0 = value;
    }
    return value;
```

The DOS code has separate `recent0/recent1` pairs for:

- Literal suffix bytes.
- Suffix class 0.
- Suffix class 1.
- Suffix class 2.
- The version-2 special six-symbol rolling state.

### Suffix Word Encoding

For marker suffix states other than the direct literal-byte state, the decoder appends a little-endian 16-bit word to the intermediate stream. The word is built from the decoded MTF value and the raw prefix:

```c
switch suffix_class {
case 0:
    word = ((value + 0x10) << 4) | suffix_low_bits;
case 1:
    word = ((value + 0x44) << 6) | suffix_low_bits;
case 2:
    if (bytes_produced_so_far < 0x9100)
        word = ((value + 0x144) << 6) | suffix_low_bits;
    else
        word = ((value + 0x0a2) << 7) | suffix_low_bits;
}

emit low_byte(word);
emit high_byte(word);
pending_state = 0;
```

For the version-2 special control-`0x40` state:

```c
word = ((value + 0x40) << 2) | suffix_low_bits;
emit low_byte(word);
emit high_byte(word);
pending_state = 0;
```

For the direct literal-byte suffix state, the decoded value is emitted as one byte and the literal recent-value history is updated.

### Marker Record Histories

When the decoder emits a new explicit marker record, it also maintains a move-to-front history of marker record start offsets. This is why symbols `0x1a1` and above can reproduce a whole prior marker record without restating all bytes.

For a copied marker record:

```c
emit 0x9e;
record_start = marker_history[symbol - 0x1a1];
copy control byte from intermediate[record_start];
copy the required one, two, or three suffix bytes according to marker_control_class[control];
update produced_final_estimate by the marker-expanded length;
move record_start to the front of marker_history;
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
4. Add `original/examples/EVALUATE.LI_` as a compressed `fT19` fixture. The first block has intermediate target `0x01e3` and must expand to the member header's 683-byte final output.
5. Compare full decompression output against IBM `UNPACK2` for known PACK2/FTCOMP samples.
6. Add trace logging for:
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
- The fixed static tables should be transcribed into source form before implementing a standalone decoder. The docs identify their IDA locations and usage, but do not inline the full constant byte arrays.
- The exact version-2 RLE side-stream split field name and all edge cases.

The `EVALUATE.LI_` test case is `fT19`, so it does not require the version-2 RLE pass.
