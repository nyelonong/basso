package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
)

// headlessProgram wraps a tea.Program configured for tests: no renderer, no
// signal handling, input piped from a keystroke string. Run() blocks until
// the model quits.
type headlessProgram struct {
	program *tea.Program
}

func (p headlessProgram) Run() (tea.Model, error) {
	return p.program.Run()
}

func (p headlessProgram) Send(msg tea.Msg) {
	p.program.Send(msg)
}

func newHeadlessProgram(keys string) func(tea.Model, ...tea.ProgramOption) programRunner {
	return func(model tea.Model, opts ...tea.ProgramOption) programRunner {
		all := append([]tea.ProgramOption{
			tea.WithInput(strings.NewReader(keys)),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		}, opts...)
		return headlessProgram{program: tea.NewProgram(model, all...)}
	}
}

// TestHelpCommand_ListsStudio verifies the single documented usage line for
// the new subcommand.
func TestHelpCommand_ListsStudio(t *testing.T) {
	var stdout bytes.Buffer
	if err := writeTopLevelHelp(&stdout); err != nil {
		t.Fatalf("writeTopLevelHelp() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "basso studio <source.fnl>") {
		t.Errorf("help = %q, want basso studio usage line", stdout.String())
	}
}

// closingProvider decorates a closablePatternProvider, signalling Close.
type closingProvider struct {
	inner  closablePatternProvider
	closed chan struct{}
}

func (p *closingProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	return p.inner.Next(bar)
}

func (p *closingProvider) Close() error {
	p.closed <- struct{}{}
	return p.inner.Close()
}

// TestRunStudioCommand_PlaysAndQuitsCleanly starts studio headless against a
// stub provider that errors after two bars, feeds it a quit keypress, and
// asserts playback started through the shared sink factory and tore down
// without touching files.
func TestRunStudioCommand_PlaysAndQuitsCleanly(t *testing.T) {
	sinkConstructed := false
	stopErr := errors.New("stop after second bar")
	providerClosed := make(chan struct{}, 1)

	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	deps.newSink = func() engine.AudioSink {
		sinkConstructed = true
		return newFakeSink()
	}
	deps.newProvider = func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
		if !strings.HasSuffix(path, "pattern.fnl") {
			t.Errorf("provider path = %q, want pattern.fnl", path)
		}
		return &closingProvider{inner: &countingProvider{stops: 2, stopErr: stopErr}, closed: providerClosed}, nil
	}
	deps.newStudioProgram = newHeadlessProgram("q")

	err := runStudioCommand(context.Background(), []string{"pattern.fnl"}, deps)
	if err != nil {
		t.Fatalf("runStudioCommand() error = %v", err)
	}
	if !sinkConstructed {
		t.Error("audio sink was never constructed; studio did not play")
	}
	select {
	case <-providerClosed:
	default:
		t.Error("provider was not closed on quit")
	}
}

func TestRunStudioCommand_WiresCandidateStore(t *testing.T) {
	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	deps.newProvider = func(string, engine.DiagnosticReporter) (closablePatternProvider, error) {
		return &countingProvider{stops: 2, stopErr: errors.New("stop")}, nil
	}
	deps.newSink = newFakeSink
	deps.newStudioProgram = func(model tea.Model, options ...tea.ProgramOption) programRunner {
		studio := model.(studioModel)
		if studio.store == nil {
			t.Error("studio candidate store = nil, want production store")
		}
		return newHeadlessProgram("q")(model, options...)
	}

	if err := runStudioCommand(context.Background(), []string{"pattern.fnl"}, deps); err != nil {
		t.Fatalf("runStudioCommand() error = %v", err)
	}
}

// TestRunStudioCommand_RequiresOneFile verifies argument validation matches
// play's contract before any UI or audio is created.
func TestRunStudioCommand_RequiresOneFile(t *testing.T) {
	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	called := false
	deps.newProvider = func(string, engine.DiagnosticReporter) (closablePatternProvider, error) {
		called = true
		return nil, nil
	}

	for _, args := range [][]string{{}, {"a.fnl", "b.fnl"}} {
		if err := runStudioCommand(context.Background(), args, deps); err == nil {
			t.Errorf("runStudioCommand(%v) error = nil, want error", args)
		}
	}
	if called {
		t.Error("provider constructor was called for invalid args")
	}
}

func studioDiagnostic(revision string, bar int, phase engine.DiagnosticPhase, message string) engine.Diagnostic {
	return engine.Diagnostic{
		RevisionSHA256: revision,
		Bar:            &bar,
		Phase:          phase,
		Err:            errors.New(message),
	}
}

// TestStudioModel_BarAdvancesInView proves barMsg drives the status line.
func TestStudioModel_BarAdvancesInView(t *testing.T) {
	var model tea.Model = newStudioModel("basic-groove.fnl")
	if strings.Contains(model.View(), "bar ") {
		t.Fatalf("initial view shows a bar before playback: %q", model.View())
	}

	updated, _ := model.Update(barMsg{bar: 5, bpm: 130, stepsPerBar: 16})
	view := updated.(studioModel).View()
	for _, want := range []string{"bar 5 bpm 130 steps 16", "basic-groove.fnl"} {
		if !strings.Contains(view, want) {
			t.Errorf("view = %q, want substring %q", view, want)
		}
	}
}

// TestStudioModel_DiagnosticsRenderAsCappedEvents proves reload diagnostics
// become an on-screen event log that keeps only the most recent entries.
func TestStudioModel_DiagnosticsRenderAsCappedEvents(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	first := studioDiagnostic(strings.Repeat("a", 64), 1, engine.DiagnosticPhaseValidate, "bad hit")
	second := studioDiagnostic(strings.Repeat("b", 64), 2, engine.DiagnosticPhaseWatch, "stale revision")

	model, _ = model.Update(diagnosticMsg{diagnostic: first})
	model, _ = model.Update(diagnosticMsg{diagnostic: second})
	for i := 0; i < maxStudioEvents; i++ {
		model, _ = model.Update(barMsg{bar: i, bpm: 120, stepsPerBar: 16})
	}

	view := model.(studioModel).View()
	if !strings.Contains(view, "bar 1 validate: bad hit") || !strings.Contains(view, "bar 2 watch: stale revision") {
		t.Errorf("view = %q, want both diagnostic events rendered", view)
	}
	if !strings.Contains(view, strings.Repeat("a", 8)) || !strings.Contains(view, strings.Repeat("b", 8)) {
		t.Errorf("view = %q, want short revision prefixes", view)
	}
	if got := model.(studioModel).diagnosticsObserved; got != 2 {
		t.Errorf("diagnosticsObserved = %d, want 2", got)
	}
}

// TestStudioModel_EventLogKeepsMostRecent proves the cap drops oldest events.
func TestStudioModel_EventLogKeepsMostRecent(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	total := maxStudioEvents + 3
	for i := 0; i < total; i++ {
		model, _ = model.Update(diagnosticMsg{diagnostic: studioDiagnostic(
			strings.Repeat(string(rune('a'+i%26)), 64), i, engine.DiagnosticPhaseWatch, "tick",
		)})
	}

	m := model.(studioModel)
	if len(m.events) != maxStudioEvents {
		t.Fatalf("events = %d, want capped at %d", len(m.events), maxStudioEvents)
	}
	if !strings.Contains(m.View(), "watch: tick") {
		t.Error("view lost the most recent event")
	}
}

// TestStudioModel_QuitAndPlaybackDone proves quit keys and playback failure
// both end the program.
func TestStudioModel_QuitAndPlaybackDone(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")

	q := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	if _, cmd := model.Update(q); cmd == nil {
		t.Error("q key produced no quit command")
	}
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	if _, cmd := model.Update(ctrlC); cmd == nil {
		t.Error("ctrl+c produced no quit command")
	}

	failed, cmd := model.Update(playbackDoneMsg{err: errors.New("device gone")})
	if cmd == nil {
		t.Error("playbackDoneMsg produced no quit command")
	}
	m := failed.(studioModel)
	if !strings.Contains(m.View(), "playback stopped: device gone") {
		t.Errorf("view = %q, want playback failure", m.View())
	}
}

// --- suggest flow fixtures ---

type fakeStudioModel struct {
	proposal suggest.Proposal
	err      error
}

func (m fakeStudioModel) Propose(
	ctx context.Context,
	request suggest.ModelRequest,
) (suggest.Proposal, error) {
	return m.proposal, m.err
}

type passPreflighter struct{}

func (passPreflighter) Preflight(context.Context, string, int, int) error { return nil }

func studioSuggestDeps(t *testing.T, dir string, model suggest.Model) commandDependencies {
	t.Helper()
	deps := testCommandDependencies(dir, io.Discard, io.Discard)
	deps.newModel = func(ai.Config) (suggest.Model, error) { return model, nil }
	deps.newPreflighter = func(string) (suggest.Preflighter, error) { return passPreflighter{}, nil }
	return deps
}

// studioTestServices binds services against the repository's real sample
// inventory so runSuggestRequest exercises its full input path.
func studioTestServices(deps commandDependencies, dir string) studioServices {
	inventory, err := filepath.Abs(filepath.Join("..", "..", "sound", "808"))
	if err != nil {
		panic(err)
	}
	return studioServices{
		getenv:         deps.getenv,
		invocationDir:  dir,
		soundsPath:     inventory,
		newModel:       deps.newModel,
		newPreflighter: deps.newPreflighter,
	}
}

// studioEnvConfig satisfies ai.ResolveConfig without dialing anything.
func studioEnvConfig(getenv func(string) string) func(string) string {
	return func(key string) string {
		switch key {
		case "BASSO_AI_PROVIDER":
			if v := getenv(key); v != "" {
				return v
			}
			return "openai-compatible"
		case "BASSO_AI_MODEL":
			if v := getenv(key); v != "" {
				return v
			}
			return "test-model"
		case "BASSO_AI_BASE_URL":
			if v := getenv(key); v != "" {
				return v
			}
			return "https://gateway.invalid/v1"
		case "BASSO_AI_API_KEY":
			if v := getenv(key); v != "" {
				return v
			}
			return "test-key"
		default:
			return getenv(key)
		}
	}
}

const validProposalSource = "(bpm 120)\n(fn pattern [bar] [])\npattern\n"

// TestStudioModel_SuggestKeyOpensPrompt proves s enters prompting mode and
// escape leaves it without side effects.
func TestStudioModel_SuggestKeyOpensPrompt(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	updated, _ := model.Update(suggestPromptMsg{})
	m := updated.(studioModel)
	if !strings.Contains(m.View(), "describe the change") {
		t.Errorf("view = %q, want prompt", m.View())
	}

	esc := tea.KeyMsg{Type: tea.KeyEsc}
	escaped, _ := m.Update(esc)
	m = escaped.(studioModel)
	if strings.Contains(m.View(), "describe the change") {
		t.Errorf("view = %q, want prompt closed on escape", m.View())
	}
}

// TestStudioModel_SubmitBuildsAsyncCommand proves submitting the prompt
// produces a command whose message carries a validated candidate end to end
// through the shared factories.
func TestStudioModel_SubmitBuildsAsyncCommand(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(source, []byte(validProposalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := studioSuggestDeps(t, dir, fakeStudioModel{
		proposal: suggest.Proposal{Summary: "denser hats", Source: validProposalSource},
	})
	deps.getenv = studioEnvConfig(deps.getenv)

	var model tea.Model = newStudioModel("pattern.fnl")
	cmd := model.(studioModel).newSuggestCmd(studioTestServices(deps, dir), source, "more hats")
	if cmd == nil {
		t.Fatal("submit produced no command")
	}

	msg := cmd()
	ready, ok := msg.(suggestionReadyMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want suggestionReadyMsg", msg)
	}
	if ready.candidate.Metadata.Summary != "denser hats" {
		t.Errorf("summary = %q, want denser hats", ready.candidate.Metadata.Summary)
	}
	if ready.candidate.Metadata.Validation.Status != "passed" {
		t.Errorf("validation = %q, want passed", ready.candidate.Metadata.Validation.Status)
	}
}

// TestStudioModel_MissingConfigIsActionable proves an unconfigured provider
// renders guidance instead of crashing the loop.
func TestStudioModel_MissingConfigIsActionable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(source, []byte(validProposalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := studioSuggestDeps(t, dir, fakeStudioModel{})
	deps.getenv = func(string) string { return "" }

	var model tea.Model = newStudioModel("pattern.fnl")
	model, _ = model.Update(barMsg{bar: 3, bpm: 130, stepsPerBar: 16})
	cmd := model.(studioModel).newSuggestCmd(studioTestServices(deps, dir), source, "x")

	msg := cmd()
	failed, ok := msg.(suggestionFailedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want suggestionFailedMsg", msg)
	}
	updated, _ := model.Update(failed)
	m := updated.(studioModel)
	view := m.View()
	for _, want := range []string{"provider is required", "BASSO_AI_PROVIDER"} {
		if !strings.Contains(view, want) {
			t.Errorf("view = %q, want %q", view, want)
		}
	}
	if !strings.Contains(view, "bar 3 bpm 130") {
		t.Errorf("view = %q, want status line preserved through failure", view)
	}
}

// TestStudioModel_ProviderErrorSurfaces proves transport/model failures reach
// the UI verbatim enough to act on.
func TestStudioModel_ProviderErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(source, []byte(validProposalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := studioSuggestDeps(t, dir, fakeStudioModel{
		err: errors.New("openai-compatible: unexpected HTTP status 503"),
	})
	deps.getenv = func(key string) string {
		if key == "BASSO_AI_PROVIDER" {
			return "openai-compatible"
		}
		if key == "BASSO_AI_MODEL" {
			return "ox-alpha-free"
		}
		if key == "BASSO_AI_BASE_URL" {
			return "https://gateway.invalid/v1"
		}
		if key == "BASSO_AI_API_KEY" {
			return "k"
		}
		return ""
	}

	var model tea.Model = newStudioModel("pattern.fnl")
	cmd := model.(studioModel).newSuggestCmd(studioTestServices(deps, dir), source, "x")
	failed, ok := cmd().(suggestionFailedMsg)
	if !ok {
		t.Fatal("want suggestionFailedMsg")
	}
	updated, _ := model.Update(failed)
	m := updated.(studioModel)
	if !strings.Contains(m.View(), "HTTP status 503") {
		t.Errorf("view = %q, want upstream status", m.View())
	}
}

// TestStudioModel_CancelledContextYieldsFailure proves an aborted request
// ends as a normal failure message with no candidate.
func TestStudioModel_CancelledContextYieldsFailure(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(source, []byte(validProposalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := studioSuggestDeps(t, dir, fakeStudioModel{
		proposal: suggest.Proposal{Summary: "late", Source: validProposalSource},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var model tea.Model = newStudioModel("pattern.fnl")
	cmd := model.(studioModel).suggestWithCtx(ctx, studioTestServices(deps, dir), source, "x")
	if _, ok := cmd().(suggestionFailedMsg); !ok {
		t.Fatal("cancelled request did not yield suggestionFailedMsg")
	}
}

// --- candidate review fixtures ---

const modifiedProposalSource = "(bpm 140)\n(fn pattern [bar] [])\npattern\n"

// readyCandidateModel drives a real end-to-end suggest through the model
// (fake provider, real service, real temp-dir store) and returns the model
// holding a saved candidate plus the source path.
func readyCandidateModel(t *testing.T, dir string) (studioModel, string) {
	t.Helper()
	return readyCandidateModelWithTransport(t, dir, nil)
}

func readyCandidateModelWithTransport(
	t *testing.T,
	dir string,
	transport studioTransportControl,
) (studioModel, string) {
	t.Helper()
	source := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(source, []byte(validProposalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := studioSuggestDeps(t, dir, fakeStudioModel{
		proposal: suggest.Proposal{Summary: "denser hats", Source: modifiedProposalSource},
	})
	deps.getenv = studioEnvConfig(deps.getenv)

	m := newStudioModel("pattern.fnl")
	m.sourcePath = source
	m.transport = transport
	m.services = studioTestServices(deps, dir)
	m.store = suggest.NewStore(filepath.Join(dir, ".basso"), deps.now)

	var model tea.Model = m
	model, _ = model.Update(barMsg{bar: 3, bpm: 130, stepsPerBar: 16})
	model, _ = model.Update(suggestPromptMsg{})
	m = model.(studioModel)
	m.prompt.SetValue("more hats")
	model = m
	model, submitCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submitCmd == nil {
		t.Fatal("submit produced no command")
	}
	ready, ok := submitCmd().(suggestionReadyMsg)
	if !ok {
		t.Fatalf("submit cmd = %T", ready)
	}
	model, _ = model.Update(ready)
	return model.(studioModel), source
}

func TestStudioModel_CandidateArrivalArmsStoredSource(t *testing.T) {
	dir := t.TempDir()
	control := &recordingTransport{state: transportPlaying}
	m, _ := readyCandidateModelWithTransport(t, dir, control)

	want := filepath.Join(dir, ".basso", "candidates", m.candidate.Metadata.ID+".fnl")
	if control.armedPath != want {
		t.Fatalf("armed candidate path = %q, want %q", control.armedPath, want)
	}

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if command != nil || updated.(studioModel).mode != studioIdle {
		t.Fatal("s opened a second suggestion while a candidate was armed")
	}
	if view := updated.(studioModel).View(); !strings.Contains(view, "accept or reject the current candidate") {
		t.Fatalf("View() = %q, want armed-candidate reason", view)
	}
}

// TestStudioModel_CandidateRendersDiffAndStatus proves a returned candidate
// shows its summary, validation badge, and a unified diff against the file,
// and is persisted to the store on arrival.
func TestStudioModel_CandidateRendersDiffAndStatus(t *testing.T) {
	dir := t.TempDir()
	m, _ := readyCandidateModel(t, dir)

	view := m.View()
	for _, want := range []string{"denser hats", "[validation passed]", "+(bpm 140)", "-(bpm 120)", "diff:"} {
		if !strings.Contains(view, want) {
			t.Errorf("view = %q, want %q", view, want)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ".basso", "candidates"))
	sources := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".fnl") {
			sources++
		}
	}
	if sources != 1 {
		t.Errorf("saved candidate sources = %d, want 1", sources)
	}
}

// TestStudioModel_RejectClearsWithoutWrites proves r drops the candidate and
// never touches the source file.
func TestStudioModel_RejectClearsWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	m, source := readyCandidateModel(t, dir)
	before, _ := os.ReadFile(source)

	r := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	rejected, cmd := m.Update(r)
	m2 := rejected.(studioModel)

	if cmd != nil {
		t.Error("reject produced an unexpected command")
	}
	if m2.candidate != nil || strings.Contains(m2.View(), "denser hats") {
		t.Errorf("candidate survived reject: view=%q", m2.View())
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(before, after) {
		t.Error("reject modified the source file")
	}
}

// TestStudioModel_ApplyGoesThroughTransactionalApplier proves a writes the
// candidate via the applier: source replaced, backup created, event logged.
func TestStudioModel_ApplyGoesThroughTransactionalApplier(t *testing.T) {
	dir := t.TempDir()
	m, source := readyCandidateModel(t, dir)

	a := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	withCmd, cmd := m.Update(a)
	if cmd == nil {
		t.Fatal("apply key produced no command")
	}
	msg := cmd()
	applied, ok := msg.(candidateAppliedMsg)
	if !ok {
		t.Fatalf("apply cmd = %T, want candidateAppliedMsg", msg)
	}
	withCmd, _ = withCmd.Update(applied)

	got, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != modifiedProposalSource {
		t.Errorf("source = %q, want applied candidate content", got)
	}
	if applied.backupPath == "" {
		t.Error("applied result missing backup path")
	}
	final := withCmd.(studioModel)
	if !strings.Contains(final.View(), "applied "+applied.id[:12]) {
		t.Errorf("view = %q, want applied event", final.View())
	}
	if final.candidate != nil {
		t.Error("candidate stayed armed after apply")
	}
}

// TestStudioModel_ApplyFailureSurfaces proves a failed transactional apply
// renders diagnostics and leaves the source untouched.
func TestStudioModel_ApplyFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	m, source := readyCandidateModel(t, dir)

	// Mutate the file so the base-hash check fails inside the applier.
	if err := os.WriteFile(source, append([]byte(validProposalSource), []byte("\n;; drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	a := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, cmd := m.Update(a)
	failed, ok := cmd().(applyFailedMsg)
	if !ok {
		t.Fatalf("apply cmd = %T, want applyFailedMsg", cmd())
	}
	if !strings.Contains(failed.err.Error(), "hash") {
		t.Errorf("err = %v, want hash mismatch", failed.err)
	}
	got, _ := os.ReadFile(source)
	if !bytes.Contains(got, []byte("drift")) {
		t.Error("failed apply still modified the source")
	}
}

// TestStudioModel_TimelineRendersHits proves barMsg hits become a velocity
// shaded step strip: full-velocity kick shows the top ramp rune.
func TestStudioModel_TimelineRendersHits(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	hits := []engine.Hit{
		{Step: 0, Sample: "kick2.wav", Velocity: 1.0},
		{Step: 4, Sample: "snare.wav", Velocity: 0.5},
	}
	model, _ = model.Update(barMsg{bar: 2, bpm: 130, stepsPerBar: 16, hits: hits})
	view := model.(studioModel).View()
	if !strings.Contains(view, "█") {
		t.Errorf("view = %q, want full-velocity cell", view)
	}
	if !strings.Contains(view, "▄") {
		t.Errorf("view = %q, want mid-velocity cell", view)
	}
}

// TestStudioModel_PulseDecaysAfterBar proves the beat pulse flashes at the
// boundary and decays toward idle without retriggering forever.
func TestStudioModel_PulseDecaysAfterBar(t *testing.T) {
	m := newStudioModel("pattern.fnl")
	model, cmd := m.Update(barMsg{bar: 0, bpm: 130, stepsPerBar: 16})
	live := model.(studioModel)
	if live.pulseLevel != 1.0 || cmd == nil {
		t.Fatalf("pulseLevel=%v cmd=%v, want flash with decay scheduled", live.pulseLevel, cmd)
	}
	for i := 0; i < 3; i++ {
		model, _ = model.Update(pulseDecayMsg{})
	}
	if got := model.(studioModel).pulseLevel; got > 0 {
		t.Errorf("pulseLevel = %v, want decayed to zero", got)
	}
}

// TestStudioModel_TimelineRowsPerInstrument proves the strip becomes labeled
// per-instrument rows, one per class actually playing.
func TestStudioModel_TimelineRowsPerInstrument(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	hits := []engine.Hit{
		{Step: 0, Sample: "kick2.wav", Velocity: 1.0},
		{Step: 4, Sample: "snare.wav", Velocity: 0.5},
	}
	model, _ = model.Update(barMsg{bar: 2, bpm: 130, stepsPerBar: 16, hits: hits})
	view := model.(studioModel).View()
	for _, want := range []string{"kick", "snare"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q row label:\n%q", want, view)
		}
	}
	if !strings.Contains(view, "█") || !strings.Contains(view, "▄") {
		t.Errorf("view lost velocity shading:\n%q", view)
	}
}

// TestStudioModel_PanSpreadMeter proves panned hits light up a left-to-right
// density strip.
func TestStudioModel_PanSpreadMeter(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	hits := []engine.Hit{
		{Step: 0, Sample: "conga1.wav", Pan: -1.0, Velocity: 1.0},
		{Step: 2, Sample: "cowbell.wav", Pan: 1.0, Velocity: 1.0},
	}
	model, _ = model.Update(barMsg{bar: 1, bpm: 130, stepsPerBar: 16, hits: hits})
	view := model.(studioModel).View()
	if !strings.Contains(view, "L") || !strings.Contains(view, "R") {
		t.Errorf("view = %q, want pan meter rails", view)
	}
	if !strings.Contains(view, "█") { // hard-left and hard-right both peak
		t.Errorf("view = %q, want lit pan extremes", view)
	}
}

// TestHighlightDiff proves diff lines get class-based coloring while keeping
// every character intact.
func TestHighlightDiff(t *testing.T) {
	originalProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(originalProfile)

	highlighted := highlightDiff("+added\nremoved\n@@ -1 +1 @@\n context\n")
	if !strings.Contains(highlighted, "+added") || !strings.Contains(highlighted, "removed") ||
		!strings.Contains(highlighted, "@@ -1 +1 @@") || !strings.Contains(highlighted, "context") {
		t.Errorf("highlightDiff lost content: %q", highlighted)
	}
	if !strings.Contains(highlighted, "\x1b[") {
		t.Error("highlightDiff produced no color codes")
	}
}

// TestRunStudioCommand_ParsesFlagsBeforeFile proves flag arguments no longer
// trip play's positional parsing.
func TestRunStudioCommand_ParsesFlagsBeforeFile(t *testing.T) {
	var gotPath string
	stopErr := errors.New("stop")
	providerClosed := make(chan struct{}, 1)
	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	deps.newProvider = func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
		gotPath = path
		return &closingProvider{inner: &stubProvider{err: stopErr}, closed: providerClosed}, nil
	}
	deps.newSink = newFakeSink
	deps.newStudioProgram = newHeadlessProgram("q")

	err := runStudioCommand(context.Background(),
		[]string{"--timeout", "45s", "--model", "ox-alpha-free", "pattern.fnl"}, deps)
	// Flag parsing must succeed: the stub stops playback immediately, so the
	// only acceptable error is its stop signal — never a usage error.
	if err != nil && !errors.Is(err, stopErr) {
		t.Fatalf("runStudioCommand() error = %v, want none or stop", err)
	}
	if !strings.HasSuffix(gotPath, "pattern.fnl") {
		t.Errorf("provider path = %q, want pattern.fnl", gotPath)
	}
}

// TestCompactDiagnostic_DropsTracebackNoise proves engine/fennel stack traces
// collapse to their semantic lines while preserving both failure phases.
func TestCompactDiagnostic_DropsTracebackNoise(t *testing.T) {
	raw := "first local preflight: fennel: compile bar 0: compile source: unknown:7:0: Compile error: local length was overshadowed\n* Try renaming local length.\nstack traceback:\n    [G]: in function 'error'\n    <string>:4285: in function 'assert-compile'\n    (tailcall): ?\n    [G]: ?\nrepaired local preflight: fennel: validate bar 0: engine: hit 39 note \"40\" is invalid"

	got := compactDiagnostic(raw)
	for _, want := range []string{
		"local length was overshadowed",
		"* Try renaming local length.",
		`note "40" is invalid`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compactDiagnostic = %q, want %q kept", got, want)
		}
	}
	if strings.Contains(got, "traceback") || strings.Contains(got, "assert-compile") || strings.Contains(got, "[G]") {
		t.Errorf("compactDiagnostic kept traceback noise: %q", got)
	}

	single := compactDiagnostic("openai-compatible: unexpected HTTP status 503")
	if single != "openai-compatible: unexpected HTTP status 503" {
		t.Errorf("compactDiagnostic changed a clean message: %q", single)
	}
}

// TestStudioModel_DiffViewportWindows proves a diff taller than the terminal
// renders as a bounded, scrollable window instead of an unpaintable wall.
func TestStudioModel_DiffViewportWindows(t *testing.T) {
	m := newStudioModel("pattern.fnl")
	m.height = 24 // ~10 diff lines visible
	long := ""
	for i := 0; i < 100; i++ {
		long += fmt.Sprintf("(hit %03d \"kick.wav\" 0.5 0.0)\n", i)
	}
	m.diff = long

	view := m.renderDiffView()
	if got := strings.Count(view, "\n"); got > 14 {
		t.Errorf("diff view rendered %d lines, want windowed", got)
	}
	if !strings.Contains(view, "of 101") {
		t.Errorf("view = %q, want total-line indicator", view)
	}

	scrolled := m
	scrolled.candidate = &suggest.Candidate{Metadata: suggest.Metadata{ID: "test"}}
	scrolled.diffScroll = 40
	view = scrolled.renderDiffView()
	if !strings.Contains(view, "(hit 040") || !strings.Contains(view, "(hit 051") {
		t.Errorf("scrolled view lost offset content: %q", view[:200])
	}

	down := tea.KeyMsg{Type: tea.KeyDown}
	model, _ := scrolled.Update(down)
	if model.(studioModel).diffScroll != 41 {
		t.Errorf("down key did not scroll: %d", model.(studioModel).diffScroll)
	}
}
