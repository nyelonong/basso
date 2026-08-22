package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type manualTime struct {
	mu  sync.Mutex
	now time.Time
}

func newManualTime() *manualTime {
	return &manualTime{now: time.Unix(0, 0)}
}

func (m *manualTime) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *manualTime) Advance(d time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(d)
	m.mu.Unlock()
}

func waitForCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

func clockActive(clock *PausableClock) bool {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.active
}

func TestPausableClock_NowSubtractsFrozenTime(t *testing.T) {
	real := newManualTime()
	pace := newPausableClock(real.Now)
	pace.Pause()

	done := make(chan error, 1)
	go func() { done <- pace.WaitUntil(context.Background(), pace.Now()) }()
	waitForCondition(t, func() bool { return clockActive(pace) })

	real.Advance(30 * time.Second)
	if got, want := pace.Frozen(), 30*time.Second; got != want {
		t.Fatalf("mid-pause Frozen() = %v, want %v", got, want)
	}
	if got, want := pace.Now(), real.Now().Add(-30*time.Second); !got.Equal(want) {
		t.Fatalf("paused Now() = %v, want %v", got, want)
	}

	pace.Resume()
	if err := <-done; err != nil {
		t.Fatalf("WaitUntil() error = %v, want nil", err)
	}
	real.Advance(10 * time.Second)
	if got, want := pace.Now(), real.Now().Add(-30*time.Second); !got.Equal(want) {
		t.Fatalf("resumed Now() = %v, want %v", got, want)
	}
}

func TestPausableClock_PauseAtBoundarySignalsOnlyAfterCurrentBar(t *testing.T) {
	pace := NewPausableClock()
	bar := 50 * time.Millisecond
	reached := pace.PauseAtBoundary()
	done := make(chan error, 1)
	go func() { done <- pace.WaitUntil(context.Background(), pace.Now().Add(bar)) }()

	select {
	case <-reached:
		t.Fatal("pause boundary signaled before current bar elapsed")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-reached:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("pause boundary was not signaled")
	}
	if !clockActive(pace) {
		t.Fatal("clock is not active-paused after boundary signal")
	}
	pace.Resume()
	if err := <-done; err != nil {
		t.Fatalf("WaitUntil() error = %v", err)
	}
}

func TestPausableClock_PauseHoldsAtEndOfCurrentBar(t *testing.T) {
	pace := NewPausableClock()
	bar := 50 * time.Millisecond
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- pace.WaitUntil(context.Background(), pace.Now().Add(bar)) }()

	time.Sleep(10 * time.Millisecond)
	pace.Pause()
	waitForCondition(t, func() bool { return clockActive(pace) })
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("pause activated after %v, want current bar to finish first", elapsed)
	}
	select {
	case err := <-done:
		t.Fatalf("WaitUntil returned while paused: %v", err)
	default:
	}

	time.Sleep(40 * time.Millisecond)
	pace.Resume()
	if err := <-done; err != nil {
		t.Fatalf("WaitUntil() error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("WaitUntil elapsed = %v, want bar plus pause hold", elapsed)
	}
}

func TestPausableClock_ResumeCancelsPendingPause(t *testing.T) {
	pace := NewPausableClock()
	pace.Pause()
	pace.Resume()
	if err := pace.WaitUntil(context.Background(), pace.Now()); err != nil {
		t.Fatalf("WaitUntil() error = %v, want nil", err)
	}
	if clockActive(pace) || pace.Frozen() != 0 {
		t.Fatalf("cancelled pending pause left clock active=%v frozen=%v", clockActive(pace), pace.Frozen())
	}
}

func TestEngine_StreamLeavesSinkLifecycleToCaller(t *testing.T) {
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}
	provider := &scriptedProvider{bars: []barScript{{bpm: 120, stepsPerBar: 16}}}

	err := engine.Stream(context.Background(), provider)
	if !errors.Is(err, errPatternExhausted) {
		t.Fatalf("Stream() error = %v, want %v", err, errPatternExhausted)
	}
	if sink.startCalls != 0 || sink.teardownCalls != 0 {
		t.Fatalf("sink lifecycle = (%d starts, %d teardowns), want (0, 0)", sink.startCalls, sink.teardownCalls)
	}
}

func TestSessionSink_ShiftsPauseAndRebasesRestart(t *testing.T) {
	real := newManualTime()
	pace := newPausableClock(real.Now)
	pace.frozen = 3 * time.Second
	inner := &fakeSink{}
	sink := NewSessionSink(inner, pace)

	sink.Start()
	sink.SetFire("a.wav", 2*time.Second, 0, 1, 0)
	real.Advance(20 * time.Second)
	sink.Rebase()
	sink.SetFireNote("C2", "bass", time.Second, 2*time.Second, 0.5, -0.2)
	sink.Teardown()

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if inner.startCalls != 1 || inner.teardownCalls != 1 {
		t.Fatalf("inner lifecycle = (%d starts, %d teardowns), want (1, 1)", inner.startCalls, inner.teardownCalls)
	}
	if got, want := inner.fires[0].begin, 2*time.Second; got != want {
		t.Fatalf("initial sample begin = %v, want %v", got, want)
	}
	if got, want := inner.fires[1].begin, 21*time.Second; got != want {
		t.Fatalf("rebased note begin = %v, want %v", got, want)
	}
}
