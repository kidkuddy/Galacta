package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Extended color palette.
const (
	colorBlue       = "\033[34m"
	colorMagenta    = "\033[35m"
	colorBoldCyan   = "\033[1;36m"
	colorBoldYellow = "\033[1;33m"
)

// Spinner displays an animated activity indicator on stderr.
type Spinner struct {
	mu      sync.Mutex
	message string
	active  bool
	done    chan struct{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func NewSpinner() *Spinner {
	return &Spinner{}
}

func (s *Spinner) Start(msg string) {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		s.Stop()
		s.mu.Lock()
	}
	s.message = msg
	s.active = true
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		i := 0
		for {
			s.mu.Lock()
			if !s.active {
				s.mu.Unlock()
				return
			}
			msg := s.message
			s.mu.Unlock()

			frame := spinnerFrames[i%len(spinnerFrames)]
			fmt.Fprintf(stderr, "\033[2K\r%s%s %s%s", colorCyan, frame, msg, colorReset)

			i++
			select {
			case <-s.done:
				return
			case <-time.After(80 * time.Millisecond):
			}
		}
	}()
}

func (s *Spinner) Update(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = msg
}

func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	close(s.done)
	s.mu.Unlock()
	fmt.Fprintf(stderr, "\033[2K\r")
}

// Box drawing helpers.

func boxTop(title string, width int) string {
	if title == "" {
		return "┌" + strings.Repeat("─", width-2) + "┐"
	}
	titlePart := "─ " + title + " "
	remaining := width - 2 - len([]rune(titlePart))
	if remaining < 0 {
		remaining = 0
	}
	return "┌" + titlePart + strings.Repeat("─", remaining) + "┐"
}

func boxLine(content string, width int) string {
	runes := []rune(content)
	padding := width - 4 - len(runes)
	if padding < 0 {
		padding = 0
	}
	return "│ " + content + strings.Repeat(" ", padding) + " │"
}

func boxBottom(width int) string {
	return "└" + strings.Repeat("─", width-2) + "┘"
}

func boxBottomTag(tag string, width int) string {
	tagPart := " " + tag + " ─"
	remaining := width - 2 - len([]rune(tagPart))
	if remaining < 0 {
		remaining = 0
	}
	return "└" + strings.Repeat("─", remaining) + tagPart + "┘"
}

// Double-line box for permission prompts.

func dboxTop(title string, width int) string {
	if title == "" {
		return "╔" + strings.Repeat("═", width-2) + "╗"
	}
	titlePart := "═ " + title + " "
	remaining := width - 2 - len([]rune(titlePart))
	if remaining < 0 {
		remaining = 0
	}
	return "╔" + titlePart + strings.Repeat("═", remaining) + "╗"
}

func dboxLine(content string, width int) string {
	runes := []rune(content)
	padding := width - 4 - len(runes)
	if padding < 0 {
		padding = 0
	}
	return "║ " + content + strings.Repeat(" ", padding) + " ║"
}

func dboxBottom(width int) string {
	return "╚" + strings.Repeat("═", width-2) + "╝"
}

// Tool output box (open-ended, no right border).

func toolBoxTop(title string, width int) string {
	titlePart := "─ " + title + " "
	remaining := width - len([]rune(titlePart)) - 1
	if remaining < 0 {
		remaining = 0
	}
	return "  ┌" + titlePart + strings.Repeat("─", remaining)
}

func toolBoxLine(content string) string {
	return "  │ " + content
}

func toolBoxBottom(tag string, width int) string {
	if tag == "" {
		return "  └" + strings.Repeat("─", width-1)
	}
	tagPart := " " + tag + " "
	remaining := width - 1 - len([]rune(tagPart))
	if remaining < 0 {
		remaining = 0
	}
	return "  └" + strings.Repeat("─", remaining) + tagPart + "─"
}

// Session banner.

func printBanner(model, dir, sessionID, mode string) {
	sid := shortID(sessionID)

	shark := []string{
		`       ╱▔▔▔▔╲        `,
		`      ╱      ╲       `,
		`     ╱  ·  ·  ╲      `,
		`    ╱    ▽▽    ╲     `,
		`   ╱  ▔▔▔▔▔▔▔▔  ╲   `,
		`  ▕              ▏   `,
		`   ╲   ╱    ╲   ╱    `,
		`    ╲ ╱  jeff ╲╱     `,
	}

	fmt.Fprintln(stderr)
	for _, line := range shark {
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, line, colorReset)
	}
	fmt.Fprintln(stderr)

	info1 := fmt.Sprintf("  %s%s%s · %s", colorBold, model, colorReset, dir)
	info2 := fmt.Sprintf("  %sSession: %s · Mode: %s%s", colorDim, sid, mode, colorReset)

	fmt.Fprintf(stderr, "%s\n", info1)
	fmt.Fprintf(stderr, "%s\n\n", info2)
}

// Number formatters.

func fmtTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func fmtCost(usd float64) string {
	return fmt.Sprintf("$%.4f", usd)
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
