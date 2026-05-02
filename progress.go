package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type progressMeter struct {
	label   string
	total   int
	enabled bool
	last    time.Time
	spin    int
}

func newProgressMeter(label string, total int, enabled bool) *progressMeter {
	isTTY := enabled && fileIsTTY(os.Stderr)
	if !isTTY {
		return &progressMeter{enabled: false}
	}

	// Avoid noisy output for tiny operations.
	if total > 0 && total < 200000 {
		return &progressMeter{enabled: false}
	}

	return &progressMeter{
		label:   label,
		total:   total,
		enabled: true,
		last:    time.Now().Add(-time.Second),
	}
}

func (m *progressMeter) TickKnown(done int) {
	if !m.enabled {
		return
	}
	now := time.Now()
	if now.Sub(m.last) < 120*time.Millisecond && done < m.total {
		return
	}
	if done < 0 {
		done = 0
	}
	if done > m.total {
		done = m.total
	}

	const width = 24
	filled := 0
	percent := 100.0
	if m.total > 0 {
		filled = int(float64(done) / float64(m.total) * float64(width))
		if filled < 0 {
			filled = 0
		}
		if filled > width {
			filled = width
		}
		percent = float64(done) / float64(m.total) * 100
	}
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	fmt.Fprintf(os.Stderr, "\r%s [%s] %5.1f%% (%d/%d)", m.label, bar, percent, done, m.total)
	m.last = now
}

func (m *progressMeter) TickUnknown(done int) {
	if !m.enabled {
		return
	}
	now := time.Now()
	if now.Sub(m.last) < 120*time.Millisecond {
		return
	}
	spinner := []string{"|", "/", "-", "\\"}
	fmt.Fprintf(os.Stderr, "\r%s %s %d", m.label, spinner[m.spin%len(spinner)], done)
	m.spin++
	m.last = now
}

func (m *progressMeter) FinishKnown(done int) {
	if !m.enabled {
		return
	}
	m.TickKnown(done)
	fmt.Fprintln(os.Stderr)
}

func (m *progressMeter) FinishUnknown(done int) {
	if !m.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "\r%s done: %d\n", m.label, done)
}
