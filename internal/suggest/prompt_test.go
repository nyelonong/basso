package suggest

import (
	"strings"
	"testing"
)

func renderPromptForTest(t *testing.T) string {
	t.Helper()

	got, err := RenderPrompt(ModelRequest{
		Prompt:      "Keep the kick and make the hats denser.",
		Source:      "(bpm 120)\n(steps 16)\n(fn pattern [bar] [])\npattern",
		Samples:     []string{"snare.wav", "kick.wav", "hat.wav"},
		Instruments: []string{"brass", "bass", "pluck"},
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
		"## Available instruments\n<instruments>\n- brass\n- bass\n- pluck\n</instruments>",
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
		Prompt:      "Add a response.",
		Source:      "pattern",
		Samples:     []string{"snare.wav", "kick.wav", "hat.wav"},
		Instruments: []string{"pluck", "bass", "brass"},
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
}

func TestRenderPrompt_IncludesOneFixedExample(t *testing.T) {
	got := renderPromptForTest(t)
	if strings.Count(got, "## Fixed valid example") != 1 {
		t.Errorf("fixed example section count = %d, want 1\nprompt:\n%s", strings.Count(got, "## Fixed valid example"), got)
	}
	want := "## Fixed valid example\n```fennel\n(bpm 120)\n(steps 16)\n\n(fn pattern [bar]\n  [{:step 0 :sample \"kick.wav\"}\n   {:step 4 :sample \"snare.wav\"}])\n\npattern\n```"
	if !strings.Contains(got, want) {
		t.Errorf("rendered prompt does not contain the fixed valid example\nprompt:\n%s", got)
	}
}

func TestRenderPrompt_RejectsEmptyFields(t *testing.T) {
	valid := ModelRequest{
		Prompt:      "Add a response.",
		Source:      "pattern",
		Samples:     []string{"kick.wav"},
		Instruments: []string{"bass"},
	}
	tests := []struct {
		name string
		edit func(*ModelRequest)
	}{
		{name: "prompt", edit: func(request *ModelRequest) { request.Prompt = "" }},
		{name: "source", edit: func(request *ModelRequest) { request.Source = "" }},
		{name: "samples", edit: func(request *ModelRequest) { request.Samples = nil }},
		{name: "instruments", edit: func(request *ModelRequest) { request.Instruments = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.edit(&request)
			if _, err := RenderPrompt(request); err == nil {
				t.Fatal("RenderPrompt() error = nil, want error")
			}
		})
	}
}
