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
	mu    sync.Mutex
	fires []recordedFire
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
