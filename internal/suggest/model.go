package suggest

import "context"

// ModelRequest is the complete, provider-neutral context a model may receive
// for one suggestion.
type ModelRequest struct {
	Prompt      string
	Source      string
	Samples     []string
	Instruments []string
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
