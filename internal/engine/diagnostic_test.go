package engine

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

func TestEvaluationDiagnostic_MapsPhasesAndUnderlyingError(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name string
		from EvaluationPhase
		want DiagnosticPhase
	}{
		{name: "compile", from: EvaluationPhaseCompile, want: DiagnosticPhaseCompile},
		{name: "evaluate", from: EvaluationPhaseEvaluate, want: DiagnosticPhaseEvaluate},
		{name: "timeout", from: EvaluationPhaseTimeout, want: DiagnosticPhaseTimeout},
		{name: "validate", from: EvaluationPhaseValidate, want: DiagnosticPhaseValidate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := evaluationDiagnostic(
				"source",
				7,
				&EvaluationError{Phase: test.from, Bar: 7, Err: cause},
			)
			if diagnostic.Phase != test.want {
				t.Errorf("phase = %q, want %q", diagnostic.Phase, test.want)
			}
			if !errors.Is(diagnostic.Err, cause) {
				t.Errorf("error = %v, want underlying %v", diagnostic.Err, cause)
			}
			if diagnostic.Bar == nil || *diagnostic.Bar != 7 {
				t.Errorf("bar = %v, want 7", diagnostic.Bar)
			}
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(diagnostic.RevisionSHA256) {
				t.Errorf("revision = %q, want full lowercase SHA-256", diagnostic.RevisionSHA256)
			}
		})
	}
}

func TestFennelProvider_DeduplicatesDiagnostic(t *testing.T) {
	source := `
(fn pattern [bar]
  (if (= bar 0)
      [{:step 0 :sample "a.wav" :velocity 1.0}]
      (error "same failure")))

pattern
`
	var diagnostics []Diagnostic
	provider, err := New(
		source,
		NewEvaluator(SoundInventory{"a.wav": {}}, 250*time.Millisecond),
		func(diagnostic Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	for bar := 1; bar <= 2; bar++ {
		if _, _, _, err := provider.Next(bar); err != nil {
			t.Fatalf("Next(%d) error = %v, want nil fallback", bar, err)
		}
	}

	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1 independent of bar", len(diagnostics))
	}

	provider.setPendingSource(fennelSourceSample("missing.wav"))
	if _, _, _, err := provider.Next(3); err != nil {
		t.Fatalf("Next(3) error = %v, want nil fallback", err)
	}
	if len(diagnostics) != 3 {
		t.Fatalf(
			"len(diagnostics) after different revision = %d, want 3",
			len(diagnostics),
		)
	}
	if _, _, _, err := provider.Next(4); err != nil {
		t.Fatalf("Next(4) error = %v, want nil fallback", err)
	}
	if len(diagnostics) != 3 {
		t.Fatalf("len(diagnostics) after repeated active error = %d, want 3", len(diagnostics))
	}
}
