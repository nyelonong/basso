package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const blankPatternSource = `(fn pattern [bar]
  [])

pattern
`

type studioPickerMode int

const (
	pickerBrowsing studioPickerMode = iota
	pickerNaming
)

type studioPickerResult struct {
	path          string
	initialPrompt string
	quit          bool
}

type studioPickerModel struct {
	dir        string
	files      []string
	cursor     int
	mode       studioPickerMode
	name       textinput.Model
	fromPrompt bool
	lastError  string
	result     studioPickerResult
}

func newStudioPickerModel(dir string) (studioPickerModel, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return studioPickerModel{}, fmt.Errorf("list studio patterns: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".fnl") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return studioPickerModel{}, fmt.Errorf("inspect studio pattern %s: %w", entry.Name(), err)
		}
		if info.Mode().IsRegular() {
			files = append(files, entry.Name())
		}
	}
	return studioPickerModel{dir: filepath.Clean(dir), files: files}, nil
}

func (model studioPickerModel) Init() tea.Cmd { return nil }

func (model studioPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return model, nil
	}
	if model.mode == pickerNaming {
		switch key.String() {
		case "esc":
			model.mode = pickerBrowsing
			model.lastError = ""
			return model, nil
		case "enter":
			var path, prompt string
			var err error
			if model.fromPrompt {
				prompt = model.name.Value()
				path, err = createStudioPatternFromPrompt(model.dir, prompt)
			} else {
				path, err = createStudioPattern(model.dir, model.name.Value())
			}
			if err != nil {
				model.lastError = err.Error()
				return model, nil
			}
			model.result = studioPickerResult{path: path, initialPrompt: prompt}
			return model, tea.Quit
		}
		var command tea.Cmd
		model.name, command = model.name.Update(message)
		return model, command
	}

	switch key.String() {
	case "q", "ctrl+c", "esc":
		model.result.quit = true
		return model, tea.Quit
	case "up", "k":
		if model.cursor > 0 {
			model.cursor--
		}
	case "down", "j":
		if model.cursor+1 < len(model.files) {
			model.cursor++
		}
	case "enter":
		if len(model.files) > 0 {
			model.result.path = filepath.Join(model.dir, model.files[model.cursor])
			return model, tea.Quit
		}
	case "n", "N":
		input := textinput.New()
		model.fromPrompt = key.String() == "N"
		if model.fromPrompt {
			input.Placeholder = "describe the music"
		} else {
			input.Placeholder = "pattern.fnl"
		}
		input.Focus()
		model.name = input
		model.mode = pickerNaming
		model.lastError = ""
		return model, textinput.Blink
	}
	return model, nil
}

func (model studioPickerModel) View() string {
	var out strings.Builder
	out.WriteString("basso studio — choose pattern\n\n")
	if model.mode == pickerNaming {
		if model.fromPrompt {
			out.WriteString("new pattern from prompt: ")
		} else {
			out.WriteString("new blank pattern: ")
		}
		out.WriteString(model.name.View() + "\n")
		if model.lastError != "" {
			out.WriteString("error: " + model.lastError + "\n")
		}
		out.WriteString("enter create · esc back\n")
		return out.String()
	}

	if len(model.files) == 0 {
		out.WriteString("(no .fnl files)\n")
	}
	for index, name := range model.files {
		marker := "  "
		if index == model.cursor {
			marker = "> "
		}
		out.WriteString(marker + name + "\n")
	}
	out.WriteString("\nenter open · n new blank · N new from prompt · q quit\n")
	return out.String()
}

func createStudioPattern(dir, requestedName string) (string, error) {
	name := strings.TrimSpace(requestedName)
	if name == "" {
		return "", errors.New("pattern name is required")
	}
	if strings.ContainsAny(name, `/\\`) || filepath.Base(name) != name {
		return "", errors.New("pattern name must not contain a directory")
	}
	if !strings.HasSuffix(name, ".fnl") {
		name += ".fnl"
	}
	if name == ".fnl" {
		return "", errors.New("pattern name is required")
	}
	path := filepath.Join(dir, name)
	if err := writeBlankPattern(path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("pattern %s already exists", name)
		}
		return "", err
	}
	return path, nil
}

func createStudioPatternFromPrompt(dir, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("music description is required")
	}
	stem := patternPromptStem(prompt)
	for number := 1; ; number++ {
		name := stem + ".fnl"
		if number > 1 {
			name = fmt.Sprintf("%s-%d.fnl", stem, number)
		}
		path := filepath.Join(dir, name)
		err := writeBlankPattern(path)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
}

func patternPromptStem(prompt string) string {
	var stem strings.Builder
	separator := false
	letters := 0
	for _, value := range strings.ToLower(prompt) {
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			if separator && stem.Len() > 0 {
				stem.WriteByte('-')
			}
			stem.WriteRune(value)
			separator = false
			letters++
			if letters == 48 {
				break
			}
		} else {
			separator = true
		}
	}
	if stem.Len() == 0 {
		return "new-pattern"
	}
	return stem.String()
}

func writeBlankPattern(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create pattern: %w", err)
	}
	if _, err := io.WriteString(file, blankPatternSource); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write pattern: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close pattern: %w", err)
	}
	return nil
}
