# Changelog

## 2.0.0

- Rewrite the downloader with pure-Go decoding of current FlipHTML5 v01 page lists; retain plain-array compatibility.
- Share HTTP connections, bound parallel downloads/conversions, retry transient failures and support cancellation.
- Validate images, isolate each book/run, clean temporary files, remove the 1,000-image cap and check PDF page counts.
- Atomically replace fresh PDFs under `--force`; preserve JPEG bytes and convert WebP to quality-90 JPEG by default (`--lossless` opts out).
- Add multi-URL arguments and complete, deduplicated file/stdin/directory lists; report batch failures through exit status.
- Replace terminal input with Bubble Tea textinput and shared-engine progress; fix cancellation and Unicode editing.
- Remove incorrect interactive browser captures; reject split-image pages explicitly.
- Move the command to `cmd/fh5dl`, update the module path and dependencies, and modernize CI and six-target releases with GoReleaser v2.


## 1.1.0

### added
- interactive terminal ui for easier usage
- interactive element capture capability
- batch processing support
- improved progress tracking with speed and eta
- memory optimization for large publications
- example batch file template
- comprehensive documentation

### changed
- modified code comments to be more human-like
- improved error handling
- optimized memory usage
- enhanced concurrent downloading

### fixed
- occasional failures with large publications
- memory leaks in long-running downloads

## v1.0.0 (original release)

### features
- basic fliphtml5 pdf downloading
- concurrent image downloading
- pdf generation from images 