package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const evaluatorTestTimeout = 250 * time.Millisecond

func TestEvaluator_EvaluateCurrentPattern(t *testing.T) {
	source := `
(bpm 120)
(steps 16)

(fn pattern [bar]
  [{:step 0 :sample "kick2.wav"}
   {:step 4 :sample "snare.wav"}
   {:step 8 :note "C2" :length 4}])

pattern
`

	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	bar, err := evaluator.Evaluate(context.Background(), source, 0)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	if bar.BPM != 120 {
		t.Errorf("BPM = %d, want 120", bar.BPM)
	}
	if bar.StepsPerBar != 16 {
		t.Errorf("StepsPerBar = %d, want 16", bar.StepsPerBar)
	}
	if len(bar.Hits) != 3 {
		t.Fatalf("len(Hits) = %d, want 3", len(bar.Hits))
	}
	if bar.Hits[0].Sample != "kick2.wav" {
		t.Errorf("Hits[0].Sample = %q, want kick2.wav", bar.Hits[0].Sample)
	}
	if bar.Hits[1].Sample != "snare.wav" {
		t.Errorf("Hits[1].Sample = %q, want snare.wav", bar.Hits[1].Sample)
	}
	if bar.Hits[2].Note != "C2" || bar.Hits[2].Instrument != "bass" || bar.Hits[2].Length != 4 {
		t.Errorf("Hits[2] = %+v, want default bass note C2 of length 4", bar.Hits[2])
	}
}

func TestEvaluator_PreflightBarsZeroThroughFifteen(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step bar :sample "kick2.wav"}])

pattern
`

	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	if err := evaluator.Preflight(context.Background(), source, 0, 15); err != nil {
		t.Fatalf("Preflight(0, 15) error = %v, want nil", err)
	}

	err := evaluator.Preflight(context.Background(), source, 0, 16)
	assertEvaluationPhase(t, err, EvaluationPhaseValidate, 16)

	for _, horizon := range []struct {
		name     string
		firstBar int
		lastBar  int
	}{
		{name: "negative first bar", firstBar: -1, lastBar: 15},
		{name: "inverted horizon", firstBar: 15, lastBar: 14},
	} {
		t.Run(horizon.name, func(t *testing.T) {
			if err := evaluator.Preflight(
				context.Background(),
				source,
				horizon.firstBar,
				horizon.lastBar,
			); err == nil {
				t.Fatal("Preflight() error = nil, want error")
			}
		})
	}
}

func TestEvaluator_UsesFreshStatePerBar(t *testing.T) {
	source := `
(fn pattern [bar]
  (set _G.evaluation-count (+ (or _G.evaluation-count 0) 1))
  [{:step (- _G.evaluation-count 1) :sample "kick2.wav"}])

pattern
`

	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	for _, barNumber := range []int{0, 1} {
		bar, err := evaluator.Evaluate(context.Background(), source, barNumber)
		if err != nil {
			t.Fatalf("Evaluate(bar %d) error = %v, want nil", barNumber, err)
		}
		if bar.Hits[0].Step != 0 {
			t.Fatalf("Evaluate(bar %d) step = %d, want 0 from a fresh state", barNumber, bar.Hits[0].Step)
		}
	}
}

func TestEvaluator_RejectsOversizedSource(t *testing.T) {
	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	source := strings.Repeat(" ", 256*1024+1)

	_, err := evaluator.Evaluate(context.Background(), source, 7)
	assertEvaluationPhase(t, err, EvaluationPhaseCompile, 7)
}

func TestEvaluator_TimesOutInfiniteLoop(t *testing.T) {
	evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
	source := `
(fn pattern [bar]
  (while true nil)
  [])

pattern
`

	result := make(chan error, 1)
	go func() {
		_, err := evaluator.Evaluate(context.Background(), source, 3)
		result <- err
	}()

	select {
	case err := <-result:
		assertEvaluationPhase(t, err, EvaluationPhaseTimeout, 3)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("errors.Is(error, context.DeadlineExceeded) = false; error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Evaluate() did not return within the test bound")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := evaluator.Evaluate(ctx, `(fn pattern [bar] []) pattern`, 4)
	assertEvaluationPhase(t, err, EvaluationPhaseTimeout, 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(error, context.Canceled) = false; error = %v", err)
	}
}

func TestEvaluator_RemovesFilesystemAndProcessGlobals(t *testing.T) {
	for _, global := range []string{
		"os",
		"io",
		"debug",
		"package",
		"require",
		"dofile",
		"loadfile",
	} {
		t.Run(global, func(t *testing.T) {
			source := fmt.Sprintf(`
(assert (= nil %s))
(fn pattern [bar] [])
pattern
`, global)

			evaluator := NewEvaluator(testInventory(), evaluatorTestTimeout)
			if _, err := evaluator.Evaluate(context.Background(), source, 0); err != nil {
				t.Fatalf("Evaluate() error = %v, want removed global %q to be nil", err, global)
			}
		})
	}
}

func TestEvaluator_ClassifiesCompileEvaluateValidateAndTimeoutErrors(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		timeout   time.Duration
		wantPhase EvaluationPhase
	}{
		{
			name:      "compile",
			source:    `(`,
			timeout:   evaluatorTestTimeout,
			wantPhase: EvaluationPhaseCompile,
		},
		{
			name: "evaluate",
			source: `
(fn pattern [bar]
  (error "pattern failed"))
pattern
`,
			timeout:   evaluatorTestTimeout,
			wantPhase: EvaluationPhaseEvaluate,
		},
		{
			name: "validate",
			source: `
(fn pattern [bar]
  [{:step 0 :sample "outside-inventory.wav"}])
pattern
`,
			timeout:   evaluatorTestTimeout,
			wantPhase: EvaluationPhaseValidate,
		},
		{
			name: "timeout",
			source: `
(fn pattern [bar]
  (while true nil)
  [])
pattern
`,
			timeout:   evaluatorTestTimeout,
			wantPhase: EvaluationPhaseTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := NewEvaluator(testInventory(), tt.timeout)
			_, err := evaluator.Evaluate(context.Background(), tt.source, 9)
			evaluationErr := assertEvaluationPhase(t, err, tt.wantPhase, 9)

			if tt.wantPhase == EvaluationPhaseTimeout {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("errors.Is(error, context.DeadlineExceeded) = false; error = %v", err)
				}
				return
			}

			if evaluationErr.Err == nil {
				t.Fatal("EvaluationError.Err = nil, want underlying error")
			}
			if tt.wantPhase == EvaluationPhaseValidate {
				return
			}

			var apiErr *lua.ApiError
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As(error, *lua.ApiError) = false; error = %v", err)
			}
		})
	}
}

func assertEvaluationPhase(
	t *testing.T,
	err error,
	wantPhase EvaluationPhase,
	wantBar int,
) *EvaluationError {
	t.Helper()

	if err == nil {
		t.Fatal("error = nil, want EvaluationError")
	}

	var evaluationErr *EvaluationError
	if !errors.As(err, &evaluationErr) {
		t.Fatalf("errors.As(error, *EvaluationError) = false; error = %v", err)
	}
	if evaluationErr.Phase != wantPhase {
		t.Errorf("EvaluationError.Phase = %q, want %q", evaluationErr.Phase, wantPhase)
	}
	if evaluationErr.Bar != wantBar {
		t.Errorf("EvaluationError.Bar = %d, want %d", evaluationErr.Bar, wantBar)
	}

	return evaluationErr
}
