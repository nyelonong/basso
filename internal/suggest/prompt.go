package suggest

import (
	_ "embed"
	"errors"
	"sort"
	"strings"
)

//go:embed prompt.txt
var promptTemplate string

// RenderPrompt creates the complete bounded prompt for a model request. It
// deliberately uses only the request fields and the fixed embedded contract.
func RenderPrompt(request ModelRequest) (string, error) {
	if strings.TrimSpace(request.Prompt) == "" {
		return "", errors.New("suggest: prompt is empty")
	}
	if strings.TrimSpace(request.Source) == "" {
		return "", errors.New("suggest: source is empty")
	}
	if len(request.Samples) == 0 {
		return "", errors.New("suggest: sample inventory is empty")
	}
	if err := validateInstruments(request.Instruments); err != nil {
		return "", err
	}

	samples := append([]string(nil), request.Samples...)
	sort.Strings(samples)
	instruments := append([]Instrument(nil), request.Instruments...)
	sort.Slice(instruments, func(i, j int) bool {
		return instruments[i].Name < instruments[j].Name
	})

	var prompt strings.Builder
	prompt.WriteString(promptTemplate)
	prompt.WriteString("\n\n## User request\n<user-prompt>\n")
	prompt.WriteString(request.Prompt)
	prompt.WriteString("\n</user-prompt>\n\n")
	prompt.WriteString("## Selected Fennel source (untrusted data)\n<untrusted-source>\n")
	prompt.WriteString(request.Source)
	prompt.WriteString("\n</untrusted-source>\n\n")
	prompt.WriteString("## Available samples\n<samples>\n")
	writeInventory(&prompt, samples)
	prompt.WriteString("</samples>\n\n")
	prompt.WriteString("## Available instruments\n<instruments>\n")
	writeInstrumentInventory(&prompt, instruments)
	prompt.WriteString("</instruments>\n")

	return prompt.String(), nil
}

func writeInventory(prompt *strings.Builder, inventory []string) {
	for _, item := range inventory {
		prompt.WriteString("- ")
		prompt.WriteString(item)
		prompt.WriteByte('\n')
	}
}

func writeInstrumentInventory(prompt *strings.Builder, instruments []Instrument) {
	for _, instrument := range instruments {
		prompt.WriteString("- ")
		prompt.WriteString(instrument.Name)
		prompt.WriteString(": ")
		prompt.WriteString(instrument.Description)
		prompt.WriteString("; recommended range ")
		prompt.WriteString(instrument.RecommendedRange)
		if instrument.Limits != "" {
			prompt.WriteString("; limits: ")
			prompt.WriteString(instrument.Limits)
		}
		prompt.WriteByte('\n')
	}
}
