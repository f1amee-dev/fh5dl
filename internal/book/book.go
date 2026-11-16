package book

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var idPart = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ParseID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if !strings.Contains(s, "://") {
		if strings.HasPrefix(s, "online.fliphtml5.com/") || strings.HasPrefix(s, "fliphtml5.com/") {
			s = "https://" + s
		} else {
			s = "https://online.fliphtml5.com/" + s
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid book URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Host != "online.fliphtml5.com" && u.Host != "fliphtml5.com") {
		return "", fmt.Errorf("unsupported host: use online.fliphtml5.com or fliphtml5.com")
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) > 0 && p[0] == "book" {
		return "", fmt.Errorf("unsupported /book/<number> share URL; open the viewer and copy its online.fliphtml5.com/<publisher>/<book> URL")
	}
	if len(p) < 2 || !idPart.MatchString(p[0]) || !idPart.MatchString(p[1]) {
		return "", fmt.Errorf("expected publisher/book IDs")
	}
	if len(p) > 2 && (len(p) > 3 || u.Host == "online.fliphtml5.com" && p[2] != "index.html") {
		return "", fmt.Errorf("unexpected book URL path")
	}
	return p[0] + "/" + p[1], nil
}

type Book struct {
	ID, Title string
	Pages     []string
}
type page struct {
	W *float64        `json:"w"`
	H *float64        `json:"h"`
	P string          `json:"p"`
	N json.RawMessage `json:"n"`
	L json.RawMessage `json:"l"`
	T json.RawMessage `json:"t"`
}

func ParseConfig(id string, data []byte) (Book, error) {
	var c struct {
		Meta struct {
			Title string `json:"title"`
		} `json:"meta"`
		Pages    json.RawMessage `json:"fliphtml5_pages"`
		Settings json.RawMessage `json:"bookConfig"`
	}
	// Decode exactly one JSON value after the known assignment, without executing JS.
	s := bytes.TrimSpace(data)
	if !bytes.HasPrefix(s, []byte("{")) {
		eq := bytes.IndexByte(s, '=')
		if eq < 0 {
			return Book{}, fmt.Errorf("missing config object")
		}
		prefix := strings.TrimSpace(string(s[:eq]))
		if prefix != "var htmlConfig" && prefix != "let htmlConfig" && prefix != "const htmlConfig" && prefix != "htmlConfig" {
			return Book{}, fmt.Errorf("unsupported config wrapper")
		}
		s = bytes.TrimSpace(s[eq+1:])
	}
	if err := json.NewDecoder(bytes.NewReader(s)).Decode(&c); err != nil {
		return Book{}, fmt.Errorf("config JSON: %w", err)
	}
	raw, err := decodeValue(c.Pages)
	if err != nil {
		return Book{}, fmt.Errorf("page list: %w", err)
	}
	var pages []page
	pageDecoder := json.NewDecoder(bytes.NewReader(raw))
	pageDecoder.DisallowUnknownFields()
	if err := pageDecoder.Decode(&pages); err != nil {
		return Book{}, fmt.Errorf("page list: %w", err)
	}
	if len(pages) == 0 {
		return Book{}, fmt.Errorf("empty book: no pages")
	}
	prefix := "files/large/"
	if len(c.Settings) > 0 && string(c.Settings) != "null" {
		value, err := decodeValue(c.Settings)
		if err != nil {
			return Book{}, fmt.Errorf("bookConfig: %w", err)
		}
		var settings struct {
			Count         json.RawMessage `json:"totalPageCount"`
			Large, Normal json.RawMessage
		}
		// Viewer settings have hundreds of presentation keys; only count and image
		// base paths affect static PDF completeness and resolution.
		var bc map[string]json.RawMessage
		if err = json.NewDecoder(bytes.NewReader(value)).Decode(&bc); err != nil {
			return Book{}, fmt.Errorf("bookConfig: %w", err)
		}
		settings.Count = bc["totalPageCount"]
		settings.Large = bc["largePath"]
		settings.Normal = bc["normalPath"]
		if len(settings.Count) > 0 {
			count := strings.Trim(string(settings.Count), "\"")
			n, err := strconv.Atoi(count)
			if err != nil || n < 1 {
				return Book{}, fmt.Errorf("bookConfig: invalid totalPageCount %s", count)
			}
			if n != len(pages) {
				return Book{}, fmt.Errorf("incomplete page list: decoded %d pages, bookConfig declares %d", len(pages), n)
			}
		}
		pathRaw := settings.Large
		if len(pathRaw) == 0 || string(pathRaw) == "null" {
			pathRaw = settings.Normal
		}
		paths, err := imageNames(pathRaw)
		if err != nil || len(paths) > 1 {
			return Book{}, fmt.Errorf("bookConfig: unsupported image base paths")
		}
		if len(paths) == 1 {
			prefix = strings.ReplaceAll(paths[0], `\/`, "/")
			if strings.TrimSpace(prefix) == "" {
				return Book{}, fmt.Errorf("bookConfig: empty image base path")
			}
		}
	}
	b := Book{ID: id, Title: c.Meta.Title}
	base, _ := url.Parse("https://online.fliphtml5.com/" + id + "/")
	for i, p := range pages {
		if (p.W != nil && *p.W <= 0) || (p.H != nil && *p.H <= 0) {
			return Book{}, fmt.Errorf("page %d: invalid dimensions", i+1)
		}
		if p.P != "" && p.P != "none" {
			return Book{}, fmt.Errorf("page %d: unsupported presentation %q", i+1, p.P)
		}
		names, err := imageNames(p.L)
		if err != nil {
			return Book{}, fmt.Errorf("page %d large image: %w", i+1, err)
		}
		if len(names) == 0 {
			names, err = imageNames(p.N)
		}
		if err != nil || len(names) != 1 {
			return Book{}, fmt.Errorf("page %d: expected one image (split-image pages are unsupported)", i+1)
		}
		name := strings.ReplaceAll(strings.TrimSpace(names[0]), `\/`, "/")
		if name == "" {
			return Book{}, fmt.Errorf("page %d: empty image URL", i+1)
		}
		initial, parseErr := url.Parse(name)
		if parseErr != nil {
			return Book{}, fmt.Errorf("page %d image URL: %w", i+1, parseErr)
		}
		if !initial.IsAbs() && initial.Host == "" && !strings.Contains(initial.Path, "/") {
			name = strings.TrimRight(prefix, "/") + "/" + name
		}
		ref, err := url.Parse(name)
		if err != nil {
			return Book{}, fmt.Errorf("page %d image URL: %w", i+1, err)
		}
		u := base.ResolveReference(ref)
		if (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return Book{}, fmt.Errorf("page %d: unsupported image URL", i+1)
		}
		b.Pages = append(b.Pages, u.String())
	}
	return b, nil
}
func imageNames(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if one == "" {
			return nil, nil
		}
		return []string{one}, nil
	}
	var many []string
	err := json.Unmarshal(raw, &many)
	return many, err
}

// Filename reserves space for the identity suffix, avoiding same-title collisions.
func Filename(title, id string) string {
	var b strings.Builder
	for _, r := range title {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.Trim(b.String(), " .")
	for len(s) > 100 {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	s = strings.Trim(s, " .")
	stem := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || regexp.MustCompile(`^(COM|LPT)[1-9]$`).MatchString(stem) {
		s = "_" + s
	}
	suffix := strings.ReplaceAll(id, "/", "-")
	if len(suffix) > 100 || strings.ContainsAny(id, "-_") {
		sum := sha256.Sum256([]byte(id))
		if len(suffix) > 80 {
			suffix = suffix[:80]
		}
		suffix += fmt.Sprintf("-%x", sum[:4])
	}

	if s == "" {
		return suffix + ".pdf"
	}
	return s + " [" + suffix + "].pdf"
}

func imageCandidates(raw string) []string {
	u, _ := url.Parse(raw)
	out := []string{raw}
	paths := []string{u.Path}
	if strings.Contains(u.Path, "/files/large/") {
		paths = append(paths, strings.Replace(u.Path, "/files/large/", "/files/page/", 1), strings.Replace(u.Path, "/files/large/", "/files/", 1))
	}
	ext := strings.ToLower(path.Ext(u.Path))
	for _, p := range paths {
		v := *u
		v.Path = p
		if v.String() != raw {
			out = append(out, v.String())
		}
		alts := []string{}
		if ext == ".webp" {
			alts = []string{".jpg"}
		} else if ext == ".jpg" || ext == ".jpeg" {
			alts = []string{".webp"}
		}
		for _, e := range alts {
			v.Path = strings.TrimSuffix(p, path.Ext(p)) + e
			out = append(out, v.String())
		}
	}
	return out
}

func decodeValue(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return nil, err
		}
		return DecodeString(encoded)
	}
	return raw, nil
}
