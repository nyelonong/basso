package engine

import (
	"math"
	"testing"
)

// TestParseNote verifies scientific-pitch-notation parsing against the
// standard equal-tempered reference points: A4 is exactly 440Hz, C4 (middle
// C) is ≈261.6256Hz, enharmonic spellings (C#2/Db2) produce equal
// frequencies, and malformed input returns an error rather than a panic or
// a zero value.
func TestParseNote(t *testing.T) {
	tests := []struct {
		name    string
		note    string
		want    float64
		wantErr bool
	}{
		{name: "A4 is exactly 440Hz", note: "A4", want: 440.0},
		{name: "C4 is middle C", note: "C4", want: 261.6256},
		{name: "C-1 is MIDI note 0", note: "C-1", want: 8.1757989156},
		{name: "empty string", note: "", wantErr: true},
		{name: "bad letter", note: "H4", wantErr: true},
		{name: "malformed octave", note: "C#", wantErr: true},
		{name: "missing letter", note: "4", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNote(tt.note)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseNote(%q) error = nil, want error", tt.note)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNote(%q) error = %v, want nil", tt.note, err)
			}
			if math.Abs(got-tt.want) > 1e-4 {
				t.Errorf("parseNote(%q) = %v, want ≈%v", tt.note, got, tt.want)
			}
		})
	}
}

// TestParseNote_Enharmonic verifies that C#2 and Db2 — the same pitch
// spelled two ways — parse to exactly the same frequency.
func TestParseNote_Enharmonic(t *testing.T) {
	sharp, err := parseNote("C#2")
	if err != nil {
		t.Fatalf("parseNote(C#2) error = %v, want nil", err)
	}
	flat, err := parseNote("Db2")
	if err != nil {
		t.Fatalf("parseNote(Db2) error = %v, want nil", err)
	}
	if sharp != flat {
		t.Errorf("parseNote(C#2) = %v, parseNote(Db2) = %v, want equal", sharp, flat)
	}
}
