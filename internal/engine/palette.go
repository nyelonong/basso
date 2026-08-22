package engine

import (
	"sort"
	"time"

	"github.com/gopxl/beep/v2"
)

// Instrument describes a built-in note voice exposed to patterns and suggestions.
type Instrument struct {
	Name             string
	Description      string
	RecommendedRange string
}

type instrumentPreset struct {
	info       Instrument
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
		catalog = append(catalog, instrumentPresets[name].info)
	}
	return catalog
}

func findInstrument(name string) (instrumentPreset, bool) {
	preset, ok := instrumentPresets[name]
	return preset, ok
}
