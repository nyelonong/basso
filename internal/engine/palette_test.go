package engine

import (
	"reflect"
	"testing"
)

func TestInstrumentCatalog(t *testing.T) {
	catalog := InstrumentCatalog()
	var names []string
	for _, instrument := range catalog {
		names = append(names, instrument.Name)
		if instrument.Description == "" {
			t.Errorf("instrument %q description is empty", instrument.Name)
		}
		if instrument.RecommendedRange == "" {
			t.Errorf("instrument %q recommended range is empty", instrument.Name)
		}
		if (instrument.Name == "lead" || instrument.Name == "pad") && instrument.Limits == "" {
			t.Errorf("instrument %q limits are empty", instrument.Name)
		}
	}
	if want := []string{"bass", "brass", "lead", "pad", "pluck"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("InstrumentCatalog() names = %v, want %v", names, want)
	}

	catalog[0].Name = "changed"
	if got := InstrumentCatalog()[0].Name; got != "bass" {
		t.Fatalf("InstrumentCatalog() shared mutable data: first name = %q, want bass", got)
	}
}

func TestInstrumentPalette(t *testing.T) {
	for name, preset := range instrumentPresets {
		if preset.info.Name != name {
			t.Errorf("preset %q info name = %q", name, preset.info.Name)
		}
		if preset.synthesize == nil {
			t.Errorf("preset %q synthesize function is nil", name)
		}
		if preset.policy.group != instrumentGroupNone {
			policy, ok := instrumentGroupPolicies[preset.policy.group]
			if !ok {
				t.Errorf("preset %q references a missing group policy", name)
			}
			if !preset.policy.mustEndWithinBar {
				t.Errorf("preset %q has bounded overlap without within-bar duration", name)
			}
			if policy.maxHits < 1 || policy.maxConcurrent < 1 {
				t.Errorf("preset %q group limits = %+v, want positive limits", name, policy)
			}
		}
	}
}
