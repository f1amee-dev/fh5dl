package book

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestParseID(t *testing.T) {
	for _, s := range []string{"pub/book", "https://online.fliphtml5.com/pub/book", "online.fliphtml5.com/pub/book/", "https://online.fliphtml5.com/pub/book/index.html#p=3", "https://online.fliphtml5.com/pub/book/?x=y", "fliphtml5.com/pub/book/a-title/", "http://online.fliphtml5.com/pub/book", "www.fliphtml5.com/pub/book/a-title/"} {
		t.Run(s, func(t *testing.T) {
			id, err := ParseID(s)
			if err != nil || id != "pub/book" {
				t.Fatalf("%q %v", id, err)
			}
		})
	}
	for _, s := range []string{"", "https://evil.com/pub/book", "https://online.fliphtml5.com.evil.com/pub/book", "https://user@online.fliphtml5.com/pub/book", "https://online.fliphtml5.com:80/pub/book", "https://online.fliphtml5.com/pub/book/extra", "pub/../book", "https://fliphtml5.com/book/12345/title", "ftp://online.fliphtml5.com/pub/book", "https://online.fliphtml5.com/pub/%2F"} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseID(s); err == nil {
				t.Fatal("accepted invalid URL")
			}
		})
	}
	_, err := ParseID("fliphtml5.com/book/1234/title")
	if !strings.Contains(err.Error(), "share URL") {
		t.Fatal(err)
	}
}
func TestRealDecoderFixtures(t *testing.T) {
	for _, id := range []string{"kzpyj_naht", "kzpyj_dnfe", "kzpyj_idoe", "kzpyj_mdmp", "whcsi_wpce"} {
		t.Run(id, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + id + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Pages string `json:"fliphtml5_pages"`
			}
			if err = json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeString(cfg.Pages)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err = json.NewDecoder(bytes.NewReader(decoded)).Decode(&got); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile("testdata/" + id + ".pages.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("decoded pages differ from viewer ground truth")
			}
			b, err := ParseConfig(strings.ReplaceAll(id, "_", "/"), data)
			if err != nil {
				t.Fatal(err)
			}
			if len(b.Pages) != len(want.([]any)) {
				t.Fatal("wrong page count")
			}
		})
	}
}
func TestDecoderMalformed(t *testing.T) {
	for _, s := range []string{"", "v02", "v010001", "v010001ffffff", "v010001100001x", "v010001100002!!", "v010001100004AAAA", "v01zzzz"} {
		if _, err := DecodeString(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	b, err := DecodeString("v010001000003abc")
	if err != nil || string(b) != "abc" {
		t.Fatalf("%s %v", b, err)
	}
}
func FuzzDecoder(f *testing.F) {
	f.Add("v010001100004AAAA")
	f.Fuzz(func(t *testing.T, s string) { _, _ = DecodeString(s) })
}
func TestConfigURLs(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"1.webp", "https://online.fliphtml5.com/pub/book/files/large/1.webp"}, {"./files/page/1.jpg", "https://online.fliphtml5.com/pub/book/files/page/1.jpg"}, {"//cdn.example/1.jpg", "https://cdn.example/1.jpg"}, {"/1.jpg", "https://online.fliphtml5.com/1.jpg"}, {"https://cdn.example/1.jpg?q=1", "https://cdn.example/1.jpg?q=1"}, {"1.jpg?t=a/b", "https://online.fliphtml5.com/pub/book/files/large/1.jpg?t=a/b"}} {
		data := fmt.Sprintf(`var htmlConfig = {"fliphtml5_pages":[{"n":%q}]}; var other = {};`, tc.name)
		b, err := ParseConfig("pub/book", []byte(data))
		if err != nil || b.Pages[0] != tc.want {
			t.Fatalf("%s: %+v %v", tc.name, b, err)
		}
	}
	for _, s := range []string{`{"fliphtml5_pages":[]}`, `{"fliphtml5_pages":[{"n":[]}]}`, `{"fliphtml5_pages":[{"n":["a","b"]}]}`, `{"fliphtml5_pages":[{"n":"javascript:alert(1)"}]}`, `{"fliphtml5_pages":[{"n":""}]}`} {
		if _, err := ParseConfig("pub/book", []byte(s)); err == nil {
			t.Fatal(s)
		}
	}
}
func TestFilename(t *testing.T) {
	for _, s := range []string{"CON", "NUL", "COM1", "...", "***", "a\nb", "čćž世界" + strings.Repeat("世界", 100)} {
		name := Filename(s, "pub/book")
		if !utf8.ValidString(name) || len(name) > 240 || strings.ContainsAny(name, "\n/\\:*?\"<>|") {
			t.Fatal(name)
		}
		if name == ".pdf" {
			t.Fatal(name)
		}
	}
	if Filename("", "pub/book") != "pub-book.pdf" {
		t.Fatal("fallback")
	}
	if Filename("CON", "pub/book")[0] != '_' {
		t.Fatal("reserved")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, data []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}
}
func jpegBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}
func TestEngineForceSkipIsolationAndCleanup(t *testing.T) {
	e := New(Options{Output: t.TempDir(), Jobs: 2})
	defer e.Close()
	var mu sync.Mutex
	imageRequests := 0
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "config.js") {
			return response(200, []byte(`{"meta":{"title":"Same"},"fliphtml5_pages":[{"n":"1.jpg"},{"n":"2.jpg"}]}`)), nil
		}
		mu.Lock()
		imageRequests++
		mu.Unlock()
		return response(200, jpegBytes()), nil
	})
	ctx := context.Background()
	r, err := e.Download(ctx, "pub/book", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		e.Options.Force = true
		r, err = e.Download(ctx, "pub/book", nil)
		if err != nil {
			t.Fatal(err)
		}
		n, err := api.PageCountFile(ctx, r.Path)
		if err != nil || n != 2 {
			t.Fatalf("count=%d err=%v", n, err)
		}
	}
	before := imageRequests
	e.Options.Force = false
	r, err = e.Download(ctx, "pub/book", nil)
	if err != nil || !r.Skipped || imageRequests != before {
		t.Fatal("skip failed", err)
	}
	other, err := e.Download(ctx, "pub/other", nil)
	if err != nil || other.Path == r.Path {
		t.Fatal("identity collision", err)
	}
	entries, _ := os.ReadDir(e.Options.Output)
	if len(entries) != 2 {
		t.Fatal("temporary files left", entries)
	}
	e.Options.Force = true
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "config.js") {
			return response(200, []byte(`{"meta":{"title":"Same"},"fliphtml5_pages":[{"n":"1.jpg"}]}`)), nil
		}
		return response(200, []byte("bad image")), nil
	})
	if _, err = e.Download(ctx, "pub/book", nil); err == nil {
		t.Fatal("accepted corrupt image")
	}
	n, err := api.PageCountFile(ctx, r.Path)
	if err != nil || n != 2 {
		t.Fatal("damaged existing output")
	}
}
func TestHTTPRetryAndCancellation(t *testing.T) {
	e := New(Options{Jobs: 1})
	defer e.Close()
	requests := 0
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return response(503, nil), nil
		}
		return response(200, []byte("ok")), nil
	})
	b, err := e.fetch(context.Background(), "https://example.com", 10)
	if err != nil || string(b) != "ok" || requests != 2 {
		t.Fatal(requests, err)
	}
	requests = 0
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { requests++; return response(403, nil), nil })
	if _, err = e.fetch(context.Background(), "https://example.com", 10); err == nil || requests != 1 {
		t.Fatal("permanent error retried")
	}
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := response(429, nil)
		resp.Header.Set("Retry-After", "60")
		return resp, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = e.fetch(ctx, "https://example.com", 10); err == nil || time.Since(start) > time.Second {
		t.Fatal("cancellation ignored")
	}
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, []byte("too large")), nil })
	if _, err = e.fetch(context.Background(), "https://example.com", 2); err == nil {
		t.Fatal("size limit ignored")
	}
}
func TestKeepImages(t *testing.T) {
	e := New(Options{Output: t.TempDir(), Jobs: 1, KeepImages: true})
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "config.js") {
			return response(200, []byte(`{"fliphtml5_pages":[{"n":"1.jpg"}]}`)), nil
		}
		return response(200, jpegBytes()), nil
	})
	r, err := e.Download(context.Background(), "pub/book", nil)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(r.Images, "*.jpg"))
	if len(files) != 1 {
		t.Fatal(files)
	}
}

func TestSettingsAndUnknownShapes(t *testing.T) {
	good := `{"bookConfig":{"totalPageCount":"1","largePath":["https://cdn.example/hi/"]},"fliphtml5_pages":[{"n":["x.jpg"],"t":"thumb.jpg"}]}`
	b, err := ParseConfig("pub/book", []byte(good))
	if err != nil || b.Pages[0] != "https://cdn.example/hi/x.jpg" {
		t.Fatal(b, err)
	}
	// New viewer keys and an empty largePath must not break downloads.
	for _, data := range []string{`{"fliphtml5_pages":[{"n":"1.jpg","future":true}]}`, `{"bookConfig":{"largePath":"","normalPath":["files/page/"]},"fliphtml5_pages":[{"n":"1.jpg"}]}`} {
		if b, err = ParseConfig("pub/book", []byte(data)); err != nil {
			t.Fatal(data, err)
		}
	}
	if b.Pages[0] != "https://online.fliphtml5.com/pub/book/files/page/1.jpg" {
		t.Fatal(b.Pages)
	}
	for _, data := range []string{`{"bookConfig":{"totalPageCount":2},"fliphtml5_pages":[{"n":"1.jpg"}]}`, `{"fliphtml5_pages":[{"n":42}]}`, `{"bookConfig":{"largePath":["a","b"]},"fliphtml5_pages":[{"n":"1.jpg"}]}`} {
		if _, err := ParseConfig("pub/book", []byte(data)); err == nil {
			t.Fatal("unknown/incomplete config accepted", data)
		}
	}
}
func TestCheckAllPagesAndMissing(t *testing.T) {
	e := New(Options{Jobs: 2, Output: t.TempDir()})
	defer e.Close()
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "config.js") {
			return response(200, []byte(`{"fliphtml5_pages":[{"n":"1.jpg"},{"n":"2.jpg"},{"n":"3.jpg"}]}`)), nil
		}
		if strings.Contains(r.URL.Path, "2.") {
			return response(404, nil), nil
		}
		return response(200, jpegBytes()), nil
	})
	r, err := e.Check(context.Background(), "pub/book", nil)
	if err == nil || r.Pages != 3 || r.Formats["jpeg"] != 2 || len(r.Missing) != 1 || r.Missing[0].Page != 2 {
		t.Fatal(r, err)
	}
	files, _ := os.ReadDir(e.Options.Output)
	if len(files) > 0 {
		t.Fatal("check wrote files")
	}
}
func TestNoThousandPageCap(t *testing.T) {
	e := New(Options{Jobs: 4, Output: t.TempDir()})
	defer e.Close()
	pages := strings.Repeat(`{"n":"1.jpg"},`, 1000) + `{"n":"1.jpg"}`
	data := []byte(`{"fliphtml5_pages":[` + pages + `]}`)
	jpeg := jpegBytes()
	e.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "config.js") {
			return response(200, data), nil
		}
		return response(200, jpeg), nil
	})
	r, err := e.Download(context.Background(), "pub/book", nil)
	if err != nil || r.Pages != 1001 {
		t.Fatal(r, err)
	}
	n, err := api.PageCountFile(context.Background(), r.Path)
	if err != nil || n != 1001 {
		t.Fatal(n, err)
	}
}
func TestLongAliasesAndIdentity(t *testing.T) {
	id := "publisher/" + strings.Repeat("long-alias-", 30)
	if got, err := ParseID(id); err != nil || got != id {
		t.Fatal(got, err)
	}
	if len(Filename("title", id)) > 240 {
		t.Fatal("unbounded filename")
	}
	if Filename("title", "pub-a/book") == Filename("title", "pub/a-book") {
		t.Fatal("ambiguous identity suffix")
	}
}

func TestConnectionResetsAreTransient(t *testing.T) {
	for _, e := range append([]syscall.Errno{syscall.ECONNRESET, syscall.EPIPE}, platformResets...) {
		err := &url.Error{Op: "Get", URL: "u", Err: &net.OpError{Op: "read", Err: os.NewSyscallError("wsarecv", e)}}
		if !transient(err) {
			t.Fatal(e)
		}
	}
}
