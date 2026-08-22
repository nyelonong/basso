package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateBar_AcceptsCurrentPatterns(t *testing.T) {
	inventory := testInventory()
	tests := []struct {
		name string
		bar  Bar
	}{
		{
			name: "basic groove",
			bar: Bar{BPM: 120, StepsPerBar: 16, Hits: []Hit{
				{Step: 0, Sample: "kick2.wav", Pan: -0.5, Velocity: 1},
				{Step: 2, Sample: "cl_hihat.wav", Pan: 0.5, Velocity: 1},
				{Step: 4, Sample: "snare.wav", Pan: 0, Velocity: 1},
			}},
		},
		{
			name: "bass groove",
			bar: Bar{BPM: 100, StepsPerBar: 16, Hits: []Hit{
				{Step: 0, Sample: "kick2.wav", Pan: 0, Velocity: 1},
				{Step: 0, Note: "C2", Instrument: "bass", Length: 4, Pan: 0, Velocity: 1},
				{Step: 0, Note: "C4", Instrument: "brass", Length: 8, Pan: 0, Velocity: 0.5},
				{Step: 2, Note: "G3", Instrument: "pluck", Length: 2, Pan: 0, Velocity: 0.7},
			}},
		},
		{
			name: "four on the floor",
			bar: Bar{BPM: 128, StepsPerBar: 16, Hits: []Hit{
				{Step: 4, Sample: "handclap.wav", Pan: 0, Velocity: 0.9},
				{Step: 2, Sample: "open_hh.wav", Pan: -0.3, Velocity: 0.7},
			}},
		},
		{
			name: "funk bass groove",
			bar: Bar{BPM: 100, StepsPerBar: 16, Hits: []Hit{
				{Step: 15, Sample: "cl_hihat.wav", Pan: 0.2, Velocity: 0.5},
				{Step: 14, Sample: "snare.wav", Pan: -0.2, Velocity: 0.3},
				{Step: 8, Note: "E2", Instrument: "bass", Length: 1, Pan: 0, Velocity: 1},
			}},
		},
		{
			name: "generative",
			bar: Bar{BPM: 100, StepsPerBar: 16, Hits: []Hit{
				{Step: 0, Sample: "kick2.wav", Pan: 0.1, Velocity: 1},
				{Step: 14, Sample: "cl_hihat.wav", Pan: -0.1, Velocity: 0.55},
			}},
		},
		{
			name: "indo bounce",
			bar: Bar{BPM: 150, StepsPerBar: 16, Hits: []Hit{
				{Step: 15, Sample: "open_hh.wav", Pan: 0.4, Velocity: 0.8},
				{Step: 14, Note: "A1", Instrument: "bass", Length: 2, Pan: 0, Velocity: 1},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateBar(tt.bar, inventory); err != nil {
				t.Fatalf("ValidateBar() error = %v", err)
			}
		})
	}
}

func TestValidateBar_RejectsTempoBounds(t *testing.T) {
	tests := []struct {
		name    string
		bpm     int
		wantErr bool
	}{
		{name: "below minimum", bpm: 19, wantErr: true},
		{name: "minimum", bpm: 20},
		{name: "maximum", bpm: 400},
		{name: "above maximum", bpm: 401, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validSampleBar()
			bar.BPM = tt.bpm
			err := ValidateBar(bar, testInventory())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBar_RejectsStepsBounds(t *testing.T) {
	tests := []struct {
		name    string
		steps   int
		wantErr bool
	}{
		{name: "below minimum", steps: 0, wantErr: true},
		{name: "minimum", steps: 1},
		{name: "maximum", steps: 256},
		{name: "above maximum", steps: 257, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validSampleBar()
			bar.StepsPerBar = tt.steps
			if tt.steps == 1 {
				bar.Hits[0].Step = 0
			}
			err := ValidateBar(bar, testInventory())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBar_RejectsHitCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "maximum", count: 4096},
		{name: "above maximum", count: 4097, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := Bar{BPM: 120, StepsPerBar: 16, Hits: make([]Hit, tt.count)}
			for i := range bar.Hits {
				bar.Hits[i] = Hit{Step: 0, Sample: "kick2.wav", Pan: 0, Velocity: 1}
			}
			err := ValidateBar(bar, testInventory())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBar_RejectsStepOutsideBar(t *testing.T) {
	for _, step := range []int{-1, 16} {
		t.Run("step", func(t *testing.T) {
			bar := validSampleBar()
			bar.Hits[0].Step = step
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_RejectsNonFinitePanAndVelocity(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Hit)
	}{
		{name: "pan NaN", set: func(hit *Hit) { hit.Pan = math.NaN() }},
		{name: "pan positive infinity", set: func(hit *Hit) { hit.Pan = math.Inf(1) }},
		{name: "pan negative infinity", set: func(hit *Hit) { hit.Pan = math.Inf(-1) }},
		{name: "velocity NaN", set: func(hit *Hit) { hit.Velocity = math.NaN() }},
		{name: "velocity positive infinity", set: func(hit *Hit) { hit.Velocity = math.Inf(1) }},
		{name: "velocity negative infinity", set: func(hit *Hit) { hit.Velocity = math.Inf(-1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validSampleBar()
			tt.set(&bar.Hits[0])
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_RejectsPanAndVelocityBounds(t *testing.T) {
	tests := []struct {
		name    string
		set     func(*Hit)
		wantErr bool
	}{
		{name: "pan minimum", set: func(hit *Hit) { hit.Pan = -1 }},
		{name: "pan maximum", set: func(hit *Hit) { hit.Pan = 1 }},
		{name: "pan below minimum", set: func(hit *Hit) { hit.Pan = -1.01 }, wantErr: true},
		{name: "pan above maximum", set: func(hit *Hit) { hit.Pan = 1.01 }, wantErr: true},
		{name: "velocity minimum", set: func(hit *Hit) { hit.Velocity = 0 }},
		{name: "velocity maximum", set: func(hit *Hit) { hit.Velocity = 1 }},
		{name: "velocity below minimum", set: func(hit *Hit) { hit.Velocity = -0.01 }, wantErr: true},
		{name: "velocity above maximum", set: func(hit *Hit) { hit.Velocity = 1.01 }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validSampleBar()
			tt.set(&bar.Hits[0])
			err := ValidateBar(bar, testInventory())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBar_RejectsSampleOutsideInventory(t *testing.T) {
	tests := []struct {
		name string
		hit  Hit
	}{
		{name: "unknown sample", hit: Hit{Step: 0, Sample: "not-in-inventory.wav", Pan: 0, Velocity: 1}},
		{name: "missing sample and note", hit: Hit{Step: 0, Pan: 0, Velocity: 1}},
		{name: "both sample and note", hit: Hit{Step: 0, Sample: "kick2.wav", Note: "C2", Instrument: "bass", Length: 1, Pan: 0, Velocity: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validSampleBar()
			bar.Hits[0] = tt.hit
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_RejectsSamplePath(t *testing.T) {
	for _, sample := range []string{"nested/kick2.wav", `nested\\kick2.wav`, "../kick2.wav"} {
		t.Run(sample, func(t *testing.T) {
			bar := validSampleBar()
			bar.Hits[0].Sample = sample
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_RejectsInvalidNote(t *testing.T) {
	for _, note := range []string{"", "H2", "C#", "C2/extra"} {
		t.Run(note, func(t *testing.T) {
			bar := validNoteBar()
			bar.Hits[0].Note = note
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_RejectsLengthBounds(t *testing.T) {
	tests := []struct {
		name    string
		length  int
		wantErr bool
	}{
		{name: "below minimum", length: 0, wantErr: true},
		{name: "minimum", length: 1},
		{name: "maximum", length: 4096},
		{name: "above maximum", length: 4097, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := validNoteBar()
			bar.Hits[0].Length = tt.length
			err := ValidateBar(bar, testInventory())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBar_RejectsInstrument(t *testing.T) {
	for _, instrument := range []string{"", "drums", "Bass"} {
		t.Run(instrument, func(t *testing.T) {
			bar := validNoteBar()
			bar.Hits[0].Instrument = instrument
			if err := ValidateBar(bar, testInventory()); err == nil {
				t.Fatal("ValidateBar() error = nil, want error")
			}
		})
	}
}

func TestValidateBar_PaletteMustEndWithinBar(t *testing.T) {
	tests := []struct {
		name       string
		instrument string
		step       int
		length     int
		wantErr    bool
	}{
		{name: "lead ends at boundary", instrument: "lead", step: 12, length: 4},
		{name: "pad fills bar", instrument: "pad", step: 0, length: 16},
		{name: "lead crosses boundary", instrument: "lead", step: 13, length: 4, wantErr: true},
		{name: "pad crosses boundary", instrument: "pad", step: 1, length: 16, wantErr: true},
		{name: "existing bass may cross boundary", instrument: "bass", step: 15, length: 4096},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bar := paletteBar([]Hit{{
				Step: test.step, Note: "C3", Instrument: test.instrument,
				Length: test.length, Pan: 0, Velocity: 0.5,
			}})
			err := ValidateBar(bar, testInventory())
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateBar_PaletteHitLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "maximum", count: 64},
		{name: "above maximum", count: 65, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			hits := make([]Hit, test.count)
			for i := range hits {
				hits[i] = Hit{
					Step: i / 8, Note: "C3", Instrument: []string{"lead", "pad"}[i%2],
					Length: 1, Pan: 0, Velocity: 0.5,
				}
			}
			err := ValidateBar(paletteBar(hits), testInventory())
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateBar_PaletteOverlapLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "maximum", count: 8},
		{name: "above maximum", count: 9, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			hits := make([]Hit, test.count)
			for i := range hits {
				hits[i] = Hit{
					Step: 2, Note: "C3", Instrument: []string{"lead", "pad"}[i%2],
					Length: 4, Pan: 0, Velocity: 0.5,
				}
			}
			err := ValidateBar(paletteBar(hits), testInventory())
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBar() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func paletteBar(hits []Hit) Bar {
	return Bar{BPM: 120, StepsPerBar: 16, Hits: hits}
}

func TestLoadSoundInventory_RegularBasenamesOnly(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "kick.wav")
	if err := os.WriteFile(regular, []byte("kick"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(regular, filepath.Join(root, "linked.wav")); err != nil {
		t.Fatal(err)
	}

	inventory, err := LoadSoundInventory(root)
	if err != nil {
		t.Fatalf("LoadSoundInventory() error = %v", err)
	}
	if _, ok := inventory["kick.wav"]; !ok {
		t.Fatal("LoadSoundInventory() omitted regular basename")
	}
	for _, name := range []string{"nested", "linked.wav"} {
		if _, ok := inventory[name]; ok {
			t.Fatalf("LoadSoundInventory() included non-regular entry %q", name)
		}
	}

	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(t.TempDir(), "sounds-link")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), file, linkRoot} {
		if _, err := LoadSoundInventory(path); err == nil {
			t.Fatalf("LoadSoundInventory(%q) error = nil, want error", path)
		}
	}
}

func validSampleBar() Bar {
	return Bar{
		BPM:         120,
		StepsPerBar: 16,
		Hits: []Hit{{
			Step:     0,
			Sample:   "kick2.wav",
			Pan:      0,
			Velocity: 1,
		}},
	}
}

func validNoteBar() Bar {
	return Bar{
		BPM:         120,
		StepsPerBar: 16,
		Hits: []Hit{{
			Step:       0,
			Note:       "C2",
			Instrument: "bass",
			Length:     1,
			Pan:        0,
			Velocity:   1,
		}},
	}
}

func testInventory() SoundInventory {
	return SoundInventory{
		"cl_hihat.wav": {},
		"handclap.wav": {},
		"kick2.wav":    {},
		"open_hh.wav":  {},
		"snare.wav":    {},
	}
}
