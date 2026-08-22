package suggest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/engine"
)

func renderPromptForTest(t *testing.T) string {
	t.Helper()

	got, err := RenderPrompt(ModelRequest{
		Prompt:  "Keep the kick and make the hats denser.",
		Source:  "(bpm 120)\n(steps 16)\n(fn pattern [bar] [])\npattern",
		Samples: []string{"snare.wav", "kick.wav", "hat.wav"},
		Instruments: []Instrument{
			{Name: "pluck", Description: "decaying string", RecommendedRange: "C2-C6"},
			{Name: "lead", Description: "bright melody", RecommendedRange: "C3-C6", Limits: "must end within its bar"},
			{Name: "bass", Description: "low voice", RecommendedRange: "C1-C3"},
			{Name: "pad", Description: "warm chord voice", RecommendedRange: "C2-C5", Limits: "must end within its bar"},
			{Name: "brass", Description: "bright swell", RecommendedRange: "C2-C5"},
		},
	})
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}
	return got
}

func TestRenderPrompt_ContainsOnlyAllowedContext(t *testing.T) {
	got := renderPromptForTest(t)

	for _, want := range []string{
		"## Basso script API",
		"## Requirements",
		"## User request\n<user-prompt>\nKeep the kick and make the hats denser.\n</user-prompt>",
		"## Available samples\n<samples>\n- hat.wav\n- kick.wav\n- snare.wav\n</samples>",
		"## Available instruments\n<instruments>\n- bass: low voice; recommended range C1-C3\n- brass: bright swell; recommended range C2-C5\n- lead: bright melody; recommended range C3-C6; limits: must end within its bar\n- pad: warm chord voice; recommended range C2-C5; limits: must end within its bar\n- pluck: decaying string; recommended range C2-C6\n</instruments>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt does not contain required section %q\nprompt:\n%s", want, got)
		}
	}

	for _, forbidden := range []string{
		"OPENAI_API_KEY",
		"BASSO_AI_PROVIDER",
		"BASSO_AI_MODEL",
		"Repository:",
		"Git history:",
		"Candidate history:",
		"Credentials:",
		"Tool context:",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("rendered prompt unexpectedly contains forbidden context %q", forbidden)
		}
	}
}

func TestRenderPrompt_DelimitsSourceAsData(t *testing.T) {
	got := renderPromptForTest(t)
	want := "## Selected Fennel source (untrusted data)\n<untrusted-source>\n" +
		"(bpm 120)\n(steps 16)\n(fn pattern [bar] [])\npattern\n" +
		"</untrusted-source>"
	if !strings.Contains(got, want) {
		t.Errorf("rendered prompt does not delimit source as untrusted data\nprompt:\n%s", got)
	}
}

func TestRenderPrompt_ListsSortedInventory(t *testing.T) {
	request := ModelRequest{
		Prompt:  "Add a response.",
		Source:  "pattern",
		Samples: []string{"snare.wav", "kick.wav", "hat.wav"},
		Instruments: []Instrument{
			{Name: "pluck", Description: "pluck", RecommendedRange: "C2-C6"},
			{Name: "bass", Description: "bass", RecommendedRange: "C1-C3"},
			{Name: "brass", Description: "brass", RecommendedRange: "C2-C5"},
		},
	}
	got, err := RenderPrompt(request)
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}
	if strings.Index(got, "- hat.wav") > strings.Index(got, "- kick.wav") ||
		strings.Index(got, "- kick.wav") > strings.Index(got, "- snare.wav") {
		t.Errorf("samples are not sorted:\n%s", got)
	}
	if strings.Join(request.Samples, ",") != "snare.wav,kick.wav,hat.wav" {
		t.Errorf("RenderPrompt() mutated samples: %v", request.Samples)
	}
	if request.Instruments[0].Name != "pluck" {
		t.Errorf("RenderPrompt() mutated instruments: %v", request.Instruments)
	}
	if strings.Index(got, "- bass:") > strings.Index(got, "- brass:") ||
		strings.Index(got, "- brass:") > strings.Index(got, "- pluck:") {
		t.Errorf("instruments are not sorted:\n%s", got)
	}
}

func TestRenderPrompt_IncludesOneFixedExample(t *testing.T) {
	got := renderPromptForTest(t)
	if strings.Count(got, "## Fixed valid example") != 1 {
		t.Errorf("fixed example section count = %d, want 1\nprompt:\n%s", strings.Count(got, "## Fixed valid example"), got)
	}
	want := "## Fixed valid example\n```fennel\n(bpm 120)\n(steps 16)\n\n(fn pattern [bar]\n  (let [lead-note (if (= (% bar 2) 0) \"E4\" \"G4\")]\n    [{:step 0 :note \"C3\" :instrument \"pad\" :length 16 :velocity 0.3}\n     {:step 0 :note \"E3\" :instrument \"pad\" :length 16 :velocity 0.3}\n     {:step 0 :note \"G3\" :instrument \"pad\" :length 16 :velocity 0.3}\n     {:step 8 :note lead-note :instrument \"lead\" :length 2 :velocity 0.7}]))\n\npattern\n```"
	if !strings.Contains(got, want) {
		t.Errorf("rendered prompt does not contain the fixed valid example\nprompt:\n%s", got)
	}
}

func TestRenderPrompt_FixedExamplePreflights(t *testing.T) {
	got := renderPromptForTest(t)
	const startMarker = "## Fixed valid example\n```fennel\n"
	start := strings.Index(got, startMarker)
	if start < 0 {
		t.Fatal("fixed example start marker missing")
	}
	start += len(startMarker)
	end := strings.Index(got[start:], "\n```")
	if end < 0 {
		t.Fatal("fixed example end marker missing")
	}
	source := got[start : start+end]
	evaluator := engine.NewEvaluator(engine.SoundInventory{"unused.wav": {}}, 250*time.Millisecond)
	if err := evaluator.Preflight(context.Background(), source, 0, 15); err != nil {
		t.Fatalf("fixed example preflight error = %v", err)
	}
}

func TestRenderPrompt_RejectsEmptyFields(t *testing.T) {
	valid := ModelRequest{
		Prompt:      "Add a response.",
		Source:      "pattern",
		Samples:     []string{"kick.wav"},
		Instruments: []Instrument{{Name: "bass", Description: "bass", RecommendedRange: "C1-C3"}},
	}
	tests := []struct {
		name string
		edit func(*ModelRequest)
	}{
		{name: "prompt", edit: func(request *ModelRequest) { request.Prompt = "" }},
		{name: "source", edit: func(request *ModelRequest) { request.Source = "" }},
		{name: "samples", edit: func(request *ModelRequest) { request.Samples = nil }},
		{name: "instruments", edit: func(request *ModelRequest) { request.Instruments = nil }},
		{name: "instrument name", edit: func(request *ModelRequest) { request.Instruments[0].Name = "" }},
		{name: "instrument description", edit: func(request *ModelRequest) { request.Instruments[0].Description = "" }},
		{name: "instrument range", edit: func(request *ModelRequest) { request.Instruments[0].RecommendedRange = "" }},
		{name: "duplicate instrument", edit: func(request *ModelRequest) {
			request.Instruments = append(request.Instruments, request.Instruments[0])
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			request.Instruments = append([]Instrument(nil), valid.Instruments...)
			test.edit(&request)
			if _, err := RenderPrompt(request); err == nil {
				t.Fatal("RenderPrompt() error = nil, want error")
			}
		})
	}
}
