package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCollect(t *testing.T) {
	dir := t.TempDir()
	for n, v := range map[string]string{"a.txt": "# comment\n\npub/book\nhttps://online.fliphtml5.com/pub/other#p=2\n", "b.txt": "pub/book\npub/third\n", "ignored.md": "bad"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(v), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := collect([]string{"pub/book"}, []string{dir, "-"}, strings.NewReader("# comment\npub/fourth\n"))
	if err != nil || !reflect.DeepEqual(ids, []string{"pub/book", "pub/other", "pub/third", "pub/fourth"}) {
		t.Fatal(ids, err)
	}
}
func TestCLIUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"-j", "0", "pub/book"}, {"https://evil.com/pub/book"}, {"--interactive"}} {
		var out, err bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &err, "test"); code != 2 {
			t.Fatalf("%v exit %d", args, code)
		}
	}
	var out, err bytes.Buffer
	if code := Run(context.Background(), []string{"--version"}, nil, &out, &err, "2.0.0"); code != 0 || out.String() != "fh5dl 2.0.0\n" {
		t.Fatal(code, out.String())
	}
}
func TestTUIInputAndCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := textinput.New()
	input.Focus()
	m := tuiModel{input: input, ctx: ctx, cancel: cancel}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qyč")})
	m = next.(tuiModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(tuiModel)
	if m.input.Value() != "qy" {
		t.Fatal(m.input.Value())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(tuiModel)
	if !m.cancelled || m.running || ctx.Err() == nil {
		t.Fatal("cancel submitted")
	}
}
