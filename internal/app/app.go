package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/f1amee-dev/fh5dl/internal/book"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const usage = `Usage: fh5dl [flags] URL...
       fh5dl -i FILE|-|DIRECTORY [flags]
       fh5dl tui [flags] [URL]

Download FlipHTML5 page images as a PDF. Ctrl-C cancels.

Flags:
  -o, --output DIR     output directory (default ".")
  -j, --jobs N         concurrent requests/conversions, 1-8 (default 8)
  -f, --force          replace an existing PDF
  -i, --input SRC      URL list file, - for stdin, or directory of .txt lists; repeatable
      --lossless       keep WebP bytes instead of converting to JPEG
      --keep-images    keep the downloaded page images
  -t, --tui            interactive UI (same as "fh5dl tui")
  -q, --quiet          only print errors
  -v, --verbose        show retry diagnostics
      --version        print version

Examples:
  fh5dl https://online.fliphtml5.com/kzpyj/naht/
  fh5dl -o ./pdfs -i books.txt
  fh5dl -i ~/books/
`

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer, version string) int {
	var opts book.Options
	var inputs []string
	var quiet, verbose, showVersion, tui, check bool
	f := pflag.NewFlagSet("fh5dl", pflag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVarP(&opts.Output, "output", "o", ".", "output directory")
	f.IntVarP(&opts.Jobs, "jobs", "j", 8, "total concurrent requests/conversions (1-8)")
	f.BoolVarP(&opts.Force, "force", "f", false, "replace existing PDF")
	f.BoolVar(&opts.KeepImages, "keep-images", false, "retain this run's book-scoped image directory")
	f.BoolVar(&opts.Lossless, "lossless", false, "keep WebP bytes; avoid JPEG conversion")
	f.StringArrayVarP(&inputs, "input", "i", nil, "URL list, stdin (-), or directory of .txt lists; repeatable")
	f.BoolVarP(&quiet, "quiet", "q", false, "only errors on stderr")
	f.BoolVarP(&verbose, "verbose", "v", false, "show retry diagnostics")
	f.BoolVar(&check, "check", false, "verify every page image without creating PDFs (JSON output)")
	_ = f.MarkHidden("check")
	f.BoolVar(&showVersion, "version", false, "print version")
	f.BoolVarP(&tui, "tui", "t", false, "open interactive input and progress UI")
	f.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := f.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			return 0
		}
		return 2
	}
	if showVersion {
		fmt.Fprintln(out, "fh5dl", version)
		return 0
	}
	if opts.Jobs < 1 || opts.Jobs > 8 {
		fmt.Fprintln(stderr, "jobs must be between 1 and 8")
		return 2
	}
	args = f.Args()
	if len(args) > 0 && args[0] == "tui" {
		tui = true
		args = args[1:]
	}
	if tui {
		if check {
			fmt.Fprintln(stderr, "--check cannot be combined with tui")
			return 2
		}
		if len(inputs) > 0 || len(args) > 1 {
			fmt.Fprintln(stderr, "tui accepts at most one URL and no input files")
			return 2
		}
		seed := ""
		if len(args) == 1 {
			seed = args[0]
		}
		return runTUI(ctx, opts, seed, in, out, stderr)
	}
	ids, err := collect(args, inputs, in)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(ids) == 0 {
		f.Usage()
		return 2
	}
	e := book.New(opts)
	defer e.Close()
	var mu sync.Mutex
	tty := false
	if file, ok := stderr.(*os.File); ok {
		tty = term.IsTerminal(int(file.Fd()))
	}
	log := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		if tty {
			fmt.Fprint(stderr, "\r\033[K")
		}
		fmt.Fprintln(stderr, s)
	}
	if verbose && !quiet {
		e.Log = log
	}
	progress := func(p book.Progress) {
		if quiet {
			return
		}
		if !tty && (p.Stage == "images" || p.Stage == "check") && p.Done != p.Total && p.Done%25 != 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if tty {
			fmt.Fprintf(stderr, "\r\033[K%s: %s %d/%d", p.ID, p.Stage, p.Done, p.Total)
			if p.Stage == "done" || p.Stage == "skipped" {
				fmt.Fprintln(stderr)
			}
		} else {
			fmt.Fprintf(stderr, "%s: %s %d/%d\n", p.ID, p.Stage, p.Done, p.Total)
		}
	}
	if check {
		failed := 0
		for _, id := range ids {
			if ctx.Err() != nil {
				return 130
			}
			r, err := e.Check(ctx, id, progress)
			if err != nil {
				failed++
				r.Error = err.Error()
				log(id + ": " + err.Error())
			}
			if err := json.NewEncoder(out).Encode(r); err != nil {
				log(err.Error())
				return 1
			}
		}
		if ctx.Err() != nil {
			return 130
		}
		if failed > 0 {
			return 1
		}
		return 0
	}
	started := time.Now()
	ok, skipped, failed := 0, 0, 0
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range min(2, opts.Jobs) {
		wg.Go(func() {
			for id := range jobs {
				r, err := e.Download(ctx, id, progress)
				mu.Lock()
				if err != nil {
					failed++
					if tty {
						fmt.Fprint(stderr, "\r\033[K")
					}
					fmt.Fprintf(stderr, "%s: %v\n", id, err)
				} else {
					if r.Skipped {
						skipped++
					} else {
						ok++
					}
					fmt.Fprintln(out, r.Path)
					if opts.KeepImages && !quiet && r.Images != "" {
						fmt.Fprintln(stderr, "images:", r.Images)
					}
				}
				mu.Unlock()
			}
		})
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if !quiet {
		log(fmt.Sprintf("%d downloaded, %d skipped, %d failed (%s)", ok, skipped, failed, time.Since(started).Round(time.Millisecond)))
	}
	if ctx.Err() != nil {
		return 130
	}
	if failed > 0 {
		return 1
	}
	return 0
}
