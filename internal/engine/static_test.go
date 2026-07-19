package engine

import (
	"testing"
)

// TestStaticProvider_ReturnsM001Hits verifies that StaticProvider.Next returns
// exactly 16 hits with the correct Step, Sample, and Velocity values in the
// correct order, matching the m001 pattern. Pan values are not asserted exactly
// since they are randomized per call.
func TestStaticProvider_ReturnsM001Hits(t *testing.T) {
	provider := &StaticProvider{}
	hits, bpm, stepsPerBar, err := provider.Next(0)

	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}

	if bpm != 120 {
		t.Errorf("bpm = %d, want 120", bpm)
	}

	if stepsPerBar != 16 {
		t.Errorf("stepsPerBar = %d, want 16", stepsPerBar)
	}

	if len(hits) != 16 {
		t.Fatalf("len(hits) = %d, want 16", len(hits))
	}

	// Expected m001 pattern in order
	expected := []struct {
		step     int
		sample   string
		velocity float64
	}{
		{0, "kick2.wav", 1.0},
		{1, "maracas.wav", 1.0},
		{2, "cl_hihat.wav", 1.0},
		{3, "maracas.wav", 1.0},
		{4, "snare.wav", 1.0},
		{5, "maracas.wav", 1.0},
		{6, "cl_hihat.wav", 1.0},
		{7, "kick2.wav", 1.0},
		{8, "maracas.wav", 1.0},
		{9, "maracas.wav", 1.0},
		{10, "hightom.wav", 1.0},
		{11, "maracas.wav", 1.0},
		{12, "snare.wav", 1.0},
		{13, "kick1.wav", 1.0},
		{14, "cl_hihat.wav", 1.0},
		{15, "maracas.wav", 1.0},
	}

	for i, exp := range expected {
		if hits[i].Step != exp.step {
			t.Errorf("hits[%d].Step = %d, want %d", i, hits[i].Step, exp.step)
		}
		if hits[i].Sample != exp.sample {
			t.Errorf("hits[%d].Sample = %q, want %q", i, hits[i].Sample, exp.sample)
		}
		if hits[i].Velocity != exp.velocity {
			t.Errorf("hits[%d].Velocity = %v, want %v", i, hits[i].Velocity, exp.velocity)
		}
		// Pan should be randomized per call, so just verify it's in the range [-1, 1]
		if hits[i].Pan < -1.0 || hits[i].Pan > 1.0 {
			t.Errorf("hits[%d].Pan = %v, want value in range [-1, 1]", i, hits[i].Pan)
		}
	}
}

// TestStaticProvider_Tempo verifies that StaticProvider.Next returns the
// correct bpm (120) and stepsPerBar (16) values.
func TestStaticProvider_Tempo(t *testing.T) {
	provider := &StaticProvider{}
	_, bpm, stepsPerBar, err := provider.Next(0)

	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}

	if bpm != 120 {
		t.Errorf("bpm = %d, want 120", bpm)
	}

	if stepsPerBar != 16 {
		t.Errorf("stepsPerBar = %d, want 16", stepsPerBar)
	}
}
