package book

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"sort"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type MissingPage struct {
	Page  int    `json:"page"`
	URL   string `json:"url"`
	Error string `json:"error"`
}
type CheckResult struct {
	Error   string         `json:"error,omitempty"`
	ID      string         `json:"id"`
	Pages   int            `json:"pages"`
	Formats map[string]int `json:"formats"`
	Missing []MissingPage  `json:"missing"`
}

// Check GETs and validates every page, using the same URL candidates and retry
// policy as Download. It writes no images or PDFs. Books are checked sequentially
// by the CLI so an entire survey shares the configured request bound.
func (e *Engine) Check(ctx context.Context, id string, progress func(Progress)) (CheckResult, error) {
	r := CheckResult{ID: id, Formats: map[string]int{}, Missing: []MissingPage{}}
	data, err := e.fetch(ctx, "https://online.fliphtml5.com/"+id+"/javascript/config.js", 16<<20)
	if err != nil {
		return r, err
	}
	b, err := ParseConfig(id, data)
	if err != nil {
		return r, err
	}
	r.Pages = len(b.Pages)
	var mu sync.Mutex
	var done atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.Options.Jobs)
	for i, u := range b.Pages {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			data, err := e.fetchImage(gctx, u)
			format := ""
			if err == nil {
				select {
				case e.slots <- struct{}{}:
				case <-gctx.Done():
					return gctx.Err()
				}
				_, format, err = decodeImage(data)
				<-e.slots
			}
			mu.Lock()
			if err != nil {
				r.Missing = append(r.Missing, MissingPage{Page: i + 1, URL: u, Error: err.Error()})
			} else {
				r.Formats[format]++
			}
			mu.Unlock()
			if progress != nil {
				progress(Progress{ID: id, Stage: "check", Done: int(done.Add(1)), Total: r.Pages})
			}
			return gctx.Err()
		})
	}
	if err = g.Wait(); err != nil {
		return r, err
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	sort.Slice(r.Missing, func(i, j int) bool { return r.Missing[i].Page < r.Missing[j].Page })
	if len(r.Missing) > 0 {
		return r, fmt.Errorf("%d of %d page images failed", len(r.Missing), r.Pages)
	}
	return r, nil
}

func decodeImage(data []byte) (image.Image, string, error) {
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("invalid image: %w", err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return nil, "", fmt.Errorf("image dimensions exceed 40 megapixels")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("incomplete image: %w", err)
	}
	return img, kind, nil
}
