// basso studio: a full-screen cockpit over playback — live bar/BPM/steps
// status plus the AI candidate review loop. Playback reuses the exact play
// machinery; the UI only observes it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
	"github.com/pmezard/go-difflib/difflib"
)

type barMsg struct {
	bar         int
	bpm         int
	stepsPerBar int
	hits        []engine.Hit
}

type diagnosticMsg struct {
	diagnostic engine.Diagnostic
}

type playbackDoneMsg struct {
	err error
}

type suggestPromptMsg struct{}

type suggestionReadyMsg struct {
	generation int
	candidate  suggest.Candidate
}

type suggestionFailedMsg struct {
	generation int
	err        error
}

type candidateAppliedMsg struct {
	id         string
	backupPath string
}

type applyFailedMsg struct {
	id  string
	err error
}

// studioServices carries the lazily-resolved AI dependencies behind the
// cockpit's suggest action.
type studioServices struct {
	overrides      ai.Overrides
	getenv         func(string) string
	invocationDir  string
	soundsPath     string
	newModel       modelFactory
	newPreflighter suggest.PreflighterFactory
}

func (deps commandDependencies) studioServices(overrides ai.Overrides, soundsPath string) studioServices {
	return studioServices{
		overrides:      overrides,
		getenv:         deps.getenv,
		invocationDir:  deps.invocationDir,
		soundsPath:     soundsPath,
		newModel:       deps.newModel,
		newPreflighter: deps.newPreflighter,
	}
}

type pulseDecayMsg struct{}

type secondTickMsg struct{}

type studioMode int

const (
	studioIdle studioMode = iota
	studioPrompting
	studioRunning
)

type studioModel struct {
	sourceName          string
	bar                 int
	bpm                 int
	stepsPerBar         int
	played              bool
	playbackErr         string
	diagnosticsObserved int
	events              []string

	mode           studioMode
	transport      studioTransportControl
	transportState studioTransportState
	prompt         textinput.Model
	generation     int
	pendingSuggest int
	candidate      *suggest.Candidate
	diff           string
	lastError      string
	cancelSuggest  context.CancelFunc
	sourcePath     string
	services       studioServices
	store          *suggest.Store

	lastHits   []engine.Hit
	pulseLevel float64
	startedAt  time.Time
	elapsed    time.Duration
	spinner    spinner.Model

	height     int
	diffScroll int
}

// maxStudioEvents caps the on-screen event log; older reload diagnostics
// scroll off.
const maxStudioEvents = 8

func newStudioModel(sourceName string) studioModel {
	return studioModel{
		sourceName:     sourceName,
		transportState: transportPlaying,
		startedAt:      time.Now(),
		spinner:        spinner.New(spinner.WithSpinner(spinner.Meter)),
	}
}

func (m studioModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, secondTick())
}

func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return secondTickMsg{} })
}

func pulseDecay() tea.Cmd {
	return tea.Tick(140*time.Millisecond, func(time.Time) tea.Msg { return pulseDecayMsg{} })
}

func (m studioModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case " ":
			if m.mode == studioPrompting {
				break
			}
			if m.transport != nil {
				m.transportState = m.transport.TogglePause()
			}
		case "x":
			if m.mode == studioPrompting {
				break
			}
			if m.transport != nil {
				m.transportState = m.transport.Stop()
			}
		case "p":
			if m.mode == studioPrompting {
				break
			}
			if m.transport != nil {
				m.transportState = m.transport.Play()
			}
		case "s":
			if m.mode != studioIdle {
				break
			}
			input := textinput.New()
			input.Placeholder = "describe the change"
			input.Focus()
			m.prompt = input
			m.mode = studioPrompting
			return m, textinput.Blink
		case "esc":
			switch m.mode {
			case studioPrompting:
				m.mode = studioIdle
				return m, nil
			case studioRunning:
				m.cancelSuggest()
				m.mode = studioIdle
				m.pendingSuggest = 0
				m.events = append(m.events, "suggest cancelled")
				return m, nil
			}
		case "up", "k":
			if m.candidate != nil && m.mode == studioIdle && m.diffScroll > 0 {
				m.diffScroll--
			}
		case "down", "j":
			if m.candidate != nil && m.mode == studioIdle {
				m.diffScroll++
			}
		case "pgup":
			if m.candidate != nil && m.mode == studioIdle {
				m.diffScroll -= m.viewportLines()
			}
		case "pgdown":
			if m.candidate != nil && m.mode == studioIdle {
				m.diffScroll += m.viewportLines()
			}
		case "a":
			if m.candidate == nil || m.mode != studioIdle || m.store == nil {
				break
			}
			id := m.candidate.Metadata.ID
			store, preflighter := m.store, m.services.newPreflighter
			return m, func() tea.Msg {
				result, err := suggest.NewApplier(store, preflighter, nil).Apply(context.Background(), id)
				if err != nil {
					return applyFailedMsg{id: id, err: err}
				}
				return candidateAppliedMsg{id: id, backupPath: result.BackupPath}
			}
		case "r":
			if m.candidate == nil || m.mode != studioIdle {
				break
			}
			m.candidate = nil
			m.diff = ""
			m.events = append(m.events, "candidate rejected")
		case "enter":
			if m.mode == studioPrompting {
				request := strings.TrimSpace(m.prompt.Value())
				if request == "" {
					return m, nil
				}
				m.generation++
				generation := m.generation
				m.pendingSuggest = generation
				ctx, cancel := context.WithCancel(context.Background())
				m.cancelSuggest = cancel
				m.mode = studioRunning
				m.lastError = ""
				base := m.suggestWithCtx(ctx, m.services, m.sourcePath, request)
				return m, func() tea.Msg {
					switch tagged := base().(type) {
					case suggestionReadyMsg:
						tagged.generation = generation
						return tagged
					case suggestionFailedMsg:
						tagged.generation = generation
						return tagged
					default:
						return tagged
					}
				}
			}
		}
		if m.mode == studioPrompting {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(msg)
			return m, cmd
		}
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case suggestPromptMsg:
		if m.mode != studioIdle {
			return m, nil
		}
		input := textinput.New()
		input.Placeholder = "describe the change"
		input.Focus()
		m.prompt = input
		m.mode = studioPrompting
		return m, textinput.Blink
	case barMsg:
		m.played = true
		m.bar = msg.bar
		m.bpm = msg.bpm
		m.stepsPerBar = msg.stepsPerBar
		m.lastHits = msg.hits
		m.pulseLevel = 1.0
		return m, pulseDecay()
	case pulseDecayMsg:
		if m.pulseLevel > 0 {
			m.pulseLevel -= 0.4
			return m, pulseDecay()
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case secondTickMsg:
		m.elapsed = time.Since(m.startedAt).Truncate(time.Second)
		return m, secondTick()
	case diagnosticMsg:
		m.diagnosticsObserved++
		m.events = append(m.events, formatStudioEvent(msg.diagnostic))
		if len(m.events) > maxStudioEvents {
			m.events = m.events[len(m.events)-maxStudioEvents:]
		}
	case playbackDoneMsg:
		if msg.err != nil {
			m.playbackErr = msg.err.Error()
		}
		return m, tea.Quit
	case suggestionReadyMsg:
		if msg.generation == 0 || msg.generation != m.pendingSuggest {
			return m, nil
		}
		m.pendingSuggest = 0
		m.mode = studioIdle
		m.lastError = ""
		candidate := msg.candidate
		if m.store != nil {
			saved, err := m.store.Save(candidate)
			if err != nil {
				m.lastError = "save candidate: " + err.Error()
				return m, nil
			}
			candidate = saved
		}
		m.candidate = &candidate
		m.diff = m.renderDiff()
		m.diffScroll = 0
	case candidateAppliedMsg:
		m.candidate = nil
		m.diff = ""
		m.lastError = ""
		m.events = append(m.events, fmt.Sprintf("applied %s backup %s", shortID(msg.id), msg.backupPath))
	case applyFailedMsg:
		m.lastError = "apply " + shortID(msg.id) + ": " + msg.err.Error()
	case suggestionFailedMsg:
		if msg.generation != m.generation && m.mode != studioRunning {
			return m, nil
		}
		m.mode = studioIdle
		m.lastError = compactDiagnostic(msg.err.Error())
	}
	return m, nil
}

func (m studioModel) View() string {
	var out strings.Builder
	out.WriteString("basso studio — " + m.sourceName + "\n")
	out.WriteString("transport " + m.transportState.String() + "\n")
	if !m.played {
		out.WriteString("waiting for first bar…\n")
	} else {
		out.WriteString(pulseGlyph(m.pulseLevel) + " " +
			fmt.Sprintf("bar %d bpm %d steps %d", m.bar, m.bpm, m.stepsPerBar) + "\n")
		out.WriteString(renderTimeline(m.lastHits, m.stepsPerBar) + "\n")
		if pan := renderPan(m.lastHits); pan != "" {
			out.WriteString(pan + "\n")
		}
	}
	if m.elapsed > 0 || m.played {
		out.WriteString(fmt.Sprintf("up %s\n", m.elapsed))
	}
	if m.playbackErr != "" {
		out.WriteString("playback stopped: " + m.playbackErr + "\n")
	}
	if len(m.events) > 0 {
		out.WriteString("\nevents:\n")
		for _, event := range m.events {
			out.WriteString(event + "\n")
		}
	}
	switch m.mode {
	case studioPrompting:
		out.WriteString("\nsuggest: " + m.prompt.View() + "\nenter send · esc back\n")
	case studioRunning:
		out.WriteString("\nsuggest: " + m.spinner.View() + " consulting provider… (esc cancel)\n")
	}
	if m.lastError != "" {
		out.WriteString("suggest failed: " + m.lastError + "\n")
		out.WriteString("hint: set --provider/--model, BASSO_AI_PROVIDER, BASSO_AI_MODEL, BASSO_AI_BASE_URL, BASSO_AI_API_KEY, or a .env file here\n")
	}
	if m.candidate != nil {
		out.WriteString(fmt.Sprintf("candidate %s: %s [validation %s]\n",
			shortID(m.candidate.Metadata.ID), m.candidate.Metadata.Summary, m.candidate.Metadata.Validation.Status))
		if m.diff != "" {
			out.WriteString("\n" + m.renderDiffView())
		}
	}
	out.WriteString("\nspace pause/resume · p play · x stop · s suggest · a apply · r reject · q quit\n")
	return out.String()
}

// pulseGlyph flashes with each bar boundary and decays until the next one.
func pulseGlyph(level float64) string {
	switch {
	case level >= 1.0:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("●●")
	case level >= 0.6:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("●○")
	case level > 0:
		return lipgloss.NewStyle().Faint(true).Render("○○")
	default:
		return lipgloss.NewStyle().Faint(true).Render("··")
	}
}

// timelineRamp shades a step by its hit velocity.
const timelineRamp = "·▁▂▃▄▅▆▇█"

var timelineColors = map[string]lipgloss.Color{
	"kick":    lipgloss.Color("#ff5555"),
	"snare":   lipgloss.Color("#f1fa8c"),
	"clap":    lipgloss.Color("#ffb86c"),
	"hat":     lipgloss.Color("#8be9fd"),
	"perc":    lipgloss.Color("#ff79c6"),
	"crash":   lipgloss.Color("#bd93f9"),
	"note":    lipgloss.Color("#50fa7b"),
	"unknown": lipgloss.Color("#888785"),
}

// sampleClass buckets a hit's sound for coloring.
func sampleClass(hit engine.Hit) string {
	name := strings.ToLower(hit.Sample)
	switch {
	case hit.Note != "":
		if hit.Instrument != "" {
			return "note"
		}
		return "note"
	case strings.Contains(name, "kick"):
		return "kick"
	case strings.Contains(name, "snare"):
		return "snare"
	case strings.Contains(name, "clap"):
		return "clap"
	case strings.Contains(name, "hat"):
		return "hat"
	case strings.Contains(name, "crash"), strings.Contains(name, "cymbal"), strings.Contains(name, "ride"):
		return "crash"
	case strings.Contains(name, "cowbell"), strings.Contains(name, "conga"),
		strings.Contains(name, "tom"), strings.Contains(name, "clave"), strings.Contains(name, "tamb"):
		return "perc"
	default:
		return "unknown"
	}
}

// renderTimeline draws one labeled row per playing instrument class, each a
// velocity-shaded step strip; empty steps stay faint dots.
func renderTimeline(hits []engine.Hit, steps int) string {
	if steps <= 0 || steps > 256 {
		steps = 16
	}
	type cell struct {
		velocity float64
		class    string
	}
	cells := make([][]cell, steps)
	for _, hit := range hits {
		if hit.Step < 0 || hit.Step >= steps {
			continue
		}
		velocity := hit.Velocity
		if velocity <= 0 {
			velocity = 0.6 // defaulted hits play audibly even when unset upstream
		}
		class := sampleClass(hit)
		for _, existing := range cells[hit.Step] {
			if existing.class == class && existing.velocity >= velocity {
				velocity = 0
				break
			}
		}
		if velocity > 0 {
			cells[hit.Step] = append(cells[hit.Step], cell{velocity: velocity, class: class})
		}
	}

	ramp := []rune(timelineRamp)
	present := map[string]bool{}
	rows := make(map[string]*strings.Builder)
	var order []string
	addClass := func(class string) {
		if !present[class] {
			present[class] = true
			order = append(order, class)
			rows[class] = &strings.Builder{}
		}
	}

	for i := range cells {
		strongest := map[string]float64{}
		for _, c := range cells[i] {
			addClass(c.class)
			if c.velocity > strongest[c.class] {
				strongest[c.class] = c.velocity
			}
		}
		for class := range rows {
			style := lipgloss.NewStyle().Foreground(timelineColors[class])
			if v := strongest[class]; v > 0 {
				idx := int(v * float64(len(ramp)-1))
				if idx < 1 {
					idx = 1
				}
				if idx >= len(ramp) {
					idx = len(ramp) - 1
				}
				rows[class].WriteString(style.Render(string(ramp[idx])))
			} else {
				rows[class].WriteString(lipgloss.NewStyle().Faint(true).Render(string(ramp[0])))
			}
		}
	}

	labelWidth := 6
	var out strings.Builder
	for _, class := range order {
		label := lipgloss.NewStyle().Foreground(timelineColors[class]).Render(fmt.Sprintf("%-*s", labelWidth, class))
		out.WriteString(label + rows[class].String() + "\n")
	}
	if len(order) == 0 {
		return ""
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// panColumns splits the stereo field into buckets for the spread meter.
const panColumns = 9

// renderPan draws a left-to-right density strip of where this bar's hits sit
// in the stereo field; columns accumulate overlapping-hit energy.
func renderPan(hits []engine.Hit) string {
	intensity := make([]float64, panColumns)
	anyHit := false
	for _, hit := range hits {
		pan := hit.Pan
		if pan < -1 {
			pan = -1
		}
		if pan > 1 {
			pan = 1
		}
		velocity := hit.Velocity
		if velocity <= 0 {
			velocity = 0.6
		}
		column := int((pan + 1) / 2 * float64(panColumns-1))
		intensity[column] += velocity
		anyHit = true
	}
	if !anyHit {
		return ""
	}
	ramp := []rune(timelineRamp)
	meter := lipgloss.NewStyle().Faint(true).Render("L")
	for _, level := range intensity {
		idx := int(level * float64(len(ramp)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(ramp) {
			idx = len(ramp) - 1
		}
		if idx == 0 {
			meter += lipgloss.NewStyle().Faint(true).Render("·")
		} else {
			meter += lipgloss.NewStyle().Foreground(timelineColors["hat"]).Render(string(ramp[idx]))
		}
	}
	meter += lipgloss.NewStyle().Faint(true).Render("R")
	return lipgloss.NewStyle().Faint(true).Render("pan   ") + meter
}

// highlightDiff colors unified-diff lines by role: additions green, removals
// red, hunk headers violet.
func highlightDiff(diff string) string {
	addition := lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b"))
	removal := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
	hunk := lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9"))

	lines := strings.Split(diff, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			lines[i] = addition.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = removal.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = hunk.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// renderDiff diffs the on-disk source against the pending candidate.
func (m studioModel) renderDiff() string {
	current, err := os.ReadFile(m.sourcePath)
	if err != nil {
		return "(source unreadable: " + err.Error() + ")"
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(current)),
		B:        difflib.SplitLines(string(m.candidate.Source)),
		FromFile: m.sourcePath,
		ToFile:   "candidate",
		Context:  3,
	})
	if err != nil {
		return "(diff unavailable)"
	}
	return diff
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// newSuggestCmd builds the async request without owning its cancellation;
// Enter-driven submissions wrap it with a per-request context instead.
func (m studioModel) newSuggestCmd(services studioServices, sourcePath, request string) tea.Cmd {
	return func() tea.Msg {
		return runSuggestRequest(context.Background(), services, sourcePath, request)
	}
}

func (m studioModel) suggestWithCtx(
	ctx context.Context,
	services studioServices,
	sourcePath, request string,
) tea.Cmd {
	return func() tea.Msg {
		return runSuggestRequest(ctx, services, sourcePath, request)
	}
}

func runSuggestRequest(
	ctx context.Context,
	services studioServices,
	sourcePath, request string,
) tea.Msg {
	fail := func(err error) tea.Msg { return suggestionFailedMsg{err: err} }

	soundsPath := services.soundsPath
	if !filepath.IsAbs(soundsPath) {
		resolved, err := absoluteFrom(services.invocationDir, soundsPath)
		if err != nil {
			return fail(fmt.Errorf("resolve sounds path: %w", err))
		}
		soundsPath = resolved
	}

	config, err := ai.ResolveConfig(services.overrides, services.getenv)
	if err != nil {
		return fail(err)
	}
	model, err := services.newModel(config)
	if err != nil {
		return fail(fmt.Errorf("construct %s model: %w", config.Provider, err))
	}
	preflighter, err := services.newPreflighter(soundsPath)
	if err != nil {
		return fail(fmt.Errorf("construct preflighter: %w", err))
	}
	inventory, err := engine.LoadSoundInventory(soundsPath)
	if err != nil {
		return fail(err)
	}
	absoluteSource, source, err := loadCommandSource(services.invocationDir, sourcePath)
	if err != nil {
		return fail(err)
	}

	candidate, err := suggest.NewService(model, preflighter).Suggest(ctx, suggest.SuggestInput{
		Provider:    config.Provider,
		Model:       config.Model,
		Prompt:      request,
		SourcePath:  absoluteSource,
		SoundsPath:  soundsPath,
		Source:      source,
		Samples:     sortedSamples(inventory),
		Instruments: []string{"bass", "brass", "pluck"},
	})
	if err != nil {
		return fail(err)
	}
	return suggestionReadyMsg{candidate: candidate}
}

func formatStudioEvent(diagnostic engine.Diagnostic) string {
	revision := diagnostic.RevisionSHA256
	if len(revision) > 8 {
		revision = revision[:8]
	}
	if diagnostic.Bar == nil {
		return fmt.Sprintf("%s %s: %v", revision, diagnostic.Phase, diagnostic.Err)
	}
	return fmt.Sprintf("%s bar %d %s: %v", revision, *diagnostic.Bar, diagnostic.Phase, diagnostic.Err)
}

// studioFlags mirrors suggest's AI flags; config resolves lazily on first
// use so pure playback needs no provider setup.
type studioFlags struct {
	provider string
	model    string
	timeout  string
	sounds   string
	source   string
}

func parseStudioFlags(args []string) (studioFlags, error) {
	flags := flag.NewFlagSet("studio", flag.ContinueOnError)
	var parsed studioFlags
	flags.StringVar(&parsed.provider, "provider", "", "AI provider: openai, ollama, or openai-compatible")
	flags.StringVar(&parsed.model, "model", "", "provider model name")
	flags.StringVar(&parsed.timeout, "timeout", "", "provider request timeout")
	flags.StringVar(&parsed.sounds, "sounds", "sound/808", "sound inventory directory")
	if err := flags.Parse(args); err != nil {
		return parsed, fmt.Errorf("studio flags: %w", err)
	}
	if len(flags.Args()) != 1 {
		return parsed, errors.New("usage: basso studio [flags] <source.fnl>")
	}
	parsed.source = flags.Args()[0]
	return parsed, nil
}

// runStudioCommand plays the source exactly like play, feeding the cockpit
// from the observer seam; quitting the UI stops playback.
func runStudioCommand(ctx context.Context, args []string, deps commandDependencies) error {
	flags, err := parseStudioFlags(args)
	if err != nil {
		return err
	}
	path, err := absoluteFrom(deps.invocationDir, flags.source)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	soundsPath, err := absoluteFrom(deps.invocationDir, flags.sounds)
	if err != nil {
		return fmt.Errorf("resolve sounds path: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	messages := make(chan tea.Msg, 64)
	send := func(message tea.Msg) {
		select {
		case messages <- message:
		case <-sessionCtx.Done():
		}
	}
	observers := playbackObservers{
		onBar: func(bar, bpm, stepsPerBar int, hits []engine.Hit) {
			send(barMsg{bar: bar, bpm: bpm, stepsPerBar: stepsPerBar, hits: hits})
		},
		onDiagnostic: func(diagnostic engine.Diagnostic) {
			send(diagnosticMsg{diagnostic: diagnostic})
		},
	}
	transport, err := newStudioTransport(
		sessionCtx,
		path,
		observers,
		deps.newProvider,
		deps.newSink,
		func(err error) { send(playbackDoneMsg{err: err}) },
	)
	if err != nil {
		cancel()
		return err
	}

	model := newStudioModel(filepath.Base(flags.source))
	model.sourcePath = path
	model.store = suggest.NewStore(filepath.Join(deps.invocationDir, ".basso"), deps.now)
	model.services = deps.studioServices(ai.Overrides{
		Provider: flags.provider,
		Model:    flags.model,
		Timeout:  flags.timeout,
	}, soundsPath)
	model.transport = transport
	model.transportState = transportPlaying
	program := deps.newStudioProgram(model, tea.WithAltScreen())

	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for {
			select {
			case <-sessionCtx.Done():
				return
			case message := <-messages:
				program.Send(message)
			}
		}
	}()
	transport.Start()
	_, programErr := program.Run()
	cancel()
	transportErr := transport.Close()
	<-forwardDone
	return errors.Join(programErr, transportErr)
}

// compactDiagnostic strips fennel/engine stack-trace frames from failure
// text, keeping the semantic lines the player can act on.
func compactDiagnostic(message string) string {
	var kept []string
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" ||
			strings.Contains(trimmed, "stack traceback") ||
			strings.Contains(trimmed, "in function ") ||
			strings.Contains(trimmed, "(tailcall)") ||
			strings.HasPrefix(trimmed, "<string>:") ||
			strings.HasPrefix(trimmed, "[G]:") {
			continue
		}
		kept = append(kept, trimmed)
	}
	out := strings.Join(kept, "\n")
	if len(out) > 400 {
		out = out[:397] + "..."
	}
	return out
}

// reservedDiffLines is the vertical space the rest of the cockpit claims
// before the diff window; the diff gets what remains.
const reservedDiffLines = 12

func (m studioModel) viewportLines() int {
	lines := m.height - reservedDiffLines
	if lines < 6 {
		lines = 6
	}
	if lines > 60 {
		lines = 60
	}
	return lines
}

// renderDiffView windows the candidate diff to a terminal-sized slice with a
// position header, so huge rewrites stay paintable and scrollable.
func (m studioModel) renderDiffView() string {
	lines := strings.Split(m.diff, "\n")
	visible := m.viewportLines()
	maxScroll := len(lines) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	start := m.diffScroll
	if start < 0 {
		start = 0
	}
	if start > maxScroll {
		start = maxScroll
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("diff: lines %d–%d of %d", start+1, end, len(lines)))
	if len(lines) > visible {
		out.WriteString(" (↑↓ scroll)")
	}
	out.WriteString("\n")
	out.WriteString(highlightDiff(strings.Join(lines[start:end], "\n")))
	return out.String()
}
