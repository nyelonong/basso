package suggest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var errUnexpectedModelCall = errors.New("unexpected model call")

func TestService_ValidFirstProposalUsesOneAttempt(t *testing.T) {
	input := validSuggestInput()
	model := &scriptedModel{
		responses: []scriptedResponse{{
			proposal: Proposal{
				Summary: "made the hats denser",
				Source:  "(bpm 120)\n(steps 16)\n",
			},
		}},
	}
	preflighter := &scriptedPreflighter{}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), input)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}

	if len(model.requests) != 1 {
		t.Fatalf("model calls = %d, want 1", len(model.requests))
	}
	wantRequest := ModelRequest{
		Prompt:      input.Prompt,
		Source:      string(input.Source),
		Samples:     input.Samples,
		Instruments: input.Instruments,
	}
	if !reflect.DeepEqual(model.requests[0], wantRequest) {
		t.Errorf("first request = %#v, want %#v", model.requests[0], wantRequest)
	}
	if candidate.Metadata.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", candidate.Metadata.Attempts)
	}
	if len(preflighter.calls) != 1 {
		t.Fatalf("preflight calls = %d, want 1", len(preflighter.calls))
	}
	if call := preflighter.calls[0]; call.firstBar != 0 || call.lastBar != 15 {
		t.Errorf("preflight range = %d..%d, want 0..15", call.firstBar, call.lastBar)
	}
}

func TestService_InvalidFirstProposalRepairsOnce(t *testing.T) {
	firstDiagnostic := errors.New("bar 7: unknown sample \"clap.wav\"")
	input := validSuggestInput()
	model := &scriptedModel{responses: []scriptedResponse{
		{proposal: Proposal{Summary: "uses an unavailable clap", Source: "bad source"}},
		{proposal: Proposal{Summary: "uses the available hat", Source: "repaired source"}},
	}}
	preflighter := &scriptedPreflighter{errs: []error{firstDiagnostic, nil}}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), input)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if candidate.Metadata.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", candidate.Metadata.Attempts)
	}
	if string(candidate.Source) != "repaired source" {
		t.Errorf("candidate source = %q, want repaired source", candidate.Source)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(model.requests))
	}
	repair := model.requests[1]
	if repair.Source != "bad source" {
		t.Errorf("repair source = %q, want rejected source", repair.Source)
	}
	if !strings.Contains(repair.Prompt, "bad source") {
		t.Errorf("repair prompt does not include rejected source: %q", repair.Prompt)
	}
	if !strings.Contains(repair.Prompt, firstDiagnostic.Error()) {
		t.Errorf("repair prompt does not include exact diagnostic: %q", repair.Prompt)
	}
	if !reflect.DeepEqual(repair.Samples, input.Samples) || !reflect.DeepEqual(repair.Instruments, input.Instruments) {
		t.Errorf("repair inventory = %#v/%#v, want %#v/%#v", repair.Samples, repair.Instruments, input.Samples, input.Instruments)
	}
	if len(preflighter.calls) != 2 {
		t.Fatalf("preflight calls = %d, want 2", len(preflighter.calls))
	}
	for _, call := range preflighter.calls {
		if call.firstBar != 0 || call.lastBar != 15 {
			t.Errorf("preflight range = %d..%d, want 0..15", call.firstBar, call.lastBar)
		}
	}
}

func TestService_InvalidRepairCreatesNoCandidate(t *testing.T) {
	firstDiagnostic := errors.New("bar 3: invalid note")
	lastDiagnostic := errors.New("bar 9: invalid velocity")
	model := &scriptedModel{responses: []scriptedResponse{
		{proposal: Proposal{Summary: "bad note", Source: "first bad source"}},
		{proposal: Proposal{Summary: "bad velocity", Source: "second bad source"}},
		{proposal: Proposal{Summary: "still bad", Source: "third bad source"}},
	}}
	preflighter := &scriptedPreflighter{errs: []error{firstDiagnostic, errors.New("middle"), lastDiagnostic}}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput())
	if err == nil {
		t.Fatal("Suggest() error = nil, want repaired-proposal failure")
	}
	if !strings.Contains(err.Error(), firstDiagnostic.Error()) || !strings.Contains(err.Error(), lastDiagnostic.Error()) {
		t.Errorf("Suggest() error = %q, want first and last diagnostics", err)
	}
	if !reflect.DeepEqual(candidate, Candidate{}) {
		t.Errorf("candidate = %#v, want zero candidate", candidate)
	}
	if len(model.requests) != 1+maxRepairRounds || len(preflighter.calls) != 1+maxRepairRounds {
		t.Errorf("model/preflight calls = %d/%d, want %d", len(model.requests), len(preflighter.calls), 1+maxRepairRounds)
	}
}

func TestService_SecondRepairConverges(t *testing.T) {
	model := &scriptedModel{responses: []scriptedResponse{
		{proposal: Proposal{Summary: "bad note", Source: "first bad source"}},
		{proposal: Proposal{Summary: "bad length", Source: "second bad source"}},
		{proposal: Proposal{Summary: "fixed", Source: "repaired source"}},
	}}
	preflighter := &scriptedPreflighter{errs: []error{
		errors.New("bar 3: invalid note"),
		errors.New("bar 9: length 0"),
		nil,
	}}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput())
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if candidate.Metadata.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", candidate.Metadata.Attempts)
	}
	if string(candidate.Source) != "repaired source" {
		t.Errorf("source = %q, want third proposal", candidate.Source)
	}
}

func TestService_RepairModelErrorIncludesFirstDiagnostic(t *testing.T) {
	firstDiagnostic := errors.New("bar 3: invalid note")
	repairFailure := errors.New("provider unavailable during repair")
	model := &scriptedModel{responses: []scriptedResponse{
		{proposal: Proposal{Summary: "bad note", Source: "first bad source"}},
		{err: repairFailure},
	}}
	preflighter := &scriptedPreflighter{errs: []error{firstDiagnostic}}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput())
	if err == nil {
		t.Fatal("Suggest() error = nil, want repair model error")
	}
	if !strings.Contains(err.Error(), firstDiagnostic.Error()) || !strings.Contains(err.Error(), repairFailure.Error()) {
		t.Errorf("Suggest() error = %q, want first diagnostic and repair model error", err)
	}
	if !reflect.DeepEqual(candidate, Candidate{}) {
		t.Errorf("candidate = %#v, want zero candidate", candidate)
	}
	if len(model.requests) != 2 {
		t.Errorf("model calls = %d, want 2", len(model.requests))
	}
}

func TestService_InvalidRepairOutputIncludesFirstDiagnostic(t *testing.T) {
	firstDiagnostic := errors.New("bar 3: invalid note")
	const repairFailure = "proposal summary is empty"
	model := &scriptedModel{responses: []scriptedResponse{
		{proposal: Proposal{Summary: "bad note", Source: "first bad source"}},
		{proposal: Proposal{Source: "second bad source"}},
	}}
	preflighter := &scriptedPreflighter{errs: []error{firstDiagnostic}}

	candidate, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput())
	if err == nil {
		t.Fatal("Suggest() error = nil, want repaired proposal validation error")
	}
	if !strings.Contains(err.Error(), firstDiagnostic.Error()) || !strings.Contains(err.Error(), repairFailure) {
		t.Errorf("Suggest() error = %q, want first diagnostic and repaired proposal validation error", err)
	}
	if !reflect.DeepEqual(candidate, Candidate{}) {
		t.Errorf("candidate = %#v, want zero candidate", candidate)
	}
	if len(model.requests) != 2 {
		t.Errorf("model calls = %d, want 2", len(model.requests))
	}
}

func TestService_PreflightsExactlyBarsZeroThroughFifteen(t *testing.T) {
	model := &scriptedModel{responses: []scriptedResponse{{proposal: Proposal{Summary: "valid", Source: "candidate"}}}}
	preflighter := &scriptedPreflighter{}

	if _, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput()); err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(preflighter.calls) != 1 {
		t.Fatalf("preflight calls = %d, want 1", len(preflighter.calls))
	}
	call := preflighter.calls[0]
	if call.firstBar != 0 || call.lastBar != 15 {
		t.Errorf("Preflight() range = %d..%d, want 0..15", call.firstBar, call.lastBar)
	}
}

func TestService_RejectsInputBoundsBeforeModel(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SuggestInput)
	}{
		{name: "empty provider", mutate: func(input *SuggestInput) { input.Provider = "" }},
		{name: "empty model", mutate: func(input *SuggestInput) { input.Model = "" }},
		{name: "empty prompt", mutate: func(input *SuggestInput) { input.Prompt = "" }},
		{name: "relative source path", mutate: func(input *SuggestInput) { input.SourcePath = "pattern.fnl" }},
		{name: "relative sounds path", mutate: func(input *SuggestInput) { input.SoundsPath = "sound/808" }},
		{name: "empty source", mutate: func(input *SuggestInput) { input.Source = nil }},
		{name: "oversized prompt", mutate: func(input *SuggestInput) { input.Prompt = strings.Repeat("p", 16*1024+1) }},
		{name: "oversized source", mutate: func(input *SuggestInput) { input.Source = []byte(strings.Repeat("s", 256*1024+1)) }},
		{name: "empty samples", mutate: func(input *SuggestInput) { input.Samples = []string{} }},
		{name: "empty instruments", mutate: func(input *SuggestInput) { input.Instruments = []Instrument{} }},
		{name: "empty instrument name", mutate: func(input *SuggestInput) { input.Instruments[0].Name = "" }},
		{name: "empty instrument description", mutate: func(input *SuggestInput) { input.Instruments[0].Description = "" }},
		{name: "empty instrument range", mutate: func(input *SuggestInput) { input.Instruments[0].RecommendedRange = "" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validSuggestInput()
			test.mutate(&input)
			model := &scriptedModel{responses: []scriptedResponse{{proposal: Proposal{Summary: "valid", Source: "candidate"}}}}
			preflighter := &scriptedPreflighter{}

			candidate, err := NewService(model, preflighter).Suggest(context.Background(), input)
			if err == nil {
				t.Fatal("Suggest() error = nil, want input validation error")
			}
			if !reflect.DeepEqual(candidate, Candidate{}) {
				t.Errorf("candidate = %#v, want zero candidate", candidate)
			}
			if len(model.requests) != 0 || len(preflighter.calls) != 0 {
				t.Errorf("model/preflight calls = %d/%d, want 0/0", len(model.requests), len(preflighter.calls))
			}
		})
	}
}

func TestService_MetadataContainsHashesAndProvenance(t *testing.T) {
	input := validSuggestInput()
	proposal := Proposal{Summary: "adds a brass response", Source: "candidate source"}
	model := &scriptedModel{responses: []scriptedResponse{{proposal: proposal}}}

	candidate, err := NewService(model, &scriptedPreflighter{}).Suggest(context.Background(), input)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	wantBaseHash := testSHA256(input.Source)
	wantCandidateHash := testSHA256([]byte(proposal.Source))
	metadata := candidate.Metadata
	if metadata.SchemaVersion != 0 || metadata.ID != "" || !metadata.CreatedAt.IsZero() {
		t.Errorf("store-owned metadata = %#v, want zero values", metadata)
	}
	if metadata.SourcePath != input.SourcePath || metadata.SoundsPath != input.SoundsPath ||
		metadata.BaseSHA256 != wantBaseHash || metadata.CandidateSHA256 != wantCandidateHash ||
		metadata.Provider != input.Provider || metadata.Model != input.Model || metadata.Prompt != input.Prompt ||
		metadata.Summary != proposal.Summary || metadata.Attempts != 1 {
		t.Errorf("metadata = %#v, want complete source provenance", metadata)
	}
	wantValidation := ValidationRecord{FirstBar: 0, LastBar: 15, TimeoutMSPerBar: 250, Status: "passed"}
	if !reflect.DeepEqual(metadata.Validation, wantValidation) {
		t.Errorf("Validation = %#v, want %#v", metadata.Validation, wantValidation)
	}
	if string(candidate.Source) != proposal.Source {
		t.Errorf("candidate Source = %q, want %q", candidate.Source, proposal.Source)
	}
}

func TestService_DoesNotMutateSource(t *testing.T) {
	input := validSuggestInput()
	wantSource := append([]byte(nil), input.Source...)
	wantSamples := append([]string(nil), input.Samples...)
	wantInstruments := append([]Instrument(nil), input.Instruments...)
	model := &scriptedModel{
		responses: []scriptedResponse{{proposal: Proposal{Summary: "valid", Source: "candidate"}}},
		mutate: func(request *ModelRequest) {
			request.Samples[0] = "mutated.wav"
			request.Instruments[0].Name = "mutated"
		},
	}

	if _, err := NewService(model, &scriptedPreflighter{}).Suggest(context.Background(), input); err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if !reflect.DeepEqual(input.Source, wantSource) || !reflect.DeepEqual(input.Samples, wantSamples) || !reflect.DeepEqual(input.Instruments, wantInstruments) {
		t.Errorf("input mutated to %#v, want source=%#v samples=%#v instruments=%#v", input, wantSource, wantSamples, wantInstruments)
	}
}

func TestService_ProviderOrOutputErrorsDoNotRetry(t *testing.T) {
	tests := []struct {
		name     string
		response scriptedResponse
	}{
		{name: "provider error", response: scriptedResponse{err: errors.New("provider unavailable")}},
		{name: "empty summary", response: scriptedResponse{proposal: Proposal{Source: "candidate"}}},
		{name: "empty source", response: scriptedResponse{proposal: Proposal{Summary: "missing source"}}},
		{name: "oversized summary", response: scriptedResponse{proposal: Proposal{Summary: strings.Repeat("s", 501), Source: "candidate"}}},
		{name: "oversized source", response: scriptedResponse{proposal: Proposal{Summary: "large", Source: strings.Repeat("s", 256*1024+1)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := &scriptedModel{responses: []scriptedResponse{test.response}}
			preflighter := &scriptedPreflighter{}

			candidate, err := NewService(model, preflighter).Suggest(context.Background(), validSuggestInput())
			if err == nil {
				t.Fatal("Suggest() error = nil, want provider or output-shape error")
			}
			if !reflect.DeepEqual(candidate, Candidate{}) {
				t.Errorf("candidate = %#v, want zero candidate", candidate)
			}
			if len(model.requests) != 1 || len(preflighter.calls) != 0 {
				t.Errorf("model/preflight calls = %d/%d, want 1/0", len(model.requests), len(preflighter.calls))
			}
		})
	}
}

type scriptedResponse struct {
	proposal Proposal
	err      error
}

type scriptedModel struct {
	responses []scriptedResponse
	requests  []ModelRequest
	mutate    func(*ModelRequest)
}

func (m *scriptedModel) Propose(_ context.Context, request ModelRequest) (Proposal, error) {
	if m.mutate != nil {
		m.mutate(&request)
	}
	m.requests = append(m.requests, request)
	if len(m.responses) == 0 {
		return Proposal{}, errUnexpectedModelCall
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.proposal, response.err
}

func testSHA256(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

type servicePreflightCall struct {
	source   string
	firstBar int
	lastBar  int
}

type scriptedPreflighter struct {
	calls []servicePreflightCall
	errs  []error
}

func (p *scriptedPreflighter) Preflight(_ context.Context, source string, firstBar, lastBar int) error {
	p.calls = append(p.calls, servicePreflightCall{
		source:   source,
		firstBar: firstBar,
		lastBar:  lastBar,
	})
	if len(p.errs) == 0 {
		return nil
	}
	err := p.errs[0]
	p.errs = p.errs[1:]
	return err
}

func validSuggestInput() SuggestInput {
	return SuggestInput{
		Provider:   "openai",
		Model:      "gpt-test",
		Prompt:     "Make the hats denser.",
		SourcePath: "/patterns/base.fnl",
		SoundsPath: "/sounds/808",
		Source:     []byte("(bpm 120)\n(steps 16)\n"),
		Samples:    []string{"kick.wav", "hat.wav"},
		Instruments: []Instrument{
			{Name: "bass", Description: "low voice", RecommendedRange: "C1-C3"},
			{Name: "brass", Description: "bright voice", RecommendedRange: "C2-C5"},
			{Name: "pluck", Description: "string voice", RecommendedRange: "C2-C6"},
		},
	}
}
