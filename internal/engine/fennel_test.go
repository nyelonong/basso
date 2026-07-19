package engine

import "testing"

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

	provider, err := New(source)
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

	provider, err := New(source)
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

	provider, err := New(source)
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
