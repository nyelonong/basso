package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/gopxl/beep/v2"
)

// Instrument describes a built-in note voice exposed to patterns and suggestions.
type Instrument struct {
	Name             string
	Description      string
	RecommendedRange string
	Limits           string
}

type instrumentGroup uint8

const (
	instrumentGroupNone instrumentGroup = iota
	instrumentGroupElectronic
	instrumentGroupCount
)

type instrumentPolicy struct {
	mustEndWithinBar bool
	group            instrumentGroup
}

type instrumentGroupPolicy struct {
	name          string
	maxHits       int
	maxConcurrent int
}

var instrumentGroupPolicies = map[instrumentGroup]instrumentGroupPolicy{
	instrumentGroupElectronic: {
		name:          "lead/pad",
		maxHits:       64,
		maxConcurrent: 8,
	},
}

type instrumentPreset struct {
	info       Instrument
	policy     instrumentPolicy
	synthesize func(float64, time.Duration) (beep.Streamer, error)
}

var instrumentPresets = map[string]instrumentPreset{
	"bass": {
		info: Instrument{
			Name:             "bass",
			Description:      "punchy low sawtooth voice",
			RecommendedRange: "C1-C3",
		},
		synthesize: synthesizeNote,
	},
	"brass": {
		info: Instrument{
			Name:             "brass",
			Description:      "bright sawtooth voice with a slower swell",
			RecommendedRange: "C2-C5",
		},
		synthesize: synthesizeBrass,
	},
	"lead": {
		info: Instrument{
			Name:             "lead",
			Description:      "bright electronic square/saw melody voice",
			RecommendedRange: "C3-C6",
		},
		policy: instrumentPolicy{
			mustEndWithinBar: true,
			group:            instrumentGroupElectronic,
		},
		synthesize: synthesizeLead,
	},
	"pad": {
		info: Instrument{
			Name:             "pad",
			Description:      "warm filtered electronic chord voice",
			RecommendedRange: "C2-C5",
		},
		policy: instrumentPolicy{
			mustEndWithinBar: true,
			group:            instrumentGroupElectronic,
		},
		synthesize: synthesizePad,
	},
	"pluck": {
		info: Instrument{
			Name:             "pluck",
			Description:      "decaying plucked-string voice",
			RecommendedRange: "C2-C6",
		},
		synthesize: synthesizePluck,
	},
}

// InstrumentCatalog returns the built-in instruments sorted by name.
func InstrumentCatalog() []Instrument {
	names := make([]string, 0, len(instrumentPresets))
	for name := range instrumentPresets {
		names = append(names, name)
	}
	sort.Strings(names)

	catalog := make([]Instrument, 0, len(names))
	for _, name := range names {
		preset := instrumentPresets[name]
		info := preset.info
		if policy, ok := instrumentGroupPolicies[preset.policy.group]; ok {
			info.Limits = fmt.Sprintf(
				"must end within its bar; %s combined maximum %d hits per bar and %d simultaneous voices",
				policy.name, policy.maxHits, policy.maxConcurrent,
			)
		}
		catalog = append(catalog, info)
	}
	return catalog
}

func findInstrument(name string) (instrumentPreset, bool) {
	preset, ok := instrumentPresets[name]
	return preset, ok
}
