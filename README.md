# FH5DL - FlipHTML5 Downloader

A Go-based tool for downloading and generating PDFs from FlipHTML5 publications. This is a fork of the original [fh5dl by ygunayer](https://github.com/ygunayer/fh5dl) with added features.

## Features

- Download FlipHTML5 publications as PDF files
- Concurrent image downloading for improved performance
- Interactive terminal UI mode
- Support for capturing interactive elements
- Batch processing capabilities
- Memory-efficient handling of large publications

## Installation

### From Releases

Download the latest binary from the [Releases](https://github.com/ygunayer/fh5dl/releases) page for your platform.

### From Source

```bash
# Clone the repository
git clone https://github.com/your-username/fh5dl.git
cd fh5dl

# Build the application
go build -o fh5dl ./cmd/main.go
```

## Usage

### Terminal UI Mode

For a guided experience, use the terminal UI:

```bash
./fh5dl -t
```

### Command Line Mode

```bash
# Basic usage
./fh5dl https://online.fliphtml5.com/abcde/fghij/

# With custom output folder
./fh5dl -o /path/to/output https://online.fliphtml5.com/abcde/fghij/

# Force overwriting existing files
./fh5dl -f https://online.fliphtml5.com/abcde/fghij/

# With interactive elements revealed
./fh5dl -i https://online.fliphtml5.com/abcde/fghij/

# Control concurrency
./fh5dl -c 8 https://online.fliphtml5.com/abcde/fghij/
```

### Command Line Arguments

| Flag | Description |
|------|-------------|
| `-c` | Number of concurrent downloads. Defaults to (number of CPUs - 1) |
| `-o` | Output folder for the PDF. Defaults to current directory |
| `--image-out` | Output folder for downloaded images. Defaults to a temporary directory |
| `-f` | Overwrite existing PDF file if it exists |
| `-i` | Capture screenshots with interactive elements revealed |
| `-t, --termui` | Use the terminal UI mode |
| `-b` | Batch size for interactive captures. Defaults to 8 |

## Requirements

- Go 1.16+ (for building from source)
- Chrome/Chromium (for interactive capture mode)

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Original project by [ygunayer](https://github.com/ygunayer/fh5dl)
- Uses [go-arg](https://github.com/alexflint/go-arg) for command line arguments
- Uses [pdfcpu](https://github.com/pdfcpu/pdfcpu) for PDF generation
- Uses [chromedp](https://github.com/chromedp/chromedp) for interactive captures 