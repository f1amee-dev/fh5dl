These are trimmed public `javascript/config.js` fixtures captured on 2026-11-16.
Only `meta`, the original encoded `fliphtml5_pages`, and selected `bookConfig`
settings are retained. naht includes the original encoded bookConfig. `*.pages.json`
is the independent ground truth produced by the public viewer's `_DeString`
through the review's Node harness (`/tmp/fh5dl-review/decode.cjs`). Books cover
one/two pages, hashed WebP paths, and JPEG paths (132 pages; only about 24 KiB
of encoded data). whcsi/wpce adds the newer numeric w/h and p="none"
page metadata and files/ paths. No FlipHTML5 JS or WASM blob is included.

Decoder derivation: `wasm2wat`, `wasm2c`, then `wasm-opt -O3` on the WASM
embedded in the public `deString.js`. Original function 23 handles the `v01`
header/descriptors, function 18 handles encoded blocks, functions 20–22 decode
nibble pairs with `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_#`
(index modulo 16), and functions 16/17 implement standard RC4. Each encoded
block's final decoded byte gives the key length; the preceding pairs contain
the key and the rest contain the cipher text. Block flag 0x100000 selects this
operation; unflagged blocks are copied. The public viewer consumes the first
JSON value and ignores a trailing marker. A scratch Python port matched all
23 accessible books in the independent review before this Go port was written.
