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
