package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSink records AudioSink calls instead of touching a real audio device.
type fakeSink struct {
	mu            sync.Mutex
	fires         []recordedFire
	startCalls    int
	teardownCalls int
}

type recordedFire struct {
	source  string
	note    string
	begin   time.Duration
	sustain time.Duration
	volume  float64
	pan     float64
}

func (f *fakeSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fires = append(f.fires, recordedFire{source: source, begin: begin, sustain: sustain, volume: volume, pan: pan})
}

func (f *fakeSink) SetFireNote(note string, begin, sustain time.Duration, volume, pan float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fires = append(f.fires, recordedFire{note: note, begin: begin, sustain: sustain, volume: volume, pan: pan})
}

func (f *fakeSink) Start() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
}

func (f *fakeSink) Teardown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teardownCalls++
}

// fakeClock is a manually-driven clock double for Engine tests: it never
// sleeps in real time. In instant mode (newFakeClock(true)) WaitUntil jumps
// straight to the requested time and returns immediately, so tests that
// don't care about pacing stay fast. In manual mode (newFakeClock(false))
// WaitUntil blocks until the test calls Advance past the target time.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	instant bool
	waiters []fakeClockWaiter
	waited  chan time.Time // optional spy: if set, WaitUntil sends its target here before blocking
}

type fakeClockWaiter struct {
	deadline time.Time
	release  chan struct{}
}

func newFakeClock(instant bool) *fakeClock {
	return &fakeClock{now: time.Unix(0, 0), instant: instant}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) WaitUntil(ctx context.Context, t time.Time) error {
	c.mu.Lock()
	if c.instant || !t.After(c.now) {
		c.now = t
		c.mu.Unlock()
		c.signal(t)
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	release := make(chan struct{})
	c.waiters = append(c.waiters, fakeClockWaiter{deadline: t, release: release})
	c.mu.Unlock()

	c.signal(t)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-release:
		return nil
	}
}

func (c *fakeClock) signal(t time.Time) {
	if c.waited != nil {
		c.waited <- t
	}
}

// Advance moves the fake clock forward by d, releasing any WaitUntil calls
// (in manual mode) whose deadline has now passed.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var pending []fakeClockWaiter
	for _, w := range c.waiters {
		if !w.deadline.After(c.now) {
			close(w.release)
		} else {
			pending = append(pending, w)
		}
	}
	c.waiters = pending
	c.mu.Unlock()
}

// errPatternExhausted is returned by test-double PatternProviders once their
// scripted bars run out, giving Engine.Run a deterministic stopping point
// without depending on real wall-clock time.
var errPatternExhausted = errors.New("engine: pattern exhausted")

// barScript is one bar's worth of scripted PatternProvider output.
type barScript struct {
	hits        []Hit
	bpm         int
	stepsPerBar int
}

// scriptedProvider is a PatternProvider test double that plays a fixed
// sequence of bars, then returns errPatternExhausted.
type scriptedProvider struct {
	bars []barScript
}

func (p *scriptedProvider) Next(bar int) ([]Hit, int, int, error) {
	if bar >= len(p.bars) {
		return nil, 0, 0, errPatternExhausted
	}
	b := p.bars[bar]
	return b.hits, b.bpm, b.stepsPerBar, nil
}

// loopingProvider is a PatternProvider test double that returns an empty
// bar forever (bpm/stepsPerBar fixed), sending bar on notify (blocking) on
// every call so a test can single-step Engine.Run, running in another
// goroutine, one bar at a time. If stopAfter > 0, it returns
// errPatternExhausted once bar reaches stopAfter, so a test doesn't have to
// rely on context cancellation to end the loop.
type loopingProvider struct {
	bpm         int
	stepsPerBar int
	notify      chan int
	stopAfter   int
}

func (p *loopingProvider) Next(bar int) ([]Hit, int, int, error) {
	if p.notify != nil {
		p.notify <- bar
	}
	if p.stopAfter > 0 && bar >= p.stopAfter {
		return nil, 0, 0, errPatternExhausted
	}
	return nil, p.bpm, p.stepsPerBar, nil
}

func TestEngine_SchedulesOnContinuousClock(t *testing.T) {
	provider := &scriptedProvider{
		bars: []barScript{
			{
				hits: []Hit{
					{Step: 0, Sample: "kick", Pan: 0, Velocity: 1},
					{Step: 2, Sample: "snare", Pan: 0.5, Velocity: 0.8},
				},
				bpm:         120,
				stepsPerBar: 4,
			},
			{
				hits: []Hit{
					{Step: 1, Sample: "hat", Pan: -0.5, Velocity: 0.6},
				},
				bpm:         120,
				stepsPerBar: 4,
			},
		},
	}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	err := engine.Run(context.Background(), provider)
	if !errors.Is(err, errPatternExhausted) {
		t.Fatalf("Run() error = %v, want errPatternExhausted", err)
	}

	stepDuration := time.Minute / time.Duration(120*4)
	barDuration := time.Duration(4) * stepDuration
	want := []recordedFire{
		{source: "kick", begin: 0, sustain: 0, volume: 1, pan: 0},
		{source: "snare", begin: 2 * stepDuration, sustain: 0, volume: 0.8, pan: 0.5},
		{source: "hat", begin: barDuration + stepDuration, sustain: 0, volume: 0.6, pan: -0.5},
	}

	sink.mu.Lock()
	got := sink.fires
	sink.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("fires = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fire[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestEngine_PacesBarsAgainstClock proves Run does not call provider.Next
// for bar N+1 until the injected clock has advanced past bar N's duration.
func TestEngine_PacesBarsAgainstClock(t *testing.T) {
	notify := make(chan int)
	waited := make(chan time.Time)
	clk := newFakeClock(false)
	clk.waited = waited

	// stopAfter 1: bar 0 plays normally, then bar 1's Next call exhausts
	// the pattern, so the test only needs to drive one bar of pacing.
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify, stopAfter: 1}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: clk}

	done := make(chan error, 1)
	go func() { done <- engine.Run(context.Background(), provider) }()

	if bar := <-notify; bar != 0 {
		t.Fatalf("first Next call bar = %d, want 0", bar)
	}

	stepDuration := time.Minute / time.Duration(120*4)
	barDuration := 4 * stepDuration
	wantDeadline := clk.Now().Add(barDuration)

	var gotDeadline time.Time
	select {
	case gotDeadline = <-waited:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Run did not wait after scheduling bar 0 (no call to the clock before requesting bar 1)")
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Errorf("wait deadline = %v, want %v", gotDeadline, wantDeadline)
	}

	select {
	case bar := <-notify:
		t.Fatalf("Next called for bar %d before the clock advanced past bar 0's duration", bar)
	case <-time.After(20 * time.Millisecond):
	}

	clk.Advance(barDuration)

	select {
	case bar := <-notify:
		if bar != 1 {
			t.Fatalf("Next call bar = %d, want 1", bar)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Run did not request bar 1 after the clock advanced past bar 0's duration")
	}

	select {
	case err := <-done:
		if !errors.Is(err, errPatternExhausted) {
			t.Fatalf("Run() error = %v, want errPatternExhausted", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Run did not return after the provider exhausted the pattern")
	}
}

// TestEngine_ContextCancelDuringBarWaitReturnsPromptly proves that
// cancelling ctx while Run is waiting out a bar's duration makes Run return
// promptly, instead of waiting out the rest of the bar. The fake clock is
// never advanced, so the only thing that can end Run here is cancellation.
func TestEngine_ContextCancelDuringBarWaitReturnsPromptly(t *testing.T) {
	notify := make(chan int)
	waited := make(chan time.Time)
	clk := newFakeClock(false)
	clk.waited = waited

	// stopAfter left at 0: the provider never stops the pattern on its
	// own, so the only thing that can end Run is ctx cancellation.
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: clk}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx, provider) }()

	<-notify // bar 0 requested.

	select {
	case <-waited: // Run is now blocked waiting out bar 0's duration.
	case <-time.After(40 * time.Millisecond):
		t.Fatal("Run did not reach the bar wait after scheduling bar 0")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(40 * time.Millisecond):
		t.Fatal("Run did not return promptly after ctx cancellation during the bar wait")
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.teardownCalls != 1 {
		t.Errorf("teardownCalls = %d, want 1", sink.teardownCalls)
	}
}

func TestEngine_HoldsAudioDeviceOpen(t *testing.T) {
	notify := make(chan int)
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify, stopAfter: 5}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	done := make(chan error, 1)
	go func() { done <- engine.Run(context.Background(), provider) }()

	// Next(bar) blocks on notify, so receiving 3 times here guarantees Run
	// is paused partway through the pattern (not finished) when we inspect
	// sink below.
	for i := 0; i < 3; i++ {
		<-notify
	}

	sink.mu.Lock()
	startCalls := sink.startCalls
	teardownCalls := sink.teardownCalls
	sink.mu.Unlock()

	if startCalls != 1 {
		t.Errorf("startCalls (mid-run) = %d, want 1", startCalls)
	}
	if teardownCalls != 0 {
		t.Errorf("teardownCalls (mid-run) = %d, want 0", teardownCalls)
	}

	// Drain the remaining bars so Run can reach stopAfter and return.
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case <-notify:
		case <-done:
			break drain
		case <-timeout:
			t.Fatal("Run did not return after provider stopped the pattern")
		}
	}

	sink.mu.Lock()
	finalStart, finalTeardown := sink.startCalls, sink.teardownCalls
	sink.mu.Unlock()

	if finalStart != 1 {
		t.Errorf("startCalls (after Run returned) = %d, want 1", finalStart)
	}
	if finalTeardown != 1 {
		t.Errorf("teardownCalls (after Run returned) = %d, want 1", finalTeardown)
	}
}

// TestEngine_RoutesNoteHitsToSetFireNote proves Run dispatches each hit by
// whether Note is set: sample hits still go through SetFire with sustain=0
// (no behavior change for existing patterns), note hits go through
// SetFireNote with sustain computed from Length x stepDuration, the same
// arithmetic TestEngine_SchedulesOnContinuousClock verifies for begin.
func TestEngine_RoutesNoteHitsToSetFireNote(t *testing.T) {
	provider := &scriptedProvider{
		bars: []barScript{
			{
				hits: []Hit{
					{Step: 0, Sample: "kick", Pan: 0, Velocity: 1},
					{Step: 2, Note: "C2", Length: 4, Pan: 0.2, Velocity: 0.9},
				},
				bpm:         120,
				stepsPerBar: 16,
			},
		},
	}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	err := engine.Run(context.Background(), provider)
	if !errors.Is(err, errPatternExhausted) {
		t.Fatalf("Run() error = %v, want errPatternExhausted", err)
	}

	stepDuration := time.Minute / time.Duration(120*4)
	want := []recordedFire{
		{source: "kick", begin: 0, sustain: 0, volume: 1, pan: 0},
		{note: "C2", begin: 2 * stepDuration, sustain: 4 * stepDuration, volume: 0.9, pan: 0.2},
	}

	sink.mu.Lock()
	got := sink.fires
	sink.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("fires = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fire[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestEngine_SigIntTeardown(t *testing.T) {
	notify := make(chan int)
	// stopAfter left at 0: the provider never stops the pattern on its
	// own, so the only thing that can end Run is ctx cancellation.
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx, provider) }()

	<-notify // Run has opened the device and requested at least one bar.

	cancel()

	// Run only notices cancellation at the top of its loop, between
	// Next calls; keep draining any bar already in flight so it can get
	// there and return.
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case <-notify:
		case <-done:
			break drain
		case <-timeout:
			t.Fatal("Run did not return after context cancellation")
		}
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.teardownCalls != 1 {
		t.Errorf("teardownCalls = %d, want 1", sink.teardownCalls)
	}
}
