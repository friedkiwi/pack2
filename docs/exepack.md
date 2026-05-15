# EXEPACK Unpacking Notes

`original/dos/UNPACK2.EXE` is wrapped with Microsoft EXEPACK. IDA Pro warned that the executable was packed, and the runtime stub matched the EXEPACK format.

## Identification

The packed executable has these EXEPACK indicators:

- DOS entry point at `CS:IP 090B:0010`, file offset `0x92c0`.
- EXEPACK header begins at file offset `0x92b0`.
- The EXEPACK signature word is `0x4252`, the ASCII marker `RB`.
- The unpacker stub contains the standard error string `Packed file is corrupt`.
- The decompression loop uses EXEPACK RLE commands:
  - `0xB0`: repeat-fill `LENGTH` bytes with one value.
  - `0xB2`: copy `LENGTH` literal bytes.
  - The low bit marks the final block.

The string `XID=COMPRESS VL=NM000` is still present after unpacking. That appears to belong to the IBM archive/compression format handled by `UNPACK2`, not to the EXEPACK runtime wrapper.

## Tool Used

The executable was unpacked with `w4kfu/unEXEPACK`:

- Project page: <https://w4kfu.github.io/unEXEPACK/>
- Source repository: <https://github.com/w4kfu/unEXEPACK>

The C source was downloaded from GitHub, compiled locally into `/private/tmp/unexepack`, and run with:

```sh
/private/tmp/unexepack original/dos/UNPACK2.EXE original/dos/UNPACK2_unpacked.exe
```

## Result

The unpacked executable is:

```text
original/dos/UNPACK2_unpacked.exe
```

Observed header/result details:

- Original size: `37,942` bytes.
- Unpacked size: `105,904` bytes.
- Output format: MS-DOS MZ executable.
- New entry point: `CS:IP 0000:4C90`.
- Stack: `SS:SP 1FDB:2000`.
- Relocation entries: `26`.
- SHA-256: `5af8477b06962e84aaf270052927bb7c0138fb22616aaaa57b87a2680b8266a0`.

Validation checks:

- The unpacked file has a valid MZ header.
- The EXEPACK stub string `Packed file is corrupt` is no longer present.
- Running `unEXEPACK` against `original/dos/UNPACK2_unpacked.exe` reports `This is not a valid EXEPACK executable`, which is expected after removing the wrapper.
