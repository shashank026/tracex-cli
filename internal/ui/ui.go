// Package ui draws the CLI: colors, the thinking spinner, prompts and markdown.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// Color is off when NO_COLOR is set or output is not a terminal.
var Color = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))

func paint(code, s string) string {
	if !Color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string   { return paint("1", s) }
func Dim(s string) string    { return paint("2", s) }
func Brand(s string) string  { return paint("38;5;69", s) } // TraceX blue
func Green(s string) string  { return paint("32", s) }
func Red(s string) string    { return paint("31", s) }
func Yellow(s string) string { return paint("33", s) }

// Success, Fail and Info print one status line.
func Success(format string, a ...any) { fmt.Println(Green("✓") + " " + fmt.Sprintf(format, a...)) }
func Fail(format string, a ...any) {
	fmt.Fprintln(os.Stderr, Red("✗")+" "+fmt.Sprintf(format, a...))
}
func Info(format string, a ...any) { fmt.Println(Brand("●") + " " + fmt.Sprintf(format, a...)) }

// Width of the terminal (80 when unknown).
func Width() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}

// --- spinner -----------------------------------------------------------------------

// The TraceX logo's dots lighting up in turn, like the dashboard while it thinks
var frames = []string{"●∙∙∙", "∙●∙∙", "∙∙●∙", "∙∙∙●", "∙∙●∙", "∙●∙∙"}

// Spinner shows a live status line on stderr while TraceX works.
type Spinner struct {
	mu      sync.Mutex
	text    string
	started time.Time
	stop    chan struct{}
	done    chan struct{}
	live    bool
}

// StartSpinner begins animating text (a no-op when stderr is not a terminal).
func StartSpinner(text string) *Spinner {
	s := &Spinner{text: text, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	s.live = term.IsTerminal(int(os.Stderr.Fd()))
	if !s.live {
		close(s.done)
		return s
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.done)
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	for i := 0; ; i++ {
		s.mu.Lock()
		elapsed := int(time.Since(s.started).Seconds())
		line := fmt.Sprintf("%s %s %s", Brand(frames[i%len(frames)]), s.text, Dim(fmt.Sprintf("(%ds · ctrl+c to stop)", elapsed)))
		fmt.Fprint(os.Stderr, "\r\x1b[2K"+truncate(line, Width()+40))
		s.mu.Unlock()
		select {
		case <-s.stop:
			fmt.Fprint(os.Stderr, "\r\x1b[2K")
			return
		case <-ticker.C:
		}
	}
}

// Set changes the status text.
func (s *Spinner) Set(text string) {
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
}

// Println prints a line above the spinner.
func (s *Spinner) Println(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live {
		fmt.Fprint(os.Stderr, "\r\x1b[2K")
	}
	fmt.Println(line)
}

// Stop clears the spinner line.
func (s *Spinner) Stop() {
	if s.live {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	<-s.done
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// --- prompts -----------------------------------------------------------------------

var stdin = bufio.NewReader(os.Stdin)

// Ask reads one line; the default is used for an empty answer.
func Ask(question, def string) string {
	if def != "" {
		fmt.Printf("%s %s %s ", Brand("?"), question, Dim("["+def+"]"))
	} else {
		fmt.Printf("%s %s ", Brand("?"), question)
	}
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// Confirm asks a yes/no question.
func Confirm(question string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	answer := strings.ToLower(Ask(question+" "+Dim("("+hint+")"), ""))
	if answer == "" {
		return def
	}
	return answer == "y" || answer == "yes"
}

// Choose shows numbered options and returns the index picked.
func Choose(question string, options []string, def int) int {
	for i, option := range options {
		fmt.Printf("  %s %s\n", Dim(fmt.Sprintf("%d)", i+1)), option)
	}
	for {
		answer := Ask(question, fmt.Sprint(def+1))
		var n int
		if _, err := fmt.Sscan(answer, &n); err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
		fmt.Println(Dim("  Pick a number from the list."))
	}
}

// Interactive is true when both input and output are terminals.
func Interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// --- markdown ----------------------------------------------------------------------

// Markdown renders an answer for the terminal (plain text when colors are off).
func Markdown(text string) string {
	if !Color {
		return text
	}
	width := Width() - 4
	if width > 110 {
		width = 110
	}
	renderer, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		return text
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}
