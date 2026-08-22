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
	}
	if want := []string{"bass", "brass", "pluck"}; !reflect.DeepEqual(names, want) {
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
	}
}
