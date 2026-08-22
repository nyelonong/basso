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
	if command == nil || created.result.path != wantPath || created.result.promptAfterCreate {
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

func TestStudioPicker_NewPromptArmsPromptAfterCreate(t *testing.T) {
	dir := t.TempDir()
	model, _ := newStudioPickerModel(dir)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("generated.fnl")})
	updated, _ = updated.(studioPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	created := updated.(studioPickerModel)

	if !created.result.promptAfterCreate {
		t.Fatal("prompt-after-create = false, want true")
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
