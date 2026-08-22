package engine

import (
	"context"
	"sync"
	"time"
)

// PausableClock is a clock whose virtual time freezes while paused, so an
// Engine paced by it stretches bar boundaries across pauses without losing
// a beat: the bar in flight always finishes sounding, and the next bar
// starts exactly one bar-duration after resume.
//
// A pause request never takes effect mid-bar. It is applied at the next
// WaitUntil entry — the engine's only pacing boundary — which guarantees
// Frozen() is stable while a bar's hits are being scheduled, so
// NewPauseAwareSink can compensate begin offsets exactly.
type PausableClock struct {
	now func() time.Time

	mu           sync.Mutex
	pending      bool // pause requested, applied at next WaitUntil
	active       bool // virtual time currently frozen
	pauseStart   time.Time
	frozen       time.Duration
	pauseReached chan struct{}
	wake         chan struct{}
}

// NewPausableClock returns a PausableClock pacing off the real wall clock.
func NewPausableClock() *PausableClock {
	return newPausableClock(time.Now)
}

func newPausableClock(now func() time.Time) *PausableClock {
	return &PausableClock{now: now, wake: make(chan struct{})}
}

// Pause requests a freeze at the next bar boundary.
func (c *PausableClock) Pause() { c.PauseAtBoundary() }

// PauseAtBoundary returns a signal closed when the freeze takes effect.
func (c *PausableClock) PauseAtBoundary() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		reached := make(chan struct{})
		close(reached)
		return reached
	}
	if c.pending {
		return c.pauseReached
	}
	c.pending = true
	c.pauseReached = make(chan struct{})
	return c.pauseReached
}

// Resume unfreezes virtual time. Resuming with no pause in effect (including
// a still-pending request) is a no-op.
func (c *PausableClock) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending && !c.active {
		c.pending = false
		if c.pauseReached != nil {
			close(c.pauseReached)
			c.pauseReached = nil
		}
		return
	}
	if !c.active {
		return
	}
	c.frozen += c.now().Sub(c.pauseStart)
	c.active = false
	c.pauseReached = nil
	c.closeWakeLocked()
}

func (c *PausableClock) closeWakeLocked() {
	close(c.wake)
	c.wake = make(chan struct{})
}

// Frozen returns the total time virtual time has been frozen, including an
// ongoing freeze. Stable across any single bar's hit scheduling by design.
func (c *PausableClock) Frozen() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frozenTotal(c.now())
}

func (c *PausableClock) frozenTotal(realNow time.Time) time.Duration {
	if c.active {
		return c.frozen + realNow.Sub(c.pauseStart)
	}
	return c.frozen
}

// Now returns virtual time: real time minus everything frozen so far.
func (c *PausableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	return now.Add(-c.frozenTotal(now))
}

// WaitUntil reaches the current bar boundary, then applies any pending pause.
func (c *PausableClock) WaitUntil(ctx context.Context, t time.Time) error {
	if d := t.Sub(c.Now()); d > 0 {
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	c.mu.Lock()
	if c.pending {
		c.pending = false
		c.active = true
		c.pauseStart = c.now()
		if c.pauseReached != nil {
			close(c.pauseReached)
		}
	}
	active, wake := c.active, c.wake
	c.mu.Unlock()

	if active {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// NewPacedEngine creates an Engine paced by pace instead of the real wall
// clock, so transport controls can freeze bar boundaries mid-session.
func NewPacedEngine(sink AudioSink, pace *PausableClock) *Engine {
	return &Engine{sink: sink, clock: pace}
}

// Stream behaves like Run but assumes the sink has already been started and
// leaves tearing it down to the caller, so a long-lived session can play,
// pause, stop, and play again over one open audio device.
func (e *Engine) Stream(ctx context.Context, provider PatternProvider) error {
	reference := e.clock.Now()
	var barStart time.Duration
	for bar := 0; ; bar++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		hits, bpm, stepsPerBar, err := provider.Next(bar)
		if err != nil {
			return err
		}

		stepDuration := time.Minute / time.Duration(bpm*4)
		for _, h := range hits {
			begin := barStart + time.Duration(h.Step)*stepDuration
			if h.Note != "" {
				sustain := time.Duration(h.Length) * stepDuration
				e.sink.SetFireNote(h.Note, h.Instrument, begin, sustain, h.Velocity, h.Pan)
			} else {
				e.sink.SetFire(h.Sample, begin, 0, h.Velocity, h.Pan)
			}
		}

		barStart += time.Duration(stepsPerBar) * stepDuration

		if err := e.clock.WaitUntil(ctx, reference.Add(barStart)); err != nil {
			return err
		}
	}
}

// SessionSink keeps one device timeline correct across pauses and restarts.
type SessionSink struct {
	inner AudioSink
	pace  *PausableClock

	mu         sync.Mutex
	startedAt  time.Time
	base       time.Duration
	frozenBase time.Duration
}

// NewSessionSink wraps inner with pause compensation and restart rebasing.
func NewSessionSink(inner AudioSink, pace *PausableClock) *SessionSink {
	return &SessionSink{inner: inner, pace: pace}
}

func (s *SessionSink) Start() {
	now, frozen := s.pace.now(), s.pace.Frozen()
	s.mu.Lock()
	s.startedAt, s.base, s.frozenBase = now, 0, frozen
	s.mu.Unlock()
	s.inner.Start()
}

// Rebase makes a new bar-zero stream start from the current device position.
func (s *SessionSink) Rebase() {
	now, frozen := s.pace.now(), s.pace.Frozen()
	s.mu.Lock()
	s.base = now.Sub(s.startedAt)
	s.frozenBase = frozen
	s.mu.Unlock()
}

func (s *SessionSink) shift() time.Duration {
	frozen := s.pace.Frozen()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base + frozen - s.frozenBase
}

func (s *SessionSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64) {
	s.inner.SetFire(source, begin+s.shift(), sustain, volume, pan)
}

func (s *SessionSink) SetFireNote(note string, instrument string, begin, sustain time.Duration, volume, pan float64) {
	s.inner.SetFireNote(note, instrument, begin+s.shift(), sustain, volume, pan)
}

func (s *SessionSink) Teardown() { s.inner.Teardown() }
