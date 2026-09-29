package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Pratyush-who/WhatsDown/internal/whatsapp"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type ProgressBar struct {
	mu         sync.Mutex
	writer     io.Writer
	startTime  time.Time
	frameIdx   int
	ticker     *time.Ticker
	done       chan struct{}
	stopped    bool
	progress   whatsapp.SyncProgress
	customText string
	lastLen    int
}

func NewProgressBar(w io.Writer) *ProgressBar {
	if w == nil {
		w = os.Stdout
	}
	pb := &ProgressBar{
		writer:    w,
		startTime: time.Now(),
		ticker:    time.NewTicker(80 * time.Millisecond),
		done:      make(chan struct{}),
	}

	go pb.animate()
	return pb
}

func (p *ProgressBar) animate() {
	for {
		select {
		case <-p.done:
			return
		case <-p.ticker.C:
			p.mu.Lock()
			if p.stopped {
				p.mu.Unlock()
				return
			}
			p.frameIdx = (p.frameIdx + 1) % len(spinnerFrames)
			p.render()
			p.mu.Unlock()
		}
	}
}

func (p *ProgressBar) Update(prog whatsapp.SyncProgress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.progress = prog
	p.customText = ""
	p.render()
}

func (p *ProgressBar) SetText(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.customText = text
	p.render()
}

func (p *ProgressBar) render() {
	frame := spinnerFrames[p.frameIdx]
	elapsed := time.Since(p.startTime).Truncate(100 * time.Millisecond)

	var sb strings.Builder
	sb.WriteString(frame)
	sb.WriteString(" ")

	if p.customText != "" {
		sb.WriteString(p.customText)
		sb.WriteString(fmt.Sprintf(" (%s)", elapsed))
	} else if p.progress.StageName != "" {
		if p.progress.StageTotal > 1 {
			sb.WriteString(fmt.Sprintf("[%d/%d] ", p.progress.StageIndex, p.progress.StageTotal))
		}
		sb.WriteString(p.progress.StageName)

		if p.progress.Total > 0 {
			percent := float64(p.progress.Current) / float64(p.progress.Total)
			if percent > 1.0 {
				percent = 1.0
			}
			barWidth := 16
			filled := int(percent * float64(barWidth))
			if filled > barWidth {
				filled = barWidth
			}

			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			sb.WriteString(fmt.Sprintf(" [%s] %3.0f%% (%d/%d)", bar, percent*100, p.progress.Current, p.progress.Total))
		}

		if p.progress.Description != "" {
			desc := p.progress.Description
			if len(desc) > 30 {
				desc = desc[:27] + "..."
			}
			sb.WriteString(" • ")
			sb.WriteString(desc)
		}

		sb.WriteString(fmt.Sprintf(" (%s)", elapsed))
	} else {
		sb.WriteString(fmt.Sprintf("Syncing... (%s)", elapsed))
	}

	line := sb.String()
	// Clear previous line content and rewrite
	pad := ""
	if len(line) < p.lastLen {
		pad = strings.Repeat(" ", p.lastLen-len(line))
	}
	p.lastLen = len(line)

	fmt.Fprintf(p.writer, "\r%s%s", line, pad)
}

func (p *ProgressBar) clear() {
	if p.lastLen > 0 {
		fmt.Fprintf(p.writer, "\r%s\r", strings.Repeat(" ", p.lastLen))
		p.lastLen = 0
	}
}

func (p *ProgressBar) StopWithSuccess(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.ticker.Stop()
	close(p.done)

	p.clear()
	elapsed := time.Since(p.startTime).Truncate(100 * time.Millisecond)
	fmt.Fprintf(p.writer, "✔ %s (%s)\n", message, elapsed)
}

func (p *ProgressBar) StopWithError(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.ticker.Stop()
	close(p.done)

	p.clear()
	elapsed := time.Since(p.startTime).Truncate(100 * time.Millisecond)
	fmt.Fprintf(p.writer, "✖ %s (%s)\n", message, elapsed)
}
