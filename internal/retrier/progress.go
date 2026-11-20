package retrier

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Progress is a snapshot of the retry queue counters.
type Progress struct {
	// Total is the number of tracked URLs, including URLs still pending.
	Total int
	// Done is the number of URLs in a terminal state.
	Done int
	// Pending is the number of URLs that still need more attempts.
	Pending int
	// Completed is the number of successful URLs.
	Completed int
	// NotFound is the number of URLs that returned HTTP 404.
	NotFound int
	// Invalid is the number of URLs rejected by their filter.
	Invalid int
	// Failed is the number of URLs that exhausted their retries.
	Failed int
	// Current is the URL being attempted or the last URL attempted.
	Current string
	// CurrentAttempt is the attempt number for Current, or 0 when no URL has
	// been attempted yet.
	CurrentAttempt int
}

// progress returns a snapshot of the queue counters.
func (b *base) progress() Progress {
	b.mu.Lock()
	defer b.mu.Unlock()

	progress := Progress{Total: len(b.states)}

	for _, state := range b.states {
		switch {
		case state.Completed:
			progress.Completed++
			progress.Done++
		case state.NotFound:
			progress.NotFound++
			progress.Done++
		case state.Invalid:
			progress.Invalid++
			progress.Done++
		case state.Failed:
			progress.Failed++
			progress.Done++
		}
	}

	progress.Pending = progress.Total - progress.Done
	progress.Current = b.current
	if state := b.states[b.current]; state != nil {
		progress.CurrentAttempt = state.Attempts
	}

	return progress
}

// showProgress clears the terminal and prints the current queue progress.
func (b *base) showProgress(progress Progress, start time.Time) {
	failed := progress.Failed + progress.NotFound + progress.Invalid

	fmt.Print("\x1b[H\x1b[2J")
	fmt.Println(color("1;36", "Catsync · Progreso"))
	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("Duración: %s\n", time.Since(start).Round(time.Second))
	fmt.Println()

	fmt.Println(color("1", "Páginas"))
	fmt.Printf("  %s\n", progressBar(progress.Done, progress.Total, 40))
	fmt.Println()

	fmt.Printf("Completadas: %d\n", progress.Completed)
	fmt.Printf("Errores:     %d\n", failed)
	fmt.Printf("Pendientes:  %d\n", progress.Pending)

	if progress.Current != "" {
		fmt.Println()
		fmt.Println(color("1", "Página actual:"))
		fmt.Printf("  %s\n", truncate(progress.Current, 100))
		if progress.CurrentAttempt > 0 {
			fmt.Printf("  Intento: %d\n", progress.CurrentAttempt)
		}
	}
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

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
