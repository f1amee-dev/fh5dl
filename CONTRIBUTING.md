# Contributing

Use Go 1.27.4 or later. Build with `go build -o fh5dl ./cmd/fh5dl`, format changes with `gofmt`, and run `go vet ./...` and `go test -race ./...`. CI also checks third-party vulnerabilities and GoReleaser v2 configuration.

Keep network fixtures small and deterministic. Tests should use a fake transport rather than live books. When changing viewer compatibility, record the public format and compare decoded pages against the viewer's output. Live smoke tests should use a small, bounded sample, never an entire publisher corpus.

The shared engine lives in `internal/book`; CLI input handling and Bubble Tea UI live in `internal/app`. Both interfaces must use the same engine. Generated PDFs, downloaded images and verification binaries belong outside the repository.

For releases, tag `v2.x.y`; GitHub Actions runs checks and GoReleaser builds all six supported targets. No release credentials are needed for local checks.
