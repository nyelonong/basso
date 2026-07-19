package engine

import (
	_ "embed"
	"fmt"
	"math/rand"
	"os"
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
const fennelReloadDebounce = 100 * time.Millisecond

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
	source string

	pendingMu sync.Mutex
	pending   *string

	watcher     *fsnotify.Watcher
	watcherDone chan struct{}
}

// New constructs a FennelProvider holding source. It does not compile or
// evaluate source; Next re-evaluates the held source fresh on every call (see
// Next's doc comment). It has no file watcher: it never changes source on
// its own. Use NewFromFile for that.
func New(source string) (*FennelProvider, error) {
	return &FennelProvider{source: source}, nil
}

// A function on FennelProvider's file-backed path, NewFromFile, reads its
// initial source from path and starts an fsnotify watcher on it. Writes to
// path are debounced by fennelReloadDebounce and then staged as the pending
// source, which Next picks up at the start of its next call — see
// setPendingSource. Callers must call Close when done with the provider, to
// stop the watcher goroutine and release its fsnotify handle.
func NewFromFile(path string) (*FennelProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fennel: read %s: %w", path, err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fennel: create watcher: %w", err)
	}
	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("fennel: watch %s: %w", path, err)
	}

	fp := &FennelProvider{
		source:      string(data),
		watcher:     watcher,
		watcherDone: make(chan struct{}),
	}
	go fp.watchLoop(path)

	return fp, nil
}

// setPendingSource stages source to be swapped in at the start of the next
// Next call. It is also used directly by tests as a deterministic hook for
// this apply logic, without needing a real file or a real fsnotify event.
func (fp *FennelProvider) setPendingSource(source string) {
	fp.pendingMu.Lock()
	defer fp.pendingMu.Unlock()
	fp.pending = &source
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
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(fennelReloadDebounce, func() {
				data, err := os.ReadFile(path)
				if err != nil {
					// Transient (e.g. mid-write on some platforms/editors);
					// the next write event will retry.
					return
				}
				fp.setPendingSource(string(data))
			})

		case _, ok := <-fp.watcher.Errors:
			if !ok {
				return
			}
		}
	}
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
		fp.source = *pending
	}

	L := lua.NewState()
	defer L.Close()

	if err := L.DoString(fennelCompilerSource); err != nil {
		return nil, 0, 0, fmt.Errorf("fennel: load compiler: %w", err)
	}
	fennelMod, ok := L.Get(-1).(*lua.LTable)
	L.Pop(1)
	if !ok {
		return nil, 0, 0, fmt.Errorf("fennel: compiler did not return a module table")
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

	evalFn := fennelMod.RawGetString("eval")
	L.Push(evalFn)
	L.Push(lua.LString(fp.source))
	if err := L.PCall(1, 1, nil); err != nil {
		return nil, 0, 0, fmt.Errorf("fennel: eval source: %w", err)
	}
	patternVal := L.Get(-1)
	L.Pop(1)
	patternFn, ok := patternVal.(*lua.LFunction)
	if !ok {
		return nil, 0, 0, fmt.Errorf("fennel: source did not yield a pattern function (last form must be `pattern`)")
	}

	L.Push(patternFn)
	L.Push(lua.LNumber(bar))
	if err := L.PCall(1, 1, nil); err != nil {
		return nil, 0, 0, fmt.Errorf("fennel: pattern(%d): %w", bar, err)
	}
	result := L.Get(-1)
	L.Pop(1)
	hitsTable, ok := result.(*lua.LTable)
	if !ok {
		return nil, 0, 0, fmt.Errorf("fennel: pattern(%d) did not return a table", bar)
	}

	n := hitsTable.Len()
	hits := make([]Hit, 0, n)
	for i := 1; i <= n; i++ {
		row, ok := hitsTable.RawGetInt(i).(*lua.LTable)
		if !ok {
			return nil, 0, 0, fmt.Errorf("fennel: hit %d is not a table", i)
		}

		pan := row.RawGetString("pan")
		panValue := rand.Float64()*2 - 1 // default: random in [-1,1], same precedent as StaticProvider (3.1)
		if pan != lua.LNil {
			panValue = float64(lua.LVAsNumber(pan))
		}

		velocity := row.RawGetString("velocity")
		velocityValue := 1.0 // default
		if velocity != lua.LNil {
			velocityValue = float64(lua.LVAsNumber(velocity))
		}

		hits = append(hits, Hit{
			Step:     int(lua.LVAsNumber(row.RawGetString("step"))),
			Sample:   lua.LVAsString(row.RawGetString("sample")),
			Pan:      panValue,
			Velocity: velocityValue,
		})
	}

	return hits, bpm, stepsPerBar, nil
}
