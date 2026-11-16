package book

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	Output                      string
	Jobs                        int
	Force, KeepImages, Lossless bool
}
type Progress struct {
	ID, Stage   string
	Done, Total int
}
type Result struct {
	ID, Path, Images string
	Pages            int
	Skipped          bool
}
type Engine struct {
	Client   *http.Client
	Options  Options
	Log      func(string)
	slots    chan struct{}
	outputMu sync.Mutex
}

var pdfInit sync.Once

func New(opts Options) *Engine {
	if opts.Jobs < 1 {
		opts.Jobs = 8
	}
	if opts.Output == "" {
		opts.Output = "."
	}
	pdfInit.Do(api.DisableConfigDir)
	return &Engine{Client: NewClient(opts.Jobs), Options: opts, slots: make(chan struct{}, opts.Jobs)}
}
func (e *Engine) Close() { e.Client.CloseIdleConnections() }

func (e *Engine) Download(ctx context.Context, id string, progress func(Progress)) (Result, error) {
	r := Result{ID: id}
	send := func(stage string, done, total int) {
		if progress != nil {
			progress(Progress{ID: id, Stage: stage, Done: done, Total: total})
		}
	}
	send("config", 0, 0)
	data, err := e.fetch(ctx, "https://online.fliphtml5.com/"+id+"/javascript/config.js", 16<<20)
	if err != nil {
		return r, err
	}
	b, err := ParseConfig(id, data)
	if err != nil {
		return r, err
	}
	r.Pages = len(b.Pages)
	out, err := filepath.Abs(e.Options.Output)
	if err != nil {
		return r, err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return r, err
	}
	r.Path = filepath.Join(out, Filename(b.Title, id))
	exists := func() (bool, error) {
		st, err := os.Lstat(r.Path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !st.Mode().IsRegular() {
			return false, fmt.Errorf("output is not a regular file: %s", r.Path)
		}
		return true, nil
	}
	e.outputMu.Lock()
	found, err := exists()
	e.outputMu.Unlock()
	if err != nil {
		return r, err
	}
	if found && !e.Options.Force {
		r.Skipped = true
		send("skipped", r.Pages, r.Pages)
		return r, nil
	}
	// A fresh, book-scoped directory never trusts leftovers from another run.
	dir, err := os.MkdirTemp(out, ".fh5dl-"+strings.TrimSuffix(Filename("", id), ".pdf")+"-")
	if err != nil {
		return r, err
	}
	if !e.Options.KeepImages {
		defer os.RemoveAll(dir)
	} else {
		r.Images = dir
	}
	files := make([]string, len(b.Pages))
	var done atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.Options.Jobs)
	for i, u := range b.Pages {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			file, err := e.downloadImage(gctx, u, dir, i)
			if err != nil {
				return fmt.Errorf("page %d: %w", i+1, err)
			}
			files[i] = file
			send("images", int(done.Add(1)), len(files))
			return nil
		})
	}
	if err = g.Wait(); err != nil {
		return r, err
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	send("pdf", len(files), len(files))
	tmp, err := os.CreateTemp(out, ".fh5dl-*.part")
	if err != nil {
		return r, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Close(); err != nil {
		return r, err
	}
	// ImportImagesFile appends when its output already exists. Remove the empty
	// reservation before import; the random path stays on the destination volume.
	if err = os.Remove(tmpName); err != nil {
		return r, err
	}
	conf := model.NewDefaultConfiguration()
	conf.Offline = true
	imp := pdfcpu.DefaultImportConfig()
	// Default import uses one full-sized image per page.
	if err = api.ImportImagesFile(ctx, files, tmpName, imp, conf); err != nil {
		return r, fmt.Errorf("PDF import: %w", err)
	}
	n, err := api.PageCountFile(ctx, tmpName)
	if err != nil {
		return r, err
	}
	if n != r.Pages {
		return r, fmt.Errorf("PDF has %d pages, expected %d", n, r.Pages)
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	e.outputMu.Lock()
	defer e.outputMu.Unlock()
	found, err = exists()
	if err != nil {
		return r, err
	}
	if found && !e.Options.Force {
		r.Skipped = true
		return r, nil
	}
	if err = os.Rename(tmpName, r.Path); err != nil {
		return r, fmt.Errorf("replace output: %w", err)
	}
	send("done", n, n)
	return r, nil
}

func (e *Engine) downloadImage(ctx context.Context, raw, dir string, index int) (string, error) {
	data, err := e.fetchImage(ctx, raw)
	if err != nil {
		return "", err
	}
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-e.slots }()
	img, kind, err := decodeImage(data)
	if err != nil {
		return "", err
	}
	ext := ".jpg"
	if kind != "jpeg" {
		ext = "." + kind
	}
	if kind == "webp" && !e.Options.Lossless {
		var buf bytes.Buffer
		if err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return "", err
		}
		data = buf.Bytes()
		ext = ".jpg"
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	name := filepath.Join(dir, fmt.Sprintf("%06d%s", index+1, ext))
	if err = os.WriteFile(name+".part", data, 0600); err != nil {
		return "", err
	}
	if err = os.Rename(name+".part", name); err != nil {
		return "", err
	}
	return name, nil
}

func (e *Engine) fetchImage(ctx context.Context, raw string) ([]byte, error) {
	var last error
	for _, u := range imageCandidates(raw) {
		data, err := e.fetch(ctx, u, 64<<20)
		if err == nil {
			return data, nil
		}
		last = err
		var status *HTTPError
		if !errors.As(err, &status) || (status.Status != 403 && status.Status != 404) {
			return nil, err
		}
	}
	return nil, last
}
