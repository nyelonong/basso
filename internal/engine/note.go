package engine

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

// noteNamePattern matches scientific pitch notation: one letter A-G
// (case-insensitive), an optional accidental (# for sharp, b for flat), then
// an integer octave (a leading '-' allowed, for very low octaves like C-1).
var noteNamePattern = regexp.MustCompile(`^([A-Ga-g])([#b]?)(-?[0-9]+)$`)

// noteSemitone is each natural note's semitone offset from C within an
// octave, standard equal-tempered spelling.
var noteSemitone = map[byte]int{
	'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11,
}

// parseNote parses a scientific-pitch-notation note name (e.g. "C4", "A#1",
// "Db2") into its equal-tempered frequency in Hz. A4 (MIDI note 69) is
// exactly 440Hz; every other note derives from 440 * 2^((midi-69)/12), where
// midi = (octave+1)*12 + semitone. Invalid input (bad letter, malformed
// octave, empty string) returns an error, never a panic or a zero value.
func parseNote(name string) (float64, error) {
	m := noteNamePattern.FindStringSubmatch(name)
	if m == nil {
		return 0, fmt.Errorf("engine: invalid note name %q", name)
	}

	letter := m[1][0]
	if letter >= 'a' && letter <= 'z' {
		letter -= 'a' - 'A'
	}
	semitone, ok := noteSemitone[letter]
	if !ok {
		return 0, fmt.Errorf("engine: invalid note letter in %q", name)
	}

	switch m[2] {
	case "#":
		semitone++
	case "b":
		semitone--
	}

	octave, err := strconv.Atoi(m[3])
	if err != nil {
		return 0, fmt.Errorf("engine: invalid octave in %q: %w", name, err)
	}

	midi := (octave+1)*12 + semitone
	freq := 440 * math.Pow(2, float64(midi-69)/12)
	return freq, nil
}
