package retrier

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

// ConsolePresenter writes progress to a terminal-like writer.
type ConsolePresenter struct {
	writer    io.Writer
	enabled   bool
	startedAt time.Time
}

// NewConsolePresenter creates a console presenter. Output is enabled
// automatically when w is a terminal.
func NewConsolePresenter(w io.Writer) *ConsolePresenter {
	return &ConsolePresenter{
		writer:  w,
		enabled: isTerminal(w),
	}
}

// Enable forces the presenter on, mostly for tests.
func (p *ConsolePresenter) Enable(enabled bool) {
	p.enabled = enabled
}

// Show redraws the progress dashboard.
func (p *ConsolePresenter) Show(progress domain.Progress) {
	if !p.enabled || p.writer == nil {
		return
	}
	if p.startedAt.IsZero() {
		p.startedAt = time.Now()
	}

	failed := progress.Failed + progress.NotFound + progress.Invalid

	fmt.Fprint(p.writer, "\x1b[H\x1b[2J")
	fmt.Fprintln(p.writer, color("1;36", "Catsync · Progreso"))
	fmt.Fprintln(p.writer, strings.Repeat("─", 72))
	fmt.Fprintf(p.writer, "Duración: %s\n", time.Since(p.startedAt).Round(time.Second))
	fmt.Fprintln(p.writer)

	fmt.Fprintln(p.writer, color("1", "Páginas"))
	fmt.Fprintf(p.writer, "  %s\n", progressBar(progress.Done, progress.Total, 40))
	fmt.Fprintln(p.writer)

	fmt.Fprintf(p.writer, "Completadas: %d\n", progress.Completed)
	fmt.Fprintf(p.writer, "Errores:     %d\n", failed)
	fmt.Fprintf(p.writer, "Pendientes:  %d\n", progress.Pending)

	if progress.Current != "" {
		fmt.Fprintln(p.writer)
		fmt.Fprintln(p.writer, color("1", "Página actual:"))
		fmt.Fprintf(p.writer, "  %s\n", truncate(progress.Current, 100))
		if progress.CurrentAttempt > 0 {
			fmt.Fprintf(p.writer, "  Intento: %d\n", progress.CurrentAttempt)
		}
	}
}

// Finish draws the final snapshot.
func (p *ConsolePresenter) Finish(progress domain.Progress, err error) {
	if !p.enabled || p.writer == nil {
		return
	}

	p.Show(progress)
	if err != nil {
		fmt.Fprintf(p.writer, "\nResultado: %v\n", err)
	}
	fmt.Fprintln(p.writer)
}

func progressBar(done, total, width int) string {
	if total <= 0 {
		return fmt.Sprintf("[%s] 0/0", strings.Repeat("░", width))
	}
	if done > total {
		done = total
	}

	filled := done * width / total
	percent := done * 100 / total

	return fmt.Sprintf(
		"[%s%s] %d/%d (%d%%)",
		strings.Repeat("█", filled),
		strings.Repeat("░", width-filled),
		done,
		total,
		percent,
	)
}

func color(code, text string) string {
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max-1] + "…"
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
