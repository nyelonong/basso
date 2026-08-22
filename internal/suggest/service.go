package suggest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxPromptBytes       = 16 * 1024
	maxSourceBytes       = 256 * 1024
	maxSummaryBytes      = 500
	maxRepairPromptBytes = 512 * 1024
)

// SuggestInput is the bounded local context for one suggestion request.
type SuggestInput struct {
	Provider    string
	Model       string
	Prompt      string
	SourcePath  string
	SoundsPath  string
	Source      []byte
	Samples     []string
	Instruments []string
}

// Service generates locally preflighted, unsaved candidates.
type Service struct {
	model       Model
	preflighter Preflighter
}

// NewService constructs a suggestion service from its consumer-side seams.
func NewService(model Model, preflighter Preflighter) *Service {
	return &Service{model: model, preflighter: preflighter}
}

// maxRepairRounds bounds diagnostic-guided repair attempts after the initial
// proposal; flaky free-model gateways often need more than one.
const maxRepairRounds = 2

// Suggest requests a proposal and locally preflights it before returning it.
func (s *Service) Suggest(ctx context.Context, input SuggestInput) (Candidate, error) {
	if err := validateSuggestInput(input); err != nil {
		return Candidate{}, err
	}
	if s == nil || s.model == nil || s.preflighter == nil {
		return Candidate{}, errors.New("suggest service dependencies are nil")
	}

	proposal, err := s.model.Propose(ctx, modelRequest(input, input.Prompt, string(input.Source)))
	if err != nil {
		return Candidate{}, fmt.Errorf("request initial proposal: %w", err)
	}
	if err := validateProposal(proposal); err != nil {
		return Candidate{}, fmt.Errorf("validate initial proposal: %w", err)
	}
	preflightErr := s.preflighter.Preflight(ctx, proposal.Source, 0, 15)
	if preflightErr == nil {
		return candidateFromProposal(input, proposal, 1), nil
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, fmt.Errorf("suggestion context cancelled after local preflight: %w", err)
	}

	firstPreflightErr := preflightErr
	source := proposal.Source
	for attempt := 2; attempt <= 1+maxRepairRounds; attempt++ {
		if err := ctx.Err(); err != nil {
			break
		}
		repairPrompt, err := buildRepairPrompt(input.Prompt, source, preflightErr)
		if err != nil {
			return Candidate{}, err
		}
		repaired, err := s.model.Propose(ctx, modelRequest(input, repairPrompt, source))
		if err != nil {
			return Candidate{}, errors.Join(
				fmt.Errorf("first local preflight: %w", firstPreflightErr),
				fmt.Errorf("request repaired proposal: %w", err),
			)
		}
		if err := validateProposal(repaired); err != nil {
			return Candidate{}, errors.Join(
				fmt.Errorf("first local preflight: %w", firstPreflightErr),
				fmt.Errorf("validate repaired proposal: %w", err),
			)
		}
		preflightErr = s.preflighter.Preflight(ctx, repaired.Source, 0, 15)
		if preflightErr == nil {
			return candidateFromProposal(input, repaired, attempt), nil
		}
		source = repaired.Source
	}
	return Candidate{}, errors.Join(
		fmt.Errorf("first local preflight: %w", firstPreflightErr),
		fmt.Errorf("repaired local preflight: %w", preflightErr),
	)
}

func validateSuggestInput(input SuggestInput) error {
	if strings.TrimSpace(input.Provider) == "" {
		return errors.New("suggest provider is empty")
	}
	if strings.TrimSpace(input.Model) == "" {
		return errors.New("suggest model is empty")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return errors.New("suggest prompt is empty")
	}
	if len(input.Prompt) > maxPromptBytes {
		return fmt.Errorf("suggest prompt exceeds %d bytes", maxPromptBytes)
	}
	if len(input.Source) == 0 {
		return errors.New("suggest source is empty")
	}
	if len(input.Source) > maxSourceBytes {
		return fmt.Errorf("suggest source exceeds %d bytes", maxSourceBytes)
	}
	if !filepath.IsAbs(input.SourcePath) || !filepath.IsAbs(input.SoundsPath) {
		return errors.New("suggest source and sounds paths must be absolute")
	}
	if len(input.Samples) == 0 {
		return errors.New("suggest sample inventory is empty")
	}
	if len(input.Instruments) == 0 {
		return errors.New("suggest instrument inventory is empty")
	}
	return nil
}

func validateProposal(proposal Proposal) error {
	if strings.TrimSpace(proposal.Summary) == "" {
		return errors.New("proposal summary is empty")
	}
	if !utf8.ValidString(proposal.Summary) {
		return errors.New("proposal summary is not valid UTF-8")
	}
	if len(proposal.Summary) > maxSummaryBytes {
		return fmt.Errorf("proposal summary exceeds %d bytes", maxSummaryBytes)
	}
	if len(proposal.Source) == 0 {
		return errors.New("proposal source is empty")
	}
	if len(proposal.Source) > maxSourceBytes {
		return fmt.Errorf("proposal source exceeds %d bytes", maxSourceBytes)
	}
	return nil
}

func modelRequest(input SuggestInput, prompt, source string) ModelRequest {
	return ModelRequest{
		Prompt:      prompt,
		Source:      source,
		Samples:     append([]string(nil), input.Samples...),
		Instruments: append([]string(nil), input.Instruments...),
	}
}

func buildRepairPrompt(prompt, rejectedSource string, diagnostic error) (string, error) {
	diagnosticText := diagnostic.Error()
	if len(prompt)+len(rejectedSource)+len(diagnosticText) > maxRepairPromptBytes {
		return "", errors.New("repair prompt exceeds bounded size")
	}
	repairPrompt := fmt.Sprintf(
		"The previous proposal failed local validation.\n\nOriginal user request:\n%s\n\nRejected source:\n%s\n\nExact local diagnostic:\n%s",
		prompt,
		rejectedSource,
		diagnosticText,
	)
	if len(repairPrompt) > maxRepairPromptBytes {
		return "", errors.New("repair prompt exceeds bounded size")
	}
	return repairPrompt, nil
}

func candidateFromProposal(input SuggestInput, proposal Proposal, attempts int) Candidate {
	source := []byte(proposal.Source)
	return Candidate{
		Metadata: Metadata{
			SourcePath:      input.SourcePath,
			SoundsPath:      input.SoundsPath,
			BaseSHA256:      sha256Hex(input.Source),
			CandidateSHA256: sha256Hex(source),
			Provider:        input.Provider,
			Model:           input.Model,
			Prompt:          input.Prompt,
			Summary:         proposal.Summary,
			Attempts:        attempts,
			Validation: ValidationRecord{
				FirstBar:        0,
				LastBar:         15,
				TimeoutMSPerBar: 250,
				Status:          "passed",
			},
		},
		Source: source,
	}
}
