<div align="center">

# fh5dl

Save FlipHTML5 books as PDFs. Paste a link, get a PDF.

[![Release](https://img.shields.io/github/v/release/f1amee-dev/fh5dl?style=flat-square&color=2ea44f)](https://github.com/f1amee-dev/fh5dl/releases/latest)
[![CI](https://img.shields.io/github/actions/workflow/status/f1amee-dev/fh5dl/default.yml?branch=main&style=flat-square&label=CI)](https://github.com/f1amee-dev/fh5dl/actions/workflows/default.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/f1amee-dev/fh5dl?style=flat-square&logo=go&logoColor=white)](go.mod)
[![Downloads](https://img.shields.io/github/downloads/f1amee-dev/fh5dl/total?style=flat-square&color=blue)](https://github.com/f1amee-dev/fh5dl/releases)
[![License](https://img.shields.io/github/license/f1amee-dev/fh5dl?style=flat-square)](LICENSE)

**Download for your system**

[![macOS Apple Silicon](https://img.shields.io/badge/macOS-Apple_Silicon-000000?style=for-the-badge&logo=apple&logoColor=white)](https://github.com/f1amee-dev/fh5dl/releases/latest/download/fh5dl_darwin_arm64.tar.gz)
[![macOS Intel](https://img.shields.io/badge/macOS-Intel-555555?style=for-the-badge&logo=apple&logoColor=white)](https://github.com/f1amee-dev/fh5dl/releases/latest/download/fh5dl_darwin_amd64.tar.gz)
[![Windows](https://img.shields.io/badge/Windows-x64-0078D4?style=for-the-badge&logo=data:image/svg%2bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0id2hpdGUiPjxwYXRoIGQ9Ik0wIDBoMTF2MTFIMHpNMTMgMGgxMXYxMUgxM3pNMCAxM2gxMXYxMUgwek0xMyAxM2gxMXYxMUgxM3oiLz48L3N2Zz4=)](https://github.com/f1amee-dev/fh5dl/releases/latest/download/fh5dl_windows_amd64.zip)
[![Linux](https://img.shields.io/badge/Linux-x64-FCC624?style=for-the-badge&logo=linux&logoColor=black)](https://github.com/f1amee-dev/fh5dl/releases/latest/download/fh5dl_linux_amd64.tar.gz)

<sub>Other builds (Windows ARM, Linux ARM) are on the <a href="https://github.com/f1amee-dev/fh5dl/releases/latest">releases page</a>.</sub>

</div>

---

## Quick start

```sh
fh5dl https://online.fliphtml5.com/kzpyj/meoo/
```

The PDF lands in the current folder, named after the book. That's the whole thing.

Prefer not to type flags? Run `fh5dl tui` and paste the link into the prompt.

## Install

**Prebuilt binary.** Grab the button for your system above, unzip it, and put `fh5dl` somewhere on your `PATH`, such as `/usr/local/bin`. On Windows, put `fh5dl.exe` in a folder that's on your `Path`. Each release ships a `checksums.txt` if you want to verify the download.

**With Go** (1.27 or newer):

```sh
go install github.com/f1amee-dev/fh5dl/v2/cmd/fh5dl@latest
```

> [!NOTE]
> On macOS, the first run of a downloaded binary may be blocked by Gatekeeper. Run `xattr -d com.apple.quarantine ./fh5dl` once, or install with `go install`.

## Usage

```sh
# one or more books
fh5dl kzpyj/naht kzpyj/mdmp

# choose where PDFs go
fh5dl -o ~/Books https://online.fliphtml5.com/kzpyj/meoo/

# a text file with one link per line
fh5dl -i books.txt

# a whole folder of .txt files
fh5dl -i ./book-lists -o ./pdfs

# from another command
cat books.txt | fh5dl -i -

# interactive mode
fh5dl tui
```

Any of these link styles work:

```text
https://online.fliphtml5.com/kzpyj/meoo/
online.fliphtml5.com/kzpyj/meoo/index.html#p=3
https://fliphtml5.com/kzpyj/meoo/some-book-title/
www.fliphtml5.com/kzpyj/meoo/
kzpyj/meoo
```

Got a `fliphtml5.com/book/12345` share link instead? Open it in a browser and copy the `online.fliphtml5.com/...` address it takes you to.

In list files, blank lines and lines starting with `#` are skipped, and duplicates are only downloaded once. If a PDF already exists, fh5dl skips it. Use `--force` to rebuild it.

### Flags

| Flag | What it does |
| --- | --- |
| `-o, --output DIR` | Where to save PDFs (default: current folder) |
| `-i, --input SRC` | Read links from a file, a folder of `.txt` files, or `-` for stdin. Can be repeated |
| `-j, --jobs N` | Parallel downloads, 1 to 8 (default 8) |
| `-f, --force` | Rebuild PDFs that already exist |
| `--lossless` | Keep the original WebP images (bigger PDFs) |
| `--keep-images` | Keep the downloaded page images next to the PDF |
| `-t, --tui` | Interactive mode, same as `fh5dl tui` |
| `-q, --quiet` | Only print errors |
| `-v, --verbose` | Show retries and other details |
| `--version` | Print the version |

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Everything downloaded or was already there |
| `1` | At least one book failed |
| `2` | Bad arguments or no links given |
| `130` | You pressed Ctrl-C |

Progress goes to stderr and the saved PDF paths go to stdout, so `fh5dl -i list.txt > saved.txt` gives you a clean list of files.

## What you get

Each PDF page is the book's page image. Most FlipHTML5 books store pages as WebP, which fh5dl converts to high quality JPEG. The PDFs come out several times smaller and build faster that way. If you'd rather keep the original pixels, pass `--lossless`.

The PDF doesn't include searchable text, links, audio, video or pop-up content. It's a picture of each page, the same as what you see in the viewer.

Private or removed books fail with a clear `403` or `404` message. fh5dl only downloads books that are publicly viewable, and it doesn't try to get around access controls.

<details>
<summary>How it works</summary>

FlipHTML5 now encodes the page list inside each book's `javascript/config.js`. fh5dl decodes that in plain Go, so there's no browser or Chrome involved. It then downloads the page images in parallel over one shared connection pool and builds the PDF. Before saving, it checks that the page count matches the book.

Temporary files and half written PDFs are cleaned up. Timeouts, dropped connections, `429` and `5xx` responses are retried up to three times. Missing pages are not retried.

There's also a hidden `--check` flag that fetches every page image and prints a JSON line per book without writing any PDFs. It's handy for testing many books at once.

</details>

<details>
<summary>Upgrading from 0.x</summary>

- The old version stopped working when FlipHTML5 changed its config format. Version 2 handles the new format.
- `--interactive` (the Chrome screenshot mode) is gone. It saved wrong pages, with both halves of a spread and a cookie banner.
- `-c` is now `-j`, and list files now read every line instead of only the first.

</details>

## Building from source

```sh
git clone https://github.com/f1amee-dev/fh5dl
cd fh5dl
go build -o fh5dl ./cmd/fh5dl
go test -race ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for more.

## License

[MIT](LICENSE). Originally forked from [ygunayer/fh5dl](https://github.com/ygunayer/fh5dl).
