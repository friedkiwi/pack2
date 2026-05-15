# pack2

Golang-based tools to pack and unpack OS/2 PACK2 archives.

## Installation

```sh
go install github.com/friedkiwi/pack2@latest
```

When working from a checkout of this module, this also works:

```sh
go install github.com/friedkiwi/pack2
```

## Usage

```sh
pack2 list file.IN_
pack2 unpack file.IN_ output-dir
pack2 pack output.IN_ input-file [input-file...]
```

