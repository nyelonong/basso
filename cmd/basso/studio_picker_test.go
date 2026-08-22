package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/engine"
)

func TestStudioPicker_ListsFlatFNLFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.fnl", "a.fnl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "hidden.fnl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	model, err := newStudioPickerModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	view := model.View()
	for _, want := range []string{"a.fnl", "b.fnl"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() = %q, want %q", view, want)
		}
	}
	for _, unwanted := range []string{"notes.txt", "hidden.fnl"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("View() = %q, do not want %q", view, unwanted)
		}
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, command := updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	picked := updated.(studioPickerModel)
	if command == nil || picked.result.path != filepath.Join(dir, "b.fnl") {
		t.Fatalf("picked path/command = %q/%v, want b.fnl/quit", picked.result.path, command)
	}
}

func TestStudioPicker_NewBlankCreatesValidSkeleton(t *testing.T) {
	dir := t.TempDir()
	model, err := newStudioPickerModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("beat")})
	updated, command := updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	created := updated.(studioPickerModel)

	wantPath := filepath.Join(dir, "beat.fnl")
	if command == nil || created.result.path != wantPath || created.result.initialPrompt != "" {
		t.Fatalf("created result/command = %+v/%v, want %s without prompt", created.result, command, wantPath)
	}
	source, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	evaluator := engine.NewEvaluator(engine.SoundInventory{}, 250*time.Millisecond)
	if _, err := evaluator.Evaluate(context.Background(), string(source), 0); err != nil {
		t.Fatalf("blank skeleton evaluation error = %v\n%s", err, source)
	}
}

func TestStudioPicker_NewPromptUsesDescriptionOnce(t *testing.T) {
	dir := t.TempDir()
	model, _ := newStudioPickerModel(dir)
	prompt := "  Create a Dangdut Beat!  "
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(prompt)})
	updated, command := updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	created := updated.(studioPickerModel)

	wantPath := filepath.Join(dir, "create-a-dangdut-beat.fnl")
	if command == nil || created.result.path != wantPath || created.result.initialPrompt != prompt {
		t.Fatalf("new-from-prompt result/command = %+v/%v, want %q submitted for %s", created.result, command, prompt, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("derived pattern file missing: %v", err)
	}
}

func TestStudioPicker_NewPromptUsesUniqueDerivedName(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "create-a-dangdut-beat.fnl")
	if err := os.WriteFile(first, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, _ := newStudioPickerModel(dir)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("create a dangdut beat")})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := updated.(studioPickerModel).result.path, filepath.Join(dir, "create-a-dangdut-beat-2.fnl"); got != want {
		t.Fatalf("unique derived path = %q, want %q", got, want)
	}
	if got, _ := os.ReadFile(first); string(got) != "existing" {
		t.Fatalf("existing derived file changed to %q", got)
	}
}

func TestStudioPicker_NewRejectsExistingAndNestedNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beat.fnl")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"beat.fnl", "nested/beat.fnl"} {
		model, _ := newStudioPickerModel(dir)
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)})
		updated, command := updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
		failed := updated.(studioPickerModel)
		if command != nil || failed.lastError == "" || failed.result.path != "" {
			t.Fatalf("create %q result/error/command = %+v/%q/%v, want inline error", name, failed.result, failed.lastError, command)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Fatalf("existing pattern changed to %q", got)
	}
}
