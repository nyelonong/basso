//go:build manual

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nyelonong/basso/internal/suggest"
)

// TestLive_StudioSuggestApplyPipeline drives the studio cockpit pipeline
// against the real configured provider: suggest -> validate -> save -> apply.
func TestLive_StudioSuggestApplyPipeline(t *testing.T) {
	if os.Getenv("BASSO_LIVE") == "" {
		t.Skip("set BASSO_LIVE=1 to run")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "pattern.fnl")
	original, err := os.ReadFile("../../patterns/four-on-the-floor.fnl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := defaultCommandDependencies(os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := filepath.Abs(filepath.Join("..", "..", "sound", "808"))
	if err != nil {
		t.Fatal(err)
	}
	services := studioServices{
		getenv:         deps.getenv,
		invocationDir:  dir,
		soundsPath:     inventory,
		newModel:       newConcreteModel,
		newPreflighter: newEvaluatorPreflighter,
	}

	m := newStudioModel("pattern.fnl")
	cmd := m.newSuggestCmd(services, source, "double the tempo and add cowbell on every offbeat")
	msg := cmd()

	switch got := msg.(type) {
	case suggestionFailedMsg:
		t.Fatalf("provider failed: %v", got.err)
	case suggestionReadyMsg:
		candidate := got.candidate
		t.Logf("summary: %s", candidate.Metadata.Summary)
		t.Logf("validation: %s (%d bars)", candidate.Metadata.Validation.Status, candidate.Metadata.Validation.LastBar+1)

		storeRoot := filepath.Join(dir, ".basso")
		store := suggest.NewStore(storeRoot, deps.now)
		saved, err := store.Save(candidate)
		if err != nil {
			t.Fatal(err)
		}
		result, err := suggest.NewApplier(store, newEvaluatorPreflighter, nil).Apply(context.Background(), saved.Metadata.ID)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		applied, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(applied), "cowbell") {
			t.Errorf("applied source lacks cowbell:\n%s", applied)
		}
		t.Logf("applied OK; backup: %s", result.BackupPath)
	default:
		t.Fatalf("unexpected msg %T", msg)
	}
}
