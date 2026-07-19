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
	engine := NewEngine(sink)

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

func TestEngine_HoldsAudioDeviceOpen(t *testing.T) {
	notify := make(chan int)
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify, stopAfter: 5}
	sink := &fakeSink{}
	engine := NewEngine(sink)

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

func TestEngine_SigIntTeardown(t *testing.T) {
	notify := make(chan int)
	// stopAfter left at 0: the provider never stops the pattern on its
	// own, so the only thing that can end Run is ctx cancellation.
	provider := &loopingProvider{bpm: 120, stepsPerBar: 4, notify: notify}
	sink := &fakeSink{}
	engine := NewEngine(sink)

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
