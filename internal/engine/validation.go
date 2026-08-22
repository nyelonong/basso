package engine

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Bar is the complete musical material and timing for one bar.
type Bar struct {
	Hits        []Hit
	BPM         int
	StepsPerBar int
}

// SoundInventory contains the regular-file sound basenames available to a bar.
type SoundInventory map[string]struct{}

// LoadSoundInventory returns the regular-file basenames directly inside path.
// It refuses roots that are absent, symbolic links, or not directories.
func LoadSoundInventory(path string) (SoundInventory, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("engine: inspect sound inventory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("engine: sound inventory root %q must not be a symbolic link", path)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("engine: sound inventory root %q is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("engine: read sound inventory %q: %w", path, err)
	}

	inventory := make(SoundInventory)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("engine: inspect sound %q: %w", filepath.Join(path, entry.Name()), err)
		}
		if entryInfo.Mode().IsRegular() {
			inventory[entry.Name()] = struct{}{}
		}
	}

	return inventory, nil
}

// ValidateBar verifies that bar is safe to schedule with inventory's samples.
func ValidateBar(bar Bar, inventory SoundInventory) error {
	if bar.BPM < 20 || bar.BPM > 400 {
		return fmt.Errorf("engine: BPM %d must be in [20,400]", bar.BPM)
	}
	if bar.StepsPerBar < 1 || bar.StepsPerBar > 256 {
		return fmt.Errorf("engine: steps per bar %d must be in [1,256]", bar.StepsPerBar)
	}
	if len(bar.Hits) > 4096 {
		return fmt.Errorf("engine: hit count %d exceeds 4096", len(bar.Hits))
	}

	type groupUsage struct {
		hits    int
		changes []int
	}
	var usages map[instrumentGroup]*groupUsage

	for index, hit := range bar.Hits {
		if hit.Step < 0 || hit.Step >= bar.StepsPerBar {
			return fmt.Errorf("engine: hit %d step %d is outside [0,%d)", index, hit.Step, bar.StepsPerBar)
		}
		if math.IsNaN(hit.Pan) || math.IsInf(hit.Pan, 0) || hit.Pan < -1 || hit.Pan > 1 {
			return fmt.Errorf("engine: hit %d pan must be finite and in [-1,1]", index)
		}
		if math.IsNaN(hit.Velocity) || math.IsInf(hit.Velocity, 0) || hit.Velocity < 0 || hit.Velocity > 1 {
			return fmt.Errorf("engine: hit %d velocity must be finite and in [0,1]", index)
		}

		hasSample := hit.Sample != ""
		hasNote := hit.Note != ""
		if hasSample == hasNote {
			return fmt.Errorf("engine: hit %d must have exactly one of sample or note", index)
		}

		if hasSample {
			if strings.ContainsAny(hit.Sample, "/\\\\") || hit.Sample == "." || hit.Sample == ".." {
				return fmt.Errorf("engine: hit %d sample %q must be a basename", index, hit.Sample)
			}
			if _, ok := inventory[hit.Sample]; !ok {
				return fmt.Errorf("engine: hit %d sample %q is not in the sound inventory", index, hit.Sample)
			}
			continue
		}

		if _, err := parseNote(hit.Note); err != nil {
			return fmt.Errorf("engine: hit %d note %q is invalid: %w", index, hit.Note, err)
		}
		if hit.Length < 1 || hit.Length > 4096 {
			return fmt.Errorf("engine: hit %d note length %d must be in [1,4096]", index, hit.Length)
		}
		preset, ok := findInstrument(hit.Instrument)
		if !ok {
			return fmt.Errorf("engine: hit %d instrument %q is not supported", index, hit.Instrument)
		}
		if preset.policy.mustEndWithinBar && hit.Step+hit.Length > bar.StepsPerBar {
			return fmt.Errorf("engine: hit %d instrument %q must end within its bar", index, hit.Instrument)
		}
		if preset.policy.group != instrumentGroupNone {
			if usages == nil {
				usages = make(map[instrumentGroup]*groupUsage)
			}
			usage := usages[preset.policy.group]
			if usage == nil {
				usage = &groupUsage{changes: make([]int, bar.StepsPerBar+1)}
				usages[preset.policy.group] = usage
			}
			usage.hits++
			usage.changes[hit.Step]++
			usage.changes[hit.Step+hit.Length]--
		}
	}

	for group := instrumentGroup(1); group < instrumentGroupCount; group++ {
		usage := usages[group]
		if usage == nil {
			continue
		}
		policy := instrumentGroupPolicies[group]
		if usage.hits > policy.maxHits {
			return fmt.Errorf("engine: %s hit count %d exceeds %d", policy.name, usage.hits, policy.maxHits)
		}
		active := 0
		for step := range bar.StepsPerBar {
			active += usage.changes[step]
			if active > policy.maxConcurrent {
				return fmt.Errorf("engine: %s overlap %d at step %d exceeds %d voices", policy.name, active, step, policy.maxConcurrent)
			}
		}
	}

	return nil
}
