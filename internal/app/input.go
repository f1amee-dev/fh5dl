package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/f1amee-dev/fh5dl/v2/internal/book"
)

func collect(args, inputs []string, stdin io.Reader) ([]string, error) {
	values := append([]string(nil), args...)
	read := func(r io.Reader, label string) error {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 4096), 1<<20)
		line := 0
		for s.Scan() {
			line++
			v := strings.TrimSpace(strings.TrimPrefix(s.Text(), "\ufeff"))
			if v == "" || strings.HasPrefix(v, "#") {
				continue
			}
			values = append(values, v)
		}
		if err := s.Err(); err != nil {
			return fmt.Errorf("%s:%d: %w", label, line, err)
		}
		return nil
	}
	for _, input := range inputs {
		if input == "-" {
			if err := read(stdin, "stdin"); err != nil {
				return nil, err
			}
			continue
		}
		st, err := os.Stat(input)
		if err != nil {
			return nil, err
		}
		files := []string{input}
		if st.IsDir() {
			files = nil
			entries, err := os.ReadDir(input)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".txt") {
					files = append(files, filepath.Join(input, entry.Name()))
				}
			}
		}
		for _, p := range files {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			err = read(f, p)
			f.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	seen := map[string]bool{}
	var ids []string
	for _, v := range values {
		id, err := book.ParseID(v)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", v, err)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}
