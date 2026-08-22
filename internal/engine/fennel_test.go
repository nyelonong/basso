package engine

import "testing"

func TestNew_RejectsNilEvaluator(t *testing.T) {
	if _, err := New(fennelSourceSample("kick2.wav"), nil, nil); err == nil {
		t.Fatal("New() error = nil, want nil evaluator error")
	}
}

// TestFennelProvider_ReproducesM001 verifies that a .fnl source reproducing
// the m001 pattern compiles via New and that Next(0) returns hits matching
// StaticProvider's Next(0) on Step, Sample, and Velocity (Pan is randomized
// on both sides and not compared, same reasoning as 3.1).
func TestFennelProvider_ReproducesM001(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :sample "kick2.wav" :velocity 1.0}
   {:step 1 :sample "maracas.wav" :velocity 1.0}
   {:step 2 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 3 :sample "maracas.wav" :velocity 1.0}
   {:step 4 :sample "snare.wav" :velocity 1.0}
   {:step 5 :sample "maracas.wav" :velocity 1.0}
   {:step 6 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 7 :sample "kick2.wav" :velocity 1.0}
   {:step 8 :sample "maracas.wav" :velocity 1.0}
   {:step 9 :sample "maracas.wav" :velocity 1.0}
   {:step 10 :sample "hightom.wav" :velocity 1.0}
   {:step 11 :sample "maracas.wav" :velocity 1.0}
   {:step 12 :sample "snare.wav" :velocity 1.0}
   {:step 13 :sample "kick1.wav" :velocity 1.0}
   {:step 14 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 15 :sample "maracas.wav" :velocity 1.0}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, bpm, stepsPerBar, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}

	wantHits, wantBPM, wantStepsPerBar, err := (&StaticProvider{}).Next(0)
	if err != nil {
		t.Fatalf("StaticProvider.Next(0) error = %v, want nil", err)
	}

	if bpm != wantBPM {
		t.Errorf("bpm = %d, want %d", bpm, wantBPM)
	}
	if stepsPerBar != wantStepsPerBar {
		t.Errorf("stepsPerBar = %d, want %d", stepsPerBar, wantStepsPerBar)
	}
	if len(hits) != len(wantHits) {
		t.Fatalf("len(hits) = %d, want %d", len(hits), len(wantHits))
	}
	for i := range wantHits {
		if hits[i].Step != wantHits[i].Step {
			t.Errorf("hits[%d].Step = %d, want %d", i, hits[i].Step, wantHits[i].Step)
		}
		if hits[i].Sample != wantHits[i].Sample {
			t.Errorf("hits[%d].Sample = %q, want %q", i, hits[i].Sample, wantHits[i].Sample)
		}
		if hits[i].Velocity != wantHits[i].Velocity {
			t.Errorf("hits[%d].Velocity = %v, want %v", i, hits[i].Velocity, wantHits[i].Velocity)
		}
	}
}

// TestFennelProvider_HitDefaults verifies that a hit table omitting :pan
// gets a random value in [-1,1], and a hit table omitting :velocity gets
// 1.0, matching StaticProvider's Go-level defaulting precedent (3.1).
func TestFennelProvider_HitDefaults(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :sample "kick2.wav" :velocity 0.5}
   {:step 1 :sample "snare.wav" :pan 0.3}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}

	// hits[0] omits :pan.
	if hits[0].Pan < -1.0 || hits[0].Pan > 1.0 {
		t.Errorf("hits[0].Pan = %v, want value in range [-1, 1]", hits[0].Pan)
	}

	// hits[1] omits :velocity.
	if hits[1].Velocity != 1.0 {
		t.Errorf("hits[1].Velocity = %v, want 1.0", hits[1].Velocity)
	}
}

// TestFennelProvider_MapsNoteHit verifies that a hit table with :note and
// :length (instead of :sample) maps to a Hit with Note/Length set and
// Sample empty.
func TestFennelProvider_MapsNoteHit(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 3 :note "C2" :length 4}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}

	got := hits[0]
	if got.Step != 3 {
		t.Errorf("Step = %d, want 3", got.Step)
	}
	if got.Note != "C2" {
		t.Errorf("Note = %q, want %q", got.Note, "C2")
	}
	if got.Sample != "" {
		t.Errorf("Sample = %q, want empty", got.Sample)
	}
	if got.Length != 4 {
		t.Errorf("Length = %d, want 4", got.Length)
	}
}

// TestFennelProvider_NoteHitDefaultsLength verifies that a :note hit
// omitting :length defaults to Length: 1.
func TestFennelProvider_NoteHitDefaultsLength(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :note "A2"}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Length != 1 {
		t.Errorf("Length = %d, want 1", hits[0].Length)
	}
}

// TestFennelProvider_NoteHitDefaultsPanToCenter verifies that a :note hit
// omitting :pan defaults to exactly 0 (centered) — not the random pan
// :sample hits get when they omit :pan. Standard mixing practice centers
// bass; random pan also made an already-quiet, low-frequency note easy to
// lose when it happened to land near a hard-left/hard-right extreme (root
// cause of a real "sometimes the bass isn't heard" report).
func TestFennelProvider_NoteHitDefaultsPanToCenter(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :note "A2"}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	for bar := 0; bar < 20; bar++ {
		hits, _, _, err := provider.Next(bar)
		if err != nil {
			t.Fatalf("Next(%d) error = %v, want nil", bar, err)
		}
		if len(hits) != 1 {
			t.Fatalf("Next(%d): len(hits) = %d, want 1", bar, len(hits))
		}
		if hits[0].Pan != 0 {
			t.Fatalf("Next(%d): Pan = %v, want exactly 0", bar, hits[0].Pan)
		}
	}
}

// TestFennelProvider_MapsInstrument verifies that a :note hit's :instrument
// key maps directly to Hit.Instrument.
func TestFennelProvider_MapsInstrument(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :note "C2" :instrument "brass"}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Instrument != "brass" {
		t.Errorf("Instrument = %q, want %q", hits[0].Instrument, "brass")
	}
}

func TestFennelProvider_MapsPaletteInstruments(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :note "C3" :instrument "pad" :length 4}
   {:step 8 :note "E4" :instrument "lead" :length 2}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 2 || hits[0].Instrument != "pad" || hits[1].Instrument != "lead" {
		t.Fatalf("hits = %+v, want pad then lead", hits)
	}
}

// TestFennelProvider_NoteHitDefaultsInstrumentToBass verifies that a :note
// hit omitting :instrument defaults to Hit.Instrument == "bass", so every
// existing pattern (bass-groove.fnl etc.) keeps working unchanged.
func TestFennelProvider_NoteHitDefaultsInstrumentToBass(t *testing.T) {
	source := `
(fn pattern [bar]
  [{:step 0 :note "A2"}])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Instrument != "bass" {
		t.Errorf("Instrument = %q, want %q", hits[0].Instrument, "bass")
	}
}

// TestFennelProvider_HitRequiresExactlyOneOfSampleOrNote verifies that a hit
// table with both :sample and :note, or with neither, makes Next return an
// error rather than a malformed Hit.
func TestFennelProvider_HitRequiresExactlyOneOfSampleOrNote(t *testing.T) {
	tests := []struct {
		name string
		hit  string
	}{
		{name: "both sample and note", hit: `{:step 0 :sample "kick2.wav" :note "C2"}`},
		{name: "neither sample nor note", hit: `{:step 0 :velocity 1.0}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "(fn pattern [bar]\n  [" + tt.hit + "])\n\npattern\n"

			if _, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil); err == nil {
				t.Fatalf("New() error = nil, want error")
			}
		})
	}
}

// TestFennelProvider_TempoFunctions verifies that a script calling (bpm 140)
// and (steps 12) makes Next return bpm == 140 and stepsPerBar == 12.
func TestFennelProvider_TempoFunctions(t *testing.T) {
	source := `
(bpm 140)
(steps 12)

(fn pattern [bar]
  [])

pattern
`

	provider, err := New(source, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	_, bpm, stepsPerBar, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}

	if bpm != 140 {
		t.Errorf("bpm = %d, want 140", bpm)
	}
	if stepsPerBar != 12 {
		t.Errorf("stepsPerBar = %d, want 12", stepsPerBar)
	}
}

func fennelProviderTestInventory() SoundInventory {
	inventory := testInventory()
	for _, name := range []string{
		"hightom.wav",
		"kick1.wav",
		"maracas.wav",
	} {
		inventory[name] = struct{}{}
	}
	return inventory
}
