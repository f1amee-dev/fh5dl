package book

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string {
	switch e.Status {
	case 403:
		return "HTTP 403: book private, restricted or removed (viewer may redirect to fliphtml5.com/check/): " + e.URL
	case 404:
		return "HTTP 404: book or image not found: " + e.URL
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.URL)
}

func NewClient(jobs int) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = jobs
	t.MaxIdleConnsPerHost = jobs
	t.MaxConnsPerHost = jobs
	t.ResponseHeaderTimeout = 20 * time.Second
	t.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: t, Timeout: 60 * time.Second}
}

func retryDelay(h string, attempt int) time.Duration {
	if n, err := strconv.ParseUint(h, 10, 32); err == nil {
		return min(time.Duration(n)*time.Second, 30*time.Second)
	}
	if t, err := http.ParseTime(h); err == nil {
		return min(max(0, time.Until(t)), 30*time.Second)
	}
	return (250 * time.Millisecond << attempt) + time.Duration(rand.Int64N(int64(250*time.Millisecond)))
}
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func transient(err error) bool {
	var n net.Error
	return errors.As(err, &n) && n.Timeout() || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

func (e *Engine) fetch(ctx context.Context, u string, limit int64) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case e.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			<-e.slots
			return nil, err
		}
		req.Header.Set("User-Agent", "fh5dl/2 (+https://github.com/f1amee-dev/fh5dl)")
		resp, err := e.Client.Do(req)
		var data []byte
		var delay time.Duration
		retry := false
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				data, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
				if int64(len(data)) > limit {
					err = fmt.Errorf("response exceeds %d-byte limit: %s", limit, u)
				}
				retry = transient(err)
			} else {
				err = &HTTPError{Status: resp.StatusCode, URL: u}
				retry = resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 599
				delay = retryDelay(resp.Header.Get("Retry-After"), attempt)
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			}
			resp.Body.Close()
		} else {
			retry = transient(err)
		}
		<-e.slots
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retry || attempt >= 2 {
			return nil, err
		}
		if delay == 0 {
			delay = retryDelay("", attempt)
		}
		if e.Log != nil {
			e.Log(fmt.Sprintf("retry %d: %s (%v)", attempt+1, u, err))
		}
		if err := wait(ctx, delay); err != nil {
			return nil, err
		}
	}
}
