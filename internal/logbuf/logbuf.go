// Package logbuf keeps the most recent log lines in memory for the admin UI.
package logbuf

import (
	"strings"
	"sync"
)

type Buffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func New(max int) *Buffer { return &Buffer{max: max} }

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		b.lines = append(b.lines, l)
	}
	if over := len(b.lines) - b.max; over > 0 {
		b.lines = append([]string(nil), b.lines[over:]...)
	}
	return len(p), nil
}

// Lines returns up to n most recent lines, newest last.
func (b *Buffer) Lines(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > len(b.lines) {
		n = len(b.lines)
	}
	return append([]string(nil), b.lines[len(b.lines)-n:]...)
}
