package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/f1amee-dev/fh5dl/v2/internal/book"
)

type finished struct {
	result book.Result
	err    error
}
type interrupted struct{}
type tuiModel struct {
	input              textinput.Model
	bar                progress.Model
	engine             *book.Engine
	ctx                context.Context
	cancel             context.CancelFunc
	events             chan tea.Msg
	running, cancelled bool
	status             string
	result             book.Result
	err                error
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, func() tea.Msg { <-m.ctx.Done(); return interrupted{} })
}
func (m tuiModel) next() tea.Cmd { return func() tea.Msg { return <-m.events } }
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.input.Width = max(1, v.Width-6)
		m.bar.Width = max(1, min(60, v.Width-6))
		return m, nil
	case interrupted:
		m.cancelled = true
		m.cancel()
		if !m.running {
			return m, tea.Quit
		}
		m.status = "Cancelling…"
		return m, nil
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			m.cancel()
			if !m.running {
				return m, tea.Quit
			}
			m.status = "Cancelling…"
			return m, nil
		case "enter":
			if m.running {
				return m, nil
			}
			id, err := book.ParseID(m.input.Value())
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.running = true
			m.status = "Loading config…"
			m.input.Blur()
			return m, func() tea.Msg {
				go func() {
					r, err := m.engine.Download(m.ctx, id, func(p book.Progress) {
						select {
						case m.events <- p:
						default:
						}
					})
					m.events <- finished{r, err}
				}()
				return <-m.events
			}
		}
	case book.Progress:
		m.status = fmt.Sprintf("%s: %s %d/%d", v.ID, v.Stage, v.Done, v.Total)
		if v.Total > 0 {
			m.bar.SetPercent(float64(v.Done) / float64(v.Total))
		}
		return m, m.next()
	case finished:
		m.result = v.result
		m.err = v.err
		return m, tea.Quit
	}
	if !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m tuiModel) View() string {
	if m.running {
		return "fh5dl\n\n" + m.status + "\n" + m.bar.ViewAs(m.bar.Percent()) + "\n\nEsc / Ctrl-C: cancel\n"
	}
	return "fh5dl\n\nBook URL or publisher/book:\n" + m.input.View() + "\n\nEnter: download · Esc / Ctrl-C: cancel\n" + m.status + "\n"
}
func runTUI(ctx context.Context, opts book.Options, seed string, in io.Reader, out, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e := book.New(opts)
	defer e.Close()
	input := textinput.New()
	input.Placeholder = "https://online.fliphtml5.com/publisher/book/"
	input.CharLimit = 4096
	input.Width = 70
	input.SetValue(strings.TrimSpace(seed))
	input.Focus()
	m := tuiModel{input: input, bar: progress.New(progress.WithDefaultGradient()), engine: e, ctx: ctx, cancel: cancel, events: make(chan tea.Msg, 64)}
	final, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(stderr), tea.WithoutSignalHandler()).Run()
	if err != nil {
		if ctx.Err() != nil {
			return 130
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	result := final.(tuiModel)
	if result.cancelled || ctx.Err() != nil {
		return 130
	}
	if result.err != nil {
		fmt.Fprintln(stderr, result.err)
		return 1
	}
	if result.result.Path != "" {
		fmt.Fprintln(out, result.result.Path)
		if result.result.Images != "" {
			fmt.Fprintln(stderr, "images:", result.result.Images)
		}
	}
	return 0
}
