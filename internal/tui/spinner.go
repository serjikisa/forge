package tui

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (t *TUI) StartSpinner(msg string) {
	t.spinMu.Lock()
	defer t.spinMu.Unlock()
	if t.spinning {
		return
	}
	t.spinning = true
	t.spinStart = time.Now()
	t.spinStop = make(chan struct{})
	t.spinWg.Add(1)
	go t.runSpinner(msg)
}

func (t *TUI) StopSpinner() {
	t.spinMu.Lock()
	if !t.spinning {
		t.spinMu.Unlock()
		return
	}
	t.spinning = false
	close(t.spinStop)
	t.spinMu.Unlock()
	// Ensure spinner is visible for at least minSpinTime
	if elapsed := time.Since(t.spinStart); elapsed < minSpinTime {
		time.Sleep(minSpinTime - elapsed)
	}
	t.spinWg.Wait()
}

func (t *TUI) runSpinner(msg string) {
	defer t.spinWg.Done()
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		frame := spinnerFrames[i%len(spinnerFrames)]
		fmt.Fprintf(t.term, "\r  %s %s", Magenta(frame), Dim(msg))
		i++
		select {
		case <-t.spinStop:
			fmt.Fprint(t.term, "\r\033[2K")
			return
		case <-ticker.C:
		}
	}
}

const minSpinTime = 200 * time.Millisecond

// spinnerFields holds the fields needed by the spinner. Embed in TUI struct.
type spinnerFields struct {
	spinning  bool
	spinStart time.Time
	spinStop  chan struct{}
	spinMu    sync.Mutex
	spinWg    sync.WaitGroup
}
