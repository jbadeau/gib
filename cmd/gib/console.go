package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"

	"github.com/jbadeau/gib"
)

var verbosities = []string{"quiet", "error", "warn", "lifecycle", "info", "debug"}

// console prints what a build logs as Jib's CliLogger does: errors on
// stderr as [ERROR], warnings on stdout as [WARN], everything else on
// stdout as it is, each only at a verbosity that shows it. A rich
// console keeps a progress bar beneath the messages in place of the
// progress messages a plain one prints.
type console struct {
	level    int
	rich     bool
	out, err io.Writer

	mu     sync.Mutex
	footer string
}

func newConsole(verbosity, mode string, trace bool, out, err io.Writer) *console {
	level := 0
	for i, v := range verbosities {
		if v == verbosity {
			level = i
		}
	}
	rich := false
	if !trace {
		switch mode {
		case "rich":
			rich = true
		case "auto":
			f, ok := out.(*os.File)
			rich = ok && term.IsTerminal(f.Fd()) && os.Getenv("TERM") != "dumb"
		}
	}
	return &console{level: level, rich: rich && level >= 3, out: out, err: err}
}

func (c *console) log(e gib.LogEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e.Level {
	case gib.LevelError:
		if c.level >= 1 {
			c.print(c.err, "[ERROR] "+e.Message)
		}
	case gib.LevelWarn:
		if c.level >= 2 {
			c.print(c.out, "[WARN] "+e.Message)
		}
	case gib.LevelLifecycle:
		if c.level >= 3 {
			c.print(c.out, e.Message)
		}
	case gib.LevelProgress:
		if c.level >= 3 && !c.rich {
			c.print(c.out, e.Message)
		}
	case gib.LevelInfo:
		if c.level >= 4 {
			c.print(c.out, e.Message)
		}
	case gib.LevelDebug:
		if c.level >= 5 {
			c.print(c.out, e.Message)
		}
	}
}

func (c *console) errorf(format string, args ...any) {
	c.log(gib.LogEvent{Level: gib.LevelError, Message: fmt.Sprintf(format, args...)})
}

// print prints a message above the progress bar.
func (c *console) print(w io.Writer, msg string) {
	if c.footer != "" {
		_, _ = io.WriteString(c.out, "\r\033[K")
	}
	_, _ = fmt.Fprintln(w, msg)
	if c.footer != "" {
		_, _ = io.WriteString(c.out, c.footer)
	}
}

var (
	stepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	barFilled = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	barEmpty  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// progress draws the progress bar of a rich console, one step for each
// of the build's phases and layers.
func (c *console) progress(layers int) gib.ProgressCallback {
	total := 5 + layers
	step := 0
	return func(e gib.ProgressEvent) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.rich {
			return
		}
		step++
		if e.Phase == gib.PhaseFinalizing {
			_, _ = io.WriteString(c.out, "\r\033[K")
			c.footer = ""
			return
		}
		filled := min(step*30/total, 30)
		bar := barFilled.Render(strings.Repeat("█", filled)) + barEmpty.Render(strings.Repeat("░", 30-filled))
		c.footer = bar + " " + stepStyle.Render(e.Message)
		_, _ = io.WriteString(c.out, "\r\033[K"+c.footer)
	}
}

// done clears the progress bar.
func (c *console) done() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.footer != "" {
		_, _ = io.WriteString(c.out, "\r\033[K")
		c.footer = ""
	}
}
