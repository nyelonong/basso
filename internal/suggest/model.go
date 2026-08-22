package suggest

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Instrument is one built-in voice exposed to a suggestion model.
type Instrument struct {
	Name             string
	Description      string
	RecommendedRange string
	Limits           string
}

// ModelRequest is the complete, provider-neutral context a model may receive
// for one suggestion.
type ModelRequest struct {
	Prompt      string
	Source      string
	Samples     []string
	Instruments []Instrument
}

func validateInstruments(instruments []Instrument) error {
	if len(instruments) == 0 {
		return errors.New("suggest instrument inventory is empty")
	}
	seen := make(map[string]struct{}, len(instruments))
	for _, instrument := range instruments {
		if strings.TrimSpace(instrument.Name) == "" {
			return errors.New("suggest instrument name is empty")
		}
		if strings.TrimSpace(instrument.Description) == "" {
			return fmt.Errorf("suggest instrument %q description is empty", instrument.Name)
		}
		if strings.TrimSpace(instrument.RecommendedRange) == "" {
			return fmt.Errorf("suggest instrument %q recommended range is empty", instrument.Name)
		}
		if _, ok := seen[instrument.Name]; ok {
			return fmt.Errorf("suggest instrument %q is duplicated", instrument.Name)
		}
		seen[instrument.Name] = struct{}{}
	}
	return nil
}

// Proposal is a model's untrusted complete-source suggestion.
type Proposal struct {
	Summary string
	Source  string
}

// Model proposes a complete pattern revision from an explicit request.
type Model interface {
	Propose(context.Context, ModelRequest) (Proposal, error)
}

// Preflighter verifies a source over an inclusive range of bars before it can
// become a candidate or be applied.
type Preflighter interface {
	Preflight(context.Context, string, int, int) error
}
