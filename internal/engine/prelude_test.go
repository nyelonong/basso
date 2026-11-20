package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func evaluatePattern(t *testing.T, header, body string) (Bar, error) {
	t.Helper()
	source := header + "\n(fn pattern [bar]\n  " + body + ")\n\npattern\n"
	return NewEvaluator(testInventory(), evaluatorTestTimeout).Evaluate(context.Background(), source, 0)
}

func TestEvaluator_SeqTurnsStepStringIntoHits(t *testing.T) {
	bar, err := evaluatePattern(t, "", `(seq "kick2.wav" "x..o | -x" {:pan 0 :velocity 0.8})`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	want := []Hit{
		{Step: 0, Sample: "kick2.wav", Pan: 0, Velocity: 0.8, Length: 1},
		{Step: 3, Sample: "kick2.wav", Pan: 0, Velocity: 0.4, Length: 1, Delay: 0},
		{Step: 5, Sample: "kick2.wav", Pan: 0, Velocity: 0.8, Length: 1},
	}
	if len(bar.Hits) != len(want) {
		t.Fatalf("Hits = %+v, want %+v", bar.Hits, want)
	}
	for i := range want {
		if bar.Hits[i] != want[i] {
			t.Errorf("Hits[%d] = %+v, want %+v", i, bar.Hits[i], want[i])
		}
	}
}

func TestEvaluator_MelodyHoldsAndRests(t *testing.T) {
	bar, err := evaluatePattern(t, "", `(melody "pluck" "A1 _ _ . C2 | D#2 _")`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	want := []Hit{
		{Step: 0, Note: "A1", Instrument: "pluck", Length: 3, Velocity: 1},
		{Step: 4, Note: "C2", Instrument: "pluck", Length: 1, Velocity: 1},
		{Step: 5, Note: "D#2", Instrument: "pluck", Length: 2, Velocity: 1},
	}
	if len(bar.Hits) != len(want) {
		t.Fatalf("Hits = %+v, want %+v", bar.Hits, want)
	}
	for i := range want {
		if bar.Hits[i] != want[i] {
			t.Errorf("Hits[%d] = %+v, want %+v", i, bar.Hits[i], want[i])
		}
	}
}

func TestEvaluator_StackMergesListsAndSingleHits(t *testing.T) {
	bar, err := evaluatePattern(t, "", `(stack (seq "kick2.wav" "x.x") {:step 1 :sample "snare.wav" :pan 0} [])`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	var got []string
	for _, hit := range bar.Hits {
		got = append(got, hit.Sample)
	}
	if strings.Join(got, ",") != "kick2.wav,kick2.wav,snare.wav" {
		t.Errorf("samples = %v, want kick2.wav,kick2.wav,snare.wav", got)
	}
}

func TestEvaluator_HelperErrorsAreEvaluationErrors(t *testing.T) {
	cases := map[string]string{
		"unknown step":    `(seq "kick2.wav" "x?x")`,
		"hold first":      `(melody "bass" "_ A1")`,
		"stack argument":  `(stack 3)`,
		"seq step option": `(seq "kick2.wav" "x.x." {:step 3})`,
		"seq note option": `(seq "kick2.wav" "x" {:note "C2"})`,
		"melody length":   `(melody "bass" "C2 _" {:length 4})`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := evaluatePattern(t, "", body)
			var evalErr *EvaluationError
			if !errors.As(err, &evalErr) || evalErr.Phase != EvaluationPhaseEvaluate {
				t.Fatalf("Evaluate() error = %v, want evaluate-phase EvaluationError", err)
			}
		})
	}
}

func TestEvaluator_SwingDelaysOddSteps(t *testing.T) {
	bar, err := evaluatePattern(t, "(swing 75)", `(seq "kick2.wav" "xxxx" {:pan 0})`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if bar.Swing != 75 {
		t.Errorf("Swing = %v, want 75", bar.Swing)
	}
	for i, want := range []float64{0, 0.5, 0, 0.5} {
		if bar.Hits[i].Delay != want {
			t.Errorf("Hits[%d].Delay = %v, want %v", i, bar.Hits[i].Delay, want)
		}
	}
}

func TestEvaluator_DefaultSwingIsStraight(t *testing.T) {
	bar, err := evaluatePattern(t, "", `(seq "kick2.wav" "xx")`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if bar.Swing != 50 || bar.Hits[1].Delay != 0 {
		t.Errorf("Swing = %v, Delay = %v, want 50 and 0", bar.Swing, bar.Hits[1].Delay)
	}
}

func TestEvaluator_RejectsSwingOutsideRange(t *testing.T) {
	for _, swing := range []string{"0", "49", "76", "(/ 0 0)"} {
		t.Run(swing, func(t *testing.T) {
			_, err := evaluatePattern(t, "(swing "+swing+")", `[]`)
			var evalErr *EvaluationError
			if !errors.As(err, &evalErr) || evalErr.Phase != EvaluationPhaseValidate {
				t.Fatalf("Evaluate() error = %v, want validate-phase EvaluationError", err)
			}
		})
	}
}

func TestEngine_PlaysDelayedHitsLate(t *testing.T) {
	provider := &scriptedProvider{bars: []barScript{{
		hits:        []Hit{{Step: 1, Sample: "hat", Velocity: 1, Delay: 0.5}},
		bpm:         120,
		stepsPerBar: 4,
	}}}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	if err := engine.Run(context.Background(), provider); !errors.Is(err, errPatternExhausted) {
		t.Fatalf("Run() error = %v, want errPatternExhausted", err)
	}

	stepDuration := time.Minute / time.Duration(120*4)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.fires) != 1 || sink.fires[0].begin != stepDuration+stepDuration/2 {
		t.Fatalf("fires = %+v, want one hat at 1.5 steps", sink.fires)
	}
}

func TestEvaluator_ShuffleGrooveExamplePlays(t *testing.T) {
	source, err := os.ReadFile("../../patterns/shuffle-groove.fnl")
	if err != nil {
		t.Fatal(err)
	}
	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	if err := evaluator.Preflight(context.Background(), string(source), 0, 7); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestEvaluator_StackSkipsNilAndKeepsBareHits(t *testing.T) {
	bar, err := evaluatePattern(t, "", `(stack (seq "kick2.wav" "x") (when false (seq "snare.wav" "x")))`)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(bar.Hits) != 1 {
		t.Errorf("Hits = %+v, want only the kick", bar.Hits)
	}

	_, err = evaluatePattern(t, "", `(stack {:sample "kick2.wav"})`)
	if err == nil || !strings.Contains(err.Error(), ":step is required") {
		t.Errorf("Evaluate() error = %v, want :step is required", err)
	}
}

func TestEvaluator_SeqIgnoresAllWhitespace(t *testing.T) {
	bar, err := evaluatePattern(t, "", "(seq \"kick2.wav\" \"x.\\r\\n.x\")")
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(bar.Hits) != 2 || bar.Hits[1].Step != 3 {
		t.Errorf("Hits = %+v, want steps 0 and 3", bar.Hits)
	}
}

func TestEvaluator_SwingAcceptsNumericString(t *testing.T) {
	bar, err := evaluatePattern(t, `(swing "62")`, `[]`)
	if err != nil || bar.Swing != 62 {
		t.Fatalf("Evaluate() = %v, %v, want swing 62", bar.Swing, err)
	}
}

func TestEngine_SwungNoteEndsOnTheGrid(t *testing.T) {
	provider := &scriptedProvider{bars: []barScript{{
		hits:        []Hit{{Step: 3, Note: "C3", Instrument: "pad", Length: 1, Velocity: 1, Delay: 0.5}},
		bpm:         120,
		stepsPerBar: 4,
	}}}
	sink := &fakeSink{}
	engine := &Engine{sink: sink, clock: newFakeClock(true)}

	if err := engine.Run(context.Background(), provider); !errors.Is(err, errPatternExhausted) {
		t.Fatalf("Run() error = %v, want errPatternExhausted", err)
	}

	stepDuration := time.Minute / time.Duration(120*4)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	fire := sink.fires[0]
	if fire.begin+fire.sustain != 4*stepDuration {
		t.Fatalf("note ends at %v, want bar end %v", fire.begin+fire.sustain, 4*stepDuration)
	}
}
