package engine

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	lua "github.com/yuin/gopher-lua"
)

// fennelCompilerSource is the vendored Fennel compiler (fennel/compiler.lua),
// loaded into a fresh gopher-lua VM on every Next call.
//
//go:embed fennel/compiler.lua
var fennelCompilerSource string

// fennelReloadDebounce is how long the fsnotify watcher waits after the
// last write event on the watched file before reading it and staging its
// contents as the pending source. This absorbs multi-chunk editor saves
// (several write events for one logical save) into a single reload.
const (
	fennelReloadDebounce    = 100 * time.Millisecond
	maxFennelSourceBytes    = 256 * 1024
	legacyEvaluationTimeout = 250 * time.Millisecond
)

// EvaluationPhase identifies the stage at which Fennel evaluation failed.
type EvaluationPhase string

const (
	EvaluationPhaseCompile  EvaluationPhase = "compile"
	EvaluationPhaseEvaluate EvaluationPhase = "evaluate"
	EvaluationPhaseTimeout  EvaluationPhase = "timeout"
	EvaluationPhaseValidate EvaluationPhase = "validate"
)

// EvaluationError reports a bounded Fennel evaluation failure for one bar.
type EvaluationError struct {
	Phase EvaluationPhase
	Bar   int
	Err   error
}

func (e *EvaluationError) Error() string {
	return fmt.Sprintf("fennel: %s bar %d: %v", e.Phase, e.Bar, e.Err)
}

// Unwrap preserves the interpreter, validation, or context error for callers
// using errors.Is or errors.As.
func (e *EvaluationError) Unwrap() error {
	return e.Err
}

// Evaluator compiles, runs, maps, and validates untrusted Fennel source in a
// fresh, sandboxed Lua state for every bar.
type Evaluator struct {
	inventory SoundInventory
	timeout   time.Duration
}

// NewEvaluator constructs an evaluator with an immutable copy of inventory.
func NewEvaluator(inventory SoundInventory, timeout time.Duration) *Evaluator {
	inventoryCopy := make(SoundInventory, len(inventory))
	for name := range inventory {
		inventoryCopy[name] = struct{}{}
	}

	return &Evaluator{
		inventory: inventoryCopy,
		timeout:   timeout,
	}
}

// FennelProvider is a PatternProvider that compiles and evaluates a Fennel
// (Lisp) script through an embedded gopher-lua VM.
//
// FennelProvider itself is not safe for concurrent use in general (Next is
// only ever meant to be called by a single goroutine, per PatternProvider's
// contract), but the pending-source field below is: a file-backed
// FennelProvider's watcher goroutine writes it while Next (called from
// Engine.Run's goroutine) reads it, so pendingMu guards that one field
// explicitly.
type FennelProvider struct {
	source    string
	evaluator *Evaluator
	reporter  DiagnosticReporter
	lastGood  *Bar

	diagnosticMu     sync.Mutex
	reporterMu       sync.Mutex
	observedRevision string
	emitted          map[string]struct{}

	pendingMu sync.Mutex
	pending   *string

	path        string
	watcher     *fsnotify.Watcher
	watcherDone chan struct{}
}

// New constructs a FennelProvider and validates its initial source at bar 0.
func New(
	source string,
	evaluator *Evaluator,
	reporter DiagnosticReporter,
) (*FennelProvider, error) {
	if evaluator == nil {
		return nil, errors.New("fennel: evaluator must not be nil")
	}
	initial, err := evaluator.Evaluate(context.Background(), source, 0)
	if err != nil {
		return nil, err
	}

	return &FennelProvider{
		source:           source,
		evaluator:        evaluator,
		reporter:         reporter,
		lastGood:         &initial,
		observedRevision: revisionSHA256(source),
		emitted:          make(map[string]struct{}),
	}, nil
}

// A function on FennelProvider's file-backed path, NewFromFile, reads its
// initial source from path and starts an fsnotify watcher on it. Writes to
// path are debounced by fennelReloadDebounce and then staged as the pending
// source, which Next picks up at the start of its next call — see
// setPendingSource. Callers must call Close when done with the provider, to
// stop the watcher goroutine and release its fsnotify handle.
func NewFromFile(
	path string,
	evaluator *Evaluator,
	reporter DiagnosticReporter,
) (*FennelProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fennel: read %s: %w", path, err)
	}
	fp, err := New(string(data), evaluator, reporter)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fennel: create watcher: %w", err)
	}
	cleanPath := filepath.Clean(path)
	parent := filepath.Dir(cleanPath)
	if err := watcher.Add(parent); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("fennel: watch %s: %w", parent, err)
	}

	fp.path = cleanPath
	fp.watcher = watcher
	fp.watcherDone = make(chan struct{})
	go fp.watchLoop(cleanPath)

	return fp, nil
}

// setPendingSource stages source to be swapped in at the start of the next
// Next call. It is also used directly by tests as a deterministic hook for
// this apply logic, without needing a real file or a real fsnotify event.
func (fp *FennelProvider) setPendingSource(source string) {
	fp.observeRevision(source)
	fp.pendingMu.Lock()
	defer fp.pendingMu.Unlock()
	fp.pending = &source
}

func (fp *FennelProvider) observeRevision(source string) {
	revision := revisionSHA256(source)
	fp.diagnosticMu.Lock()
	defer fp.diagnosticMu.Unlock()
	if revision == fp.observedRevision {
		return
	}
	fp.observedRevision = revision
	fp.emitted = make(map[string]struct{})
}

func (fp *FennelProvider) report(diagnostic Diagnostic) {
	if fp.reporter == nil {
		return
	}

	key := diagnosticKey(diagnostic)
	fp.diagnosticMu.Lock()
	if _, exists := fp.emitted[key]; exists {
		fp.diagnosticMu.Unlock()
		return
	}
	fp.emitted[key] = struct{}{}
	fp.diagnosticMu.Unlock()

	fp.reporterMu.Lock()
	defer fp.reporterMu.Unlock()
	fp.reporter(diagnostic)
}

func (fp *FennelProvider) observedRevisionSHA256() string {
	fp.diagnosticMu.Lock()
	defer fp.diagnosticMu.Unlock()
	return fp.observedRevision
}

// takePendingSource returns the staged pending source, if any, clearing it,
// so a second call before the next setPendingSource returns nil.
func (fp *FennelProvider) takePendingSource() *string {
	fp.pendingMu.Lock()
	defer fp.pendingMu.Unlock()
	pending := fp.pending
	fp.pending = nil
	return pending
}

// watchLoop reads path and calls setPendingSource fennelReloadDebounce after
// the last write event, until watcherDone is closed (by Close) or the
// watcher's Events channel closes. It runs in its own goroutine, started by
// NewFromFile.
func (fp *FennelProvider) watchLoop(path string) {
	var debounce *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()

	for {
		select {
		case <-fp.watcherDone:
			return

		case event, ok := <-fp.watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != path {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if debounce == nil {
				debounce = time.NewTimer(fennelReloadDebounce)
			} else {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(fennelReloadDebounce)
			}
			debounceC = debounce.C

		case <-debounceC:
			debounceC = nil
			data, err := os.ReadFile(path)
			if err != nil {
				fp.report(watchDiagnostic(fp.observedRevisionSHA256(), err))
				continue
			}
			fp.setPendingSource(string(data))

		case err, ok := <-fp.watcher.Errors:
			if !ok {
				return
			}
			fp.report(watchDiagnostic(fp.observedRevisionSHA256(), err))
		}
	}
}

// Refresh synchronously stages the current file for the next bar.
func (fp *FennelProvider) Refresh() error {
	if fp.path == "" {
		return errors.New("fennel: provider is not file-backed")
	}
	data, err := os.ReadFile(fp.path)
	if err != nil {
		return fmt.Errorf("fennel: refresh %s: %w", fp.path, err)
	}
	fp.setPendingSource(string(data))
	return nil
}

// Close stops fp's fsnotify watcher goroutine and releases its underlying
// fsnotify handle. It is a no-op on a FennelProvider constructed via New
// (which has no watcher). Safe to defer right after NewFromFile.
func (fp *FennelProvider) Close() error {
	if fp.watcher == nil {
		return nil
	}
	close(fp.watcherDone)
	return fp.watcher.Close()
}

// Next re-evaluates fp's currently held Fennel source in a fresh gopher-lua
// VM state on every call — never compiled once and cached — so a later hot
// reload can simply swap the held source string without changing Next's
// behavior. It loads the vendored Fennel compiler, evaluates the source
// (whose final top-level form must be a reference to the script's pattern
// function), calls pattern(bar), and maps the returned hit tables to Hits.
func (fp *FennelProvider) Next(bar int) ([]Hit, int, int, error) {
	if pending := fp.takePendingSource(); pending != nil {
		result, err := fp.evaluator.Evaluate(context.Background(), *pending, bar)
		if err == nil {
			fp.source = *pending
			return fp.rememberAndReturn(result)
		}
		fp.report(evaluationDiagnostic(*pending, bar, err))
	}

	result, err := fp.evaluator.Evaluate(context.Background(), fp.source, bar)
	if err != nil {
		fp.report(evaluationDiagnostic(fp.source, bar, err))
		if fp.lastGood != nil {
			fallback := cloneBar(*fp.lastGood)
			return fallback.Hits, fallback.BPM, fallback.StepsPerBar, nil
		}
		return nil, 0, 0, err
	}

	return fp.rememberAndReturn(result)
}

func (fp *FennelProvider) rememberAndReturn(result Bar) ([]Hit, int, int, error) {
	stored := cloneBar(result)
	fp.lastGood = &stored
	return cloneHits(result.Hits), result.BPM, result.StepsPerBar, nil
}

func cloneBar(bar Bar) Bar {
	bar.Hits = cloneHits(bar.Hits)
	return bar
}

func cloneHits(hits []Hit) []Hit {
	return append([]Hit(nil), hits...)
}

// Evaluate compiles and evaluates source for bar in a fresh sandboxed Lua
// state and validates the mapped result against the evaluator's inventory.
func (e *Evaluator) Evaluate(ctx context.Context, source string, bar int) (Bar, error) {
	if ctx == nil {
		return Bar{}, newEvaluationError(
			EvaluationPhaseEvaluate,
			bar,
			errors.New("context must not be nil"),
		)
	}
	if len(source) > maxFennelSourceBytes {
		return Bar{}, newEvaluationError(
			EvaluationPhaseCompile,
			bar,
			fmt.Errorf("source exceeds %d bytes", maxFennelSourceBytes),
		)
	}

	evaluationCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	L := lua.NewState()
	defer L.Close()
	L.SetContext(evaluationCtx)

	if err := L.DoString(fennelCompilerSource); err != nil {
		return Bar{}, classifyEvaluationError(
			evaluationCtx,
			EvaluationPhaseCompile,
			bar,
			fmt.Errorf("load compiler: %w", err),
		)
	}
	fennelMod, ok := L.Get(-1).(*lua.LTable)
	L.Pop(1)
	if !ok {
		return Bar{}, newEvaluationError(
			EvaluationPhaseCompile,
			bar,
			errors.New("compiler did not return a module table"),
		)
	}

	bpm := 120
	stepsPerBar := 16

	L.SetGlobal("bpm", L.NewFunction(func(l *lua.LState) int {
		bpm = int(l.CheckNumber(1))
		return 0
	}))
	L.SetGlobal("steps", L.NewFunction(func(l *lua.LState) int {
		stepsPerBar = int(l.CheckNumber(1))
		return 0
	}))

	removeUnsafeGlobals(L)

	compileFn := fennelMod.RawGetString("compileString")
	if compileFn == lua.LNil {
		compileFn = fennelMod.RawGetString("compile-string")
	}
	L.Push(compileFn)
	L.Push(lua.LString(source))
	if err := L.PCall(1, 1, nil); err != nil {
		return Bar{}, classifyEvaluationError(
			evaluationCtx,
			EvaluationPhaseCompile,
			bar,
			fmt.Errorf("compile source: %w", err),
		)
	}
	luaSource, ok := L.Get(-1).(lua.LString)
	L.Pop(1)
	if !ok {
		return Bar{}, newEvaluationError(
			EvaluationPhaseCompile,
			bar,
			errors.New("compiler did not return Lua source"),
		)
	}

	chunk, err := L.LoadString(string(luaSource))
	if err != nil {
		return Bar{}, classifyEvaluationError(
			evaluationCtx,
			EvaluationPhaseCompile,
			bar,
			fmt.Errorf("load compiled source: %w", err),
		)
	}

	L.Push(chunk)
	if err := L.PCall(0, 1, nil); err != nil {
		return Bar{}, classifyEvaluationError(
			evaluationCtx,
			EvaluationPhaseEvaluate,
			bar,
			fmt.Errorf("evaluate source: %w", err),
		)
	}
	patternVal := L.Get(-1)
	L.Pop(1)
	patternFn, ok := patternVal.(*lua.LFunction)
	if !ok {
		return Bar{}, newEvaluationError(
			EvaluationPhaseEvaluate,
			bar,
			errors.New("source did not yield a pattern function (last form must be `pattern`)"),
		)
	}

	L.Push(patternFn)
	L.Push(lua.LNumber(bar))
	if err := L.PCall(1, 1, nil); err != nil {
		return Bar{}, classifyEvaluationError(
			evaluationCtx,
			EvaluationPhaseEvaluate,
			bar,
			fmt.Errorf("pattern: %w", err),
		)
	}
	result := L.Get(-1)
	L.Pop(1)
	hitsTable, ok := result.(*lua.LTable)
	if !ok {
		return Bar{}, newEvaluationError(
			EvaluationPhaseEvaluate,
			bar,
			errors.New("pattern did not return a table"),
		)
	}

	n := hitsTable.Len()
	hits := make([]Hit, 0, n)
	for i := 1; i <= n; i++ {
		row, ok := hitsTable.RawGetInt(i).(*lua.LTable)
		if !ok {
			return Bar{}, newEvaluationError(
				EvaluationPhaseEvaluate,
				bar,
				fmt.Errorf("hit %d is not a table", i),
			)
		}

		sample := row.RawGetString("sample")
		note := row.RawGetString("note")
		if (sample != lua.LNil) == (note != lua.LNil) {
			return Bar{}, newEvaluationError(
				EvaluationPhaseEvaluate,
				bar,
				fmt.Errorf("hit %d must have exactly one of :sample or :note", i),
			)
		}

		stepValue, hasStep, err := mappedHitNumber(row, "step", i)
		if err != nil {
			return Bar{}, newEvaluationError(EvaluationPhaseEvaluate, bar, err)
		}
		if !hasStep {
			return Bar{}, newEvaluationError(
				EvaluationPhaseEvaluate,
				bar,
				fmt.Errorf("hit %d :step is required", i),
			)
		}

		panValue, hasPan, err := mappedHitNumber(row, "pan", i)
		if err != nil {
			return Bar{}, newEvaluationError(EvaluationPhaseEvaluate, bar, err)
		}
		if !hasPan && note != lua.LNil {
			// :note hits default to centered pan, not random: bass is
			// conventionally centered, and a low, often-quiet synthesized
			// note is easy to lose when random pan happens to land near a
			// hard-left/hard-right extreme.
			panValue = 0
		}
		if !hasPan && note == lua.LNil {
			// :sample hits default to random in [-1,1], same precedent as
			// StaticProvider (3.1) and the original m001 per-fire pan.
			panValue = rand.Float64()*2 - 1
		}

		velocityValue, hasVelocity, err := mappedHitNumber(row, "velocity", i)
		if err != nil {
			return Bar{}, newEvaluationError(EvaluationPhaseEvaluate, bar, err)
		}
		if !hasVelocity {
			velocityValue = 1
		}

		lengthNumber, hasLength, err := mappedHitNumber(row, "length", i)
		if err != nil {
			return Bar{}, newEvaluationError(EvaluationPhaseEvaluate, bar, err)
		}
		lengthValue := 1
		if hasLength {
			lengthValue = int(lengthNumber)
		}

		instrument := row.RawGetString("instrument")
		instrumentValue := lua.LVAsString(instrument)
		if note != lua.LNil && instrument == lua.LNil {
			// :note hits default to "bass" when :instrument is omitted, so
			// every existing pattern keeps working unchanged. Not validated
			// here — whether a given name is a real instrument is beepSink's
			// call at play time, same precedent as a malformed :note.
			instrumentValue = "bass"
		}

		hits = append(hits, Hit{
			Step:       int(stepValue),
			Sample:     lua.LVAsString(sample),
			Note:       lua.LVAsString(note),
			Instrument: instrumentValue,
			Length:     lengthValue,
			Pan:        panValue,
			Velocity:   velocityValue,
		})
	}

	mappedBar := Bar{
		Hits:        hits,
		BPM:         bpm,
		StepsPerBar: stepsPerBar,
	}
	if err := ValidateBar(mappedBar, e.inventory); err != nil {
		return Bar{}, newEvaluationError(EvaluationPhaseValidate, bar, err)
	}

	return mappedBar, nil
}

// Preflight evaluates every bar in the inclusive horizon using a fresh state
// per Evaluate call.
func (e *Evaluator) Preflight(
	ctx context.Context,
	source string,
	firstBar int,
	lastBar int,
) error {
	if firstBar < 0 {
		return newEvaluationError(
			EvaluationPhaseValidate,
			firstBar,
			errors.New("preflight first bar must not be negative"),
		)
	}
	if lastBar < firstBar {
		return newEvaluationError(
			EvaluationPhaseValidate,
			firstBar,
			errors.New("preflight horizon must not be inverted"),
		)
	}

	for bar := firstBar; ; bar++ {
		if _, err := e.Evaluate(ctx, source, bar); err != nil {
			return err
		}
		if bar == lastBar {
			return nil
		}
	}
}

func removeUnsafeGlobals(L *lua.LState) {
	for _, name := range []string{
		"os",
		"io",
		"debug",
		"package",
		"require",
		"dofile",
		"loadfile",
		"channel",
		"coroutine",
	} {
		L.SetGlobal(name, lua.LNil)
	}
}

func classifyEvaluationError(
	ctx context.Context,
	phase EvaluationPhase,
	bar int,
	err error,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return newEvaluationError(EvaluationPhaseTimeout, bar, ctxErr)
	}
	return newEvaluationError(phase, bar, err)
}

func newEvaluationError(phase EvaluationPhase, bar int, err error) error {
	return &EvaluationError{
		Phase: phase,
		Bar:   bar,
		Err:   err,
	}
}

func mappedHitNumber(row *lua.LTable, field string, hit int) (float64, bool, error) {
	value := row.RawGetString(field)
	if value == lua.LNil {
		return 0, false, nil
	}

	number, ok := value.(lua.LNumber)
	if !ok {
		return 0, false, fmt.Errorf("hit %d :%s must be a number", hit, field)
	}
	return float64(number), true, nil
}
