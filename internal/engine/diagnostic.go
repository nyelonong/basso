package engine

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

// DiagnosticPhase identifies the stage at which a live revision failed.
type DiagnosticPhase string

const (
	DiagnosticPhaseCompile  DiagnosticPhase = "compile"
	DiagnosticPhaseEvaluate DiagnosticPhase = "evaluate"
	DiagnosticPhaseTimeout  DiagnosticPhase = "timeout"
	DiagnosticPhaseValidate DiagnosticPhase = "validate"
	DiagnosticPhaseWatch    DiagnosticPhase = "watch"
)

// Diagnostic describes a rejected source revision.
type Diagnostic struct {
	RevisionSHA256 string
	Bar            *int
	Phase          DiagnosticPhase
	Err            error
}

// DiagnosticReporter receives live playback diagnostics.
type DiagnosticReporter func(Diagnostic)

func evaluationDiagnostic(source string, bar int, err error) Diagnostic {
	phase := DiagnosticPhaseEvaluate
	underlying := err

	var evaluationError *EvaluationError
	if errors.As(err, &evaluationError) {
		phase = diagnosticPhase(evaluationError.Phase)
		underlying = evaluationError.Err
	}

	barCopy := bar
	return Diagnostic{
		RevisionSHA256: revisionSHA256(source),
		Bar:            &barCopy,
		Phase:          phase,
		Err:            underlying,
	}
}

func revisionSHA256(source string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
}

func diagnosticKey(diagnostic Diagnostic) string {
	return fmt.Sprintf(
		"%s\x00%s\x00%T\x00%v",
		diagnostic.RevisionSHA256,
		diagnostic.Phase,
		diagnostic.Err,
		diagnostic.Err,
	)
}

func watchDiagnostic(revision string, err error) Diagnostic {
	return Diagnostic{
		RevisionSHA256: revision,
		Phase:          DiagnosticPhaseWatch,
		Err:            err,
	}
}

func diagnosticPhase(phase EvaluationPhase) DiagnosticPhase {
	switch phase {
	case EvaluationPhaseCompile:
		return DiagnosticPhaseCompile
	case EvaluationPhaseTimeout:
		return DiagnosticPhaseTimeout
	case EvaluationPhaseValidate:
		return DiagnosticPhaseValidate
	default:
		return DiagnosticPhaseEvaluate
	}
}
