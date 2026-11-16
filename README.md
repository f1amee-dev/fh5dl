# fh5dl

Download public FlipHTML5 books as PDFs. Version 2 reads the viewer's encoded page lists in pure Go, downloads pages concurrently, and checks the PDF page count before saving.

## Install

Download an archive for your OS and architecture from [Releases](https://github.com/f1amee-dev/fh5dl/releases), verify it against `checksums.txt`, and put `fh5dl` on your PATH. macOS, Linux and Windows binaries support amd64 and arm64, without CGO or Chrome.

With Go 1.27.4 or later:

```sh
go install github.com/f1amee-dev/fh5dl/cmd/fh5dl@latest
```

## Usage

```sh
fh5dl https://online.fliphtml5.com/kzpyj/meoo/
fh5dl -o ./pdfs -j 8 kzpyj/naht kzpyj/mdmp
fh5dl --force --keep-images kzpyj/meoo
fh5dl --lossless kzpyj/meoo
fh5dl -i books.txt -i more-books.txt
printf '%s\n' kzpyj/naht kzpyj/dnfe | fh5dl -i -
fh5dl -i ./book-lists -o ./pdfs
fh5dl tui -o ./pdfs
fh5dl -t --force kzpyj/meoo
fh5dl --version
```

Supported inputs include bare `publisher/book`, scheme-less viewer URLs, `online.fliphtml5.com/<publisher>/<book>/index.html`, query strings and page fragments, and `fliphtml5.com/<publisher>/<book>/<slug>/`. For `/book/<number>` share links, open the viewer and copy its `online.fliphtml5.com` URL.

Each input file may contain several URLs, one per line. Blank lines and lines starting with `#` are ignored. A directory reads its immediate `.txt` files in filename order. Duplicate books are removed across all arguments and lists. Output filenames use a portable, bounded title plus publisher/book IDs, so equal titles stay separate. Existing PDFs are skipped unless `--force` replaces them with a freshly generated PDF.

| Flag | Purpose |
| --- | --- |
| `-o, --output DIR` | Output directory (default: current directory) |
| `-j, --jobs N` | Total request/conversion concurrency, 1–8 (default: 8) |
| `-i, --input PATH` | File, stdin (`-`), or directory; repeatable |
| `-f, --force` | Replace existing PDFs |
| `--keep-images` | Retain the current run's unique book-scoped image directory |
| `--lossless` | Preserve original WebP images instead of converting to JPEG |
| `-q, --quiet` | Suppress CLI progress and summary |
| `-v, --verbose` | Include retry diagnostics |
| `-t, --tui` | Open the Bubble Tea UI (also `fh5dl tui`) |
| `--version` | Version and commit, with Go build-info fallback |

The TUI accepts normal text, including `q`, `y`, `n` and Unicode. Enter explicitly submits; Esc or Ctrl-C cancels. CLI output paths go to stdout; progress, errors and batch summaries go to stderr. Non-terminal progress uses plain lines. Batches run at most two books concurrently, sharing the `--jobs` limit. Ctrl-C cancels requests, retry waits and PDF generation.

| Exit | Meaning |
| --- | --- |
| 0 | All books downloaded or skipped; help/version |
| 1 | At least one download failed |
| 2 | Invalid arguments, input lists, or no URLs |
| 130 | Interrupted/cancelled |

## Output and limits

JPEG bytes are embedded unchanged. By default WebP pages become quality-90 JPEGs for smaller PDFs and faster generation; this conversion is lossy. `--lossless` retains the source pixels but can produce substantially larger PDFs. Images are validated before import, written through `.part` files, and never reused from previous runs. Temporary images and incomplete PDFs are removed unless `--keep-images` is set (that flag retains images only).

PDFs contain static page images. They do not preserve searchable text, links, audio, video, animations or hidden annotation content. Version 2 removes the old `--interactive` browser capture because it duplicated spreads and included consent banners. Books with multiple component images per logical page are rejected with an explicit error until their layout can be reconstructed correctly. There is no fixed page-count cap.

Private/restricted/removed books return a clear 403 error; missing books return 404. The downloader does not bypass access controls. Config responses are limited to 16 MiB, images to 64 MiB and 40 megapixels. Only transient failures (timeouts, incomplete transfers, 429 and 5xx) are retried, up to three attempts with cancellable backoff and `Retry-After` support. Missing/forbidden images also try documented large/page paths and JPEG/WebP alternatives.

For compatibility surveys, `fh5dl --check URL...` GETs and validates every page image, processes books sequentially, and emits one JSON record per book (counts, formats and missing pages). It creates no PDFs or image files.

## Development

```sh
go build -o fh5dl ./cmd/fh5dl
go vet ./...
go test -race ./...
goreleaser check
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [decoder fixture notes](internal/book/testdata/README.md).
