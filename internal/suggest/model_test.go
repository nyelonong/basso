package suggest

import "context"

type modelContractFake struct{}

func (modelContractFake) Propose(context.Context, ModelRequest) (Proposal, error) {
	return Proposal{}, nil
}

type preflighterContractFake struct{}

func (preflighterContractFake) Preflight(context.Context, string, int, int) error {
	return nil
}

var _ Model = modelContractFake{}
var _ Preflighter = preflighterContractFake{}
