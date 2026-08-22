package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
)

const (
	aiWorkflowTimeout     = 5 * time.Second
	aiWorkflowBarDuration = time.Minute / (400 * 4)
)

var aiWorkflowNow = time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC)

type aiWorkflowFixture struct {
	root       string
	soundsPath string
	sourcePath string
	storeRoot  string
	original   []byte
}

type aiWorkflowModel struct {
	mu        sync.Mutex
	proposals []suggest.Proposal
	calls     int
}

func (model *aiWorkflowModel) Propose(
	_ context.Context,
	_ suggest.ModelRequest,
) (suggest.Proposal, error) {
	model.mu.Lock()
	defer model.mu.Unlock()

	model.calls++
	index := model.calls - 1
	if index >= len(model.proposals) {
		return suggest.Proposal{}, fmt.Errorf("unexpected model call %d", model.calls)
	}
	return model.proposals[index], nil
}

func (model *aiWorkflowModel) callCount() int {
	model.mu.Lock()
	defer model.mu.Unlock()
	return model.calls
}

type aiWorkflowFire struct {
	source string
	begin  time.Duration
}

type aiWorkflowSink struct {
	mu            sync.Mutex
	fires         []aiWorkflowFire
	startCalls    int
	teardownCalls int
	events        chan aiWorkflowFire
}

func newAIWorkflowSink() *aiWorkflowSink {
	return &aiWorkflowSink{events: make(chan aiWorkflowFire, 4096)}
}

func (sink *aiWorkflowSink) Start() {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.startCalls++
}

func (sink *aiWorkflowSink) SetFire(
	source string,
	begin time.Duration,
	_ time.Duration,
	_ float64,
	_ float64,
) {
	fire := aiWorkflowFire{source: source, begin: begin}
	sink.mu.Lock()
	sink.fires = append(sink.fires, fire)
	sink.mu.Unlock()

	select {
	case sink.events <- fire:
	default:
	}
}

func (sink *aiWorkflowSink) SetFireNote(
	_ string,
	_ string,
	_ time.Duration,
	_ time.Duration,
	_ float64,
	_ float64,
) {
}

func (sink *aiWorkflowSink) Teardown() {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.teardownCalls++
}

func (sink *aiWorkflowSink) lifecycle() (int, int) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.startCalls, sink.teardownCalls
}

type aiWorkflowEngineSession struct {
	cancel   context.CancelFunc
	done     <-chan error
	provider *engine.FennelProvider
	once     sync.Once
	runErr   error
	closeErr error
}

func (session *aiWorkflowEngineSession) stop() (error, error) {
	session.once.Do(func() {
		session.cancel()
		select {
		case session.runErr = <-session.done:
		case <-time.After(aiWorkflowTimeout):
			session.runErr = errors.New("engine did not stop before deadline")
		}
		session.closeErr = session.provider.Close()
	})
	return session.runErr, session.closeErr
}

func TestAIWorkflow_SuggestApplyAndActivateAtNextBar(t *testing.T) {
	fixture := newAIWorkflowFixture(t)
	candidateSource := aiWorkflowSource("b.wav", 1)
	model := &aiWorkflowModel{
		proposals: []suggest.Proposal{{
			Summary: "replace a with b",
			Source:  string(candidateSource),
		}},
	}
	sink, session := startAIWorkflowEngine(t, fixture, nil)
	initial := waitForAIWorkflowFire(t, sink.events, "a.wav", -1)
	assertAIWorkflowLifecycle(t, sink, 1, 0)

	var stdout bytes.Buffer
	deps := aiWorkflowCommandDependencies(fixture, model, &stdout)
	if err := runCommand(
		context.Background(),
		aiWorkflowSuggestArgs(fixture, "replace a with b"),
		deps,
	); err != nil {
		t.Fatalf("suggest runCommand() error = %v", err)
	}

	if model.callCount() != 1 {
		t.Errorf("model calls = %d, want 1", model.callCount())
	}
	if got := readAIWorkflowFile(t, fixture.sourcePath); !bytes.Equal(got, fixture.original) {
		t.Errorf("source changed during suggest: got %q, want %q", got, fixture.original)
	}
	candidateID, candidatePath, metadataPath := requireAIWorkflowCandidatePair(t, fixture.storeRoot)
	saved, err := suggest.NewStore(fixture.storeRoot, nil).Load(candidateID)
	if err != nil {
		t.Fatalf("load real candidate pair: %v", err)
	}
	if !bytes.Equal(saved.Source, candidateSource) {
		t.Errorf("candidate source = %q, want %q", saved.Source, candidateSource)
	}
	if saved.Metadata.BaseSHA256 != aiWorkflowSHA256(fixture.original) {
		t.Errorf("candidate base hash = %q, want original hash", saved.Metadata.BaseSHA256)
	}
	if saved.Metadata.CandidateSHA256 != aiWorkflowSHA256(candidateSource) {
		t.Errorf("candidate hash = %q, want candidate source hash", saved.Metadata.CandidateSHA256)
	}
	if saved.Metadata.Validation.FirstBar != 0 ||
		saved.Metadata.Validation.LastBar != 15 ||
		saved.Metadata.Validation.Status != "passed" {
		t.Errorf("candidate validation = %+v, want passed bars 0..15", saved.Metadata.Validation)
	}
	suggestOutput := stdout.String()
	for _, want := range []string{
		"candidate ID: " + candidateID,
		"summary: replace a with b",
		"validation: passed",
		"candidate path: " + candidatePath,
		"--- " + fixture.sourcePath,
		"+++ " + candidatePath,
		`-  [{:step 0 :sample "a.wav"`,
		`+  [{:step 0 :sample "b.wav"`,
	} {
		if !strings.Contains(suggestOutput, want) {
			t.Errorf("suggest stdout = %q, want substring %q", suggestOutput, want)
		}
	}
	if info, err := os.Stat(metadataPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("candidate metadata is not a regular file: info=%v err=%v", info, err)
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"apply", candidateID}, deps); err != nil {
		t.Fatalf("apply runCommand() error = %v", err)
	}
	if got := readAIWorkflowFile(t, fixture.sourcePath); !bytes.Equal(got, candidateSource) {
		t.Errorf("applied source = %q, want candidate %q", got, candidateSource)
	}
	backupPath := requireSingleAIWorkflowBackup(t, fixture.storeRoot)
	if got := readAIWorkflowFile(t, backupPath); !bytes.Equal(got, fixture.original) {
		t.Errorf("backup = %q, want exact original %q", got, fixture.original)
	}
	applyOutput := stdout.String()
	for _, want := range []string{
		"source path: " + fixture.sourcePath,
		"backup path: " + backupPath,
	} {
		if !strings.Contains(applyOutput, want) {
			t.Errorf("apply stdout = %q, want substring %q", applyOutput, want)
		}
	}

	activated := waitForAIWorkflowFire(t, sink.events, "b.wav", initial.begin)
	if activated.begin <= initial.begin {
		t.Errorf("b.wav began at %v, want later than initial bar at %v", activated.begin, initial.begin)
	}
	assertAIWorkflowLifecycle(t, sink, 1, 0)

	runErr, closeErr := session.stop()
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("engine Run() error = %v, want context.Canceled", runErr)
	}
	if closeErr != nil {
		t.Errorf("provider Close() error = %v", closeErr)
	}
	assertAIWorkflowLifecycle(t, sink, 1, 1)
}

func TestAIWorkflow_InvalidGeneratedRevisionNeverInterruptsActiveAudio(t *testing.T) {
	fixture := newAIWorkflowFixture(t)
	diagnostics := make(chan engine.Diagnostic, 16)
	sink, session := startAIWorkflowEngine(t, fixture, func(diagnostic engine.Diagnostic) {
		select {
		case diagnostics <- diagnostic:
		default:
		}
	})
	initial := waitForAIWorkflowFire(t, sink.events, "a.wav", -1)
	assertAIWorkflowLifecycle(t, sink, 1, 0)

	invalidInitial := "(fn pattern [bar]"
	invalidRepair := string(aiWorkflowPaletteOverlap(9))
	model := &aiWorkflowModel{
		proposals: []suggest.Proposal{
			{Summary: "broken initial", Source: invalidInitial},
			{Summary: "broken repair", Source: invalidRepair},
			{Summary: "still broken repair", Source: invalidRepair},
		},
	}
	deps := aiWorkflowCommandDependencies(fixture, model, io.Discard)
	err := runCommand(
		context.Background(),
		aiWorkflowSuggestArgs(fixture, "make an invalid change"),
		deps,
	)
	if err == nil {
		t.Fatal("suggest runCommand() error = nil, want both invalid proposals rejected")
	}
	if model.callCount() != 3 {
		t.Errorf("model calls = %d, want exactly 3 (initial + repair budget)", model.callCount())
	}
	for _, want := range []string{
		"first local preflight",
		"compile bar 0",
		"repaired local preflight",
		"lead/pad overlap 9 at step 0 exceeds 8 voices",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("suggest error = %q, want substring %q", err, want)
		}
	}
	if got := readAIWorkflowFile(t, fixture.sourcePath); !bytes.Equal(got, fixture.original) {
		t.Errorf("source changed after invalid generation: got %q, want %q", got, fixture.original)
	}
	if artifacts := globAIWorkflowFiles(t, filepath.Join(fixture.storeRoot, "candidates", "*")); len(artifacts) != 0 {
		t.Errorf("candidate artifacts = %v, want none", artifacts)
	}
	assertAIWorkflowLifecycle(t, sink, 1, 0)

	if err := os.WriteFile(fixture.sourcePath, []byte(invalidRepair), 0o600); err != nil {
		t.Fatalf("write invalid live revision: %v", err)
	}
	diagnostic := waitForAIWorkflowDiagnostic(t, diagnostics)
	if diagnostic.Bar == nil {
		t.Fatal("live diagnostic bar = nil, want rejected bar")
	}
	if diagnostic.Phase != engine.DiagnosticPhaseValidate {
		t.Errorf("live diagnostic phase = %q, want %q", diagnostic.Phase, engine.DiagnosticPhaseValidate)
	}
	if diagnostic.Err == nil ||
		!strings.Contains(diagnostic.Err.Error(), "lead/pad overlap 9 at step 0 exceeds 8 voices") {
		t.Errorf("live diagnostic error = %v, want palette overlap validation", diagnostic.Err)
	}
	invalidHash := aiWorkflowSHA256([]byte(invalidRepair))
	if diagnostic.RevisionSHA256 != invalidHash {
		t.Errorf("live diagnostic revision = %q, want %q", diagnostic.RevisionSHA256, invalidHash)
	}

	rejectedBarStart := time.Duration(*diagnostic.Bar) * aiWorkflowBarDuration
	fallback := waitForAIWorkflowFire(t, sink.events, "a.wav", rejectedBarStart-time.Nanosecond)
	if fallback.begin < rejectedBarStart {
		t.Errorf("fallback begin = %v, want rejected bar start at least %v", fallback.begin, rejectedBarStart)
	}
	if fallback.begin <= initial.begin {
		t.Errorf("fallback begin = %v, want later than initial %v", fallback.begin, initial.begin)
	}
	assertAIWorkflowLifecycle(t, sink, 1, 0)

	runErr, closeErr := session.stop()
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("engine Run() error = %v, want context.Canceled", runErr)
	}
	if closeErr != nil {
		t.Errorf("provider Close() error = %v", closeErr)
	}
	assertAIWorkflowLifecycle(t, sink, 1, 1)
}

func TestAIWorkflow_StaleCandidateCannotOverwriteManualEdit(t *testing.T) {
	fixture := newAIWorkflowFixture(t)
	candidateSource := aiWorkflowSource("b.wav", 1)
	model := &aiWorkflowModel{
		proposals: []suggest.Proposal{{
			Summary: "replace a with b",
			Source:  string(candidateSource),
		}},
	}
	deps := aiWorkflowCommandDependencies(fixture, model, io.Discard)
	if err := runCommand(
		context.Background(),
		aiWorkflowSuggestArgs(fixture, "replace a with b"),
		deps,
	); err != nil {
		t.Fatalf("suggest runCommand() error = %v", err)
	}
	candidateID, candidatePath, _ := requireAIWorkflowCandidatePair(t, fixture.storeRoot)
	candidateBytes := readAIWorkflowFile(t, candidatePath)

	manualEdit := aiWorkflowSource("a.wav", 0.5)
	if err := os.WriteFile(fixture.sourcePath, manualEdit, 0o600); err != nil {
		t.Fatalf("write manual edit: %v", err)
	}
	err := runCommand(context.Background(), []string{"apply", candidateID}, deps)
	if err == nil {
		t.Fatal("apply runCommand() error = nil, want stale-base refusal")
	}
	if !strings.Contains(err.Error(), "target source hash does not match candidate base hash") {
		t.Errorf("apply error = %q, want stale-base diagnostic", err)
	}
	if got := readAIWorkflowFile(t, fixture.sourcePath); !bytes.Equal(got, manualEdit) {
		t.Errorf("source after stale apply = %q, want manual edit %q", got, manualEdit)
	}
	if bytes.Equal(manualEdit, candidateBytes) {
		t.Fatal("manual edit unexpectedly equals candidate; stale activation was not observable")
	}
	if got := readAIWorkflowFile(t, candidatePath); !bytes.Equal(got, candidateBytes) {
		t.Errorf("candidate changed after stale apply: got %q, want %q", got, candidateBytes)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.storeRoot, "backups")); !os.IsNotExist(statErr) {
		t.Errorf("backup directory stat error = %v, want not exist", statErr)
	}
}

func newAIWorkflowFixture(t *testing.T) aiWorkflowFixture {
	t.Helper()

	root := t.TempDir()
	soundsPath := filepath.Join(root, "sounds")
	if err := os.MkdirAll(soundsPath, 0o700); err != nil {
		t.Fatalf("create sound directory: %v", err)
	}
	for _, name := range []string{"a.wav", "b.wav"} {
		if err := os.WriteFile(filepath.Join(soundsPath, name), []byte("fake audio"), 0o600); err != nil {
			t.Fatalf("write sound %s: %v", name, err)
		}
	}
	original := aiWorkflowSource("a.wav", 1)
	sourcePath := filepath.Join(root, "pattern.fnl")
	if err := os.WriteFile(sourcePath, original, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return aiWorkflowFixture{
		root:       root,
		soundsPath: soundsPath,
		sourcePath: sourcePath,
		storeRoot:  filepath.Join(root, ".basso"),
		original:   original,
	}
}

func aiWorkflowSource(sample string, velocity float64) []byte {
	return []byte(fmt.Sprintf(
		"(bpm 400)\n(steps 1)\n\n(fn pattern [bar]\n  [{:step 0 :sample %q :velocity %.1f :pan 0}])\n\npattern\n",
		sample,
		velocity,
	))
}

func aiWorkflowPaletteOverlap(count int) []byte {
	var source strings.Builder
	source.WriteString("(bpm 400)\n(steps 1)\n\n(fn pattern [bar]\n  [")
	for range count {
		source.WriteString(`
   {:step 0 :note "C3" :instrument "pad" :length 1 :velocity 0.3 :pan 0}`)
	}
	source.WriteString("])\n\npattern\n")
	return []byte(source.String())
}

func aiWorkflowCommandDependencies(
	fixture aiWorkflowFixture,
	model suggest.Model,
	stdout io.Writer,
) commandDependencies {
	return commandDependencies{
		stdout:        stdout,
		stderr:        io.Discard,
		getenv:        func(string) string { return "" },
		now:           func() time.Time { return aiWorkflowNow },
		invocationDir: fixture.root,
		storeRoot:     fixture.storeRoot,
		newModel: func(ai.Config) (suggest.Model, error) {
			return model, nil
		},
		newPreflighter: newEvaluatorPreflighter,
	}
}

func aiWorkflowSuggestArgs(fixture aiWorkflowFixture, prompt string) []string {
	return []string{
		"suggest",
		"--provider", "ollama",
		"--model", "workflow-model",
		"--sounds", fixture.soundsPath,
		fixture.sourcePath,
		prompt,
	}
}

func startAIWorkflowEngine(
	t *testing.T,
	fixture aiWorkflowFixture,
	reporter engine.DiagnosticReporter,
) (*aiWorkflowSink, *aiWorkflowEngineSession) {
	t.Helper()

	inventory, err := engine.LoadSoundInventory(fixture.soundsPath)
	if err != nil {
		t.Fatalf("load sound inventory: %v", err)
	}
	provider, err := engine.NewFromFile(
		fixture.sourcePath,
		engine.NewEvaluator(inventory, 250*time.Millisecond),
		reporter,
	)
	if err != nil {
		t.Fatalf("engine.NewFromFile() error = %v", err)
	}
	sink := newAIWorkflowSink()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	playback := engine.NewEngine(sink)
	go func() {
		done <- playback.Run(ctx, provider)
	}()

	session := &aiWorkflowEngineSession{
		cancel:   cancel,
		done:     done,
		provider: provider,
	}
	t.Cleanup(func() {
		runErr, closeErr := session.stop()
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			t.Errorf("cleanup engine Run() error = %v", runErr)
		}
		if closeErr != nil {
			t.Errorf("cleanup provider Close() error = %v", closeErr)
		}
	})
	return sink, session
}

func waitForAIWorkflowFire(
	t *testing.T,
	events <-chan aiWorkflowFire,
	source string,
	after time.Duration,
) aiWorkflowFire {
	t.Helper()

	timer := time.NewTimer(aiWorkflowTimeout)
	defer timer.Stop()
	for {
		select {
		case fire := <-events:
			if fire.source == source && fire.begin > after {
				return fire
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s fire later than %v", source, after)
		}
	}
}

func waitForAIWorkflowDiagnostic(
	t *testing.T,
	diagnostics <-chan engine.Diagnostic,
) engine.Diagnostic {
	t.Helper()

	select {
	case diagnostic := <-diagnostics:
		return diagnostic
	case <-time.After(aiWorkflowTimeout):
		t.Fatal("timed out waiting for live revision diagnostic")
		return engine.Diagnostic{}
	}
}

func assertAIWorkflowLifecycle(
	t *testing.T,
	sink *aiWorkflowSink,
	wantStarts int,
	wantTeardowns int,
) {
	t.Helper()
	starts, teardowns := sink.lifecycle()
	if starts != wantStarts || teardowns != wantTeardowns {
		t.Errorf(
			"sink lifecycle = (%d starts, %d teardowns), want (%d, %d)",
			starts,
			teardowns,
			wantStarts,
			wantTeardowns,
		)
	}
}

func requireAIWorkflowCandidatePair(
	t *testing.T,
	storeRoot string,
) (string, string, string) {
	t.Helper()

	fennelFiles := globAIWorkflowFiles(t, filepath.Join(storeRoot, "candidates", "*.fnl"))
	metadataFiles := globAIWorkflowFiles(t, filepath.Join(storeRoot, "candidates", "*.json"))
	if len(fennelFiles) != 1 || len(metadataFiles) != 1 {
		t.Fatalf(
			"candidate pair counts = (%d .fnl, %d .json), want exactly (1, 1): %v %v",
			len(fennelFiles),
			len(metadataFiles),
			fennelFiles,
			metadataFiles,
		)
	}
	id := strings.TrimSuffix(filepath.Base(fennelFiles[0]), ".fnl")
	if got := strings.TrimSuffix(filepath.Base(metadataFiles[0]), ".json"); got != id {
		t.Fatalf("candidate pair IDs = (%q, %q), want equal", id, got)
	}
	return id, fennelFiles[0], metadataFiles[0]
}

func requireSingleAIWorkflowBackup(t *testing.T, storeRoot string) string {
	t.Helper()
	backups := globAIWorkflowFiles(t, filepath.Join(storeRoot, "backups", "*"))
	if len(backups) != 1 {
		t.Fatalf("backup count = %d, want 1: %v", len(backups), backups)
	}
	return backups[0]
}

func globAIWorkflowFiles(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %q: %v", pattern, err)
	}
	return matches
}

func readAIWorkflowFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func aiWorkflowSHA256(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
