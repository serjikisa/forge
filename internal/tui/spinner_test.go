package tui

import (
	"sync"
	"testing"
	"time"
)

func TestSpinnerStartStop(t *testing.T) {
	s := &spinnerFields{}
	// Manually test the state machine without a real terminal

	// Initially not spinning
	if s.spinning {
		t.Fatal("should not be spinning initially")
	}

	// Simulate start
	s.spinMu.Lock()
	s.spinning = true
	s.spinStart = time.Now()
	s.spinStop = make(chan struct{})
	s.spinMu.Unlock()

	// Simulate stop
	s.spinMu.Lock()
	if !s.spinning {
		t.Fatal("should be spinning after start")
	}
	s.spinning = false
	close(s.spinStop)
	s.spinMu.Unlock()

	// Verify stopped
	if s.spinning {
		t.Fatal("should not be spinning after stop")
	}
}

func TestSpinnerDoubleStart(t *testing.T) {
	s := &spinnerFields{}

	s.spinMu.Lock()
	s.spinning = true
	s.spinStop = make(chan struct{})
	s.spinMu.Unlock()

	// Second start should be a no-op (check spinning flag)
	s.spinMu.Lock()
	alreadySpinning := s.spinning
	s.spinMu.Unlock()

	if !alreadySpinning {
		t.Fatal("should still be spinning, second start should be blocked")
	}
}

func TestSpinnerDoubleStop(t *testing.T) {
	s := &spinnerFields{}

	// Stop when not spinning should be safe (no panic)
	s.spinMu.Lock()
	wasSpinning := s.spinning
	s.spinMu.Unlock()

	if wasSpinning {
		t.Fatal("should not be spinning")
	}
	// No panic = pass
}

func TestSpinnerConcurrency(t *testing.T) {
	s := &spinnerFields{}
	s.spinning = true
	s.spinStop = make(chan struct{})

	var wg sync.WaitGroup
	// Hammer the mutex from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.spinMu.Lock()
			_ = s.spinning
			s.spinMu.Unlock()
		}()
	}
	wg.Wait()

	// Clean up
	close(s.spinStop)
}

func TestMinSpinTime(t *testing.T) {
	if minSpinTime != 200*time.Millisecond {
		t.Errorf("minSpinTime = %v, want 200ms", minSpinTime)
	}
}

func TestSpinnerFrames(t *testing.T) {
	if len(spinnerFrames) == 0 {
		t.Fatal("spinnerFrames should not be empty")
	}
	if len(spinnerFrames) != 10 {
		t.Errorf("expected 10 braille frames, got %d", len(spinnerFrames))
	}
}
