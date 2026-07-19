package engine

import (
	_ "embed"
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// fennelCompilerSource is the vendored Fennel compiler (fennel/compiler.lua),
// loaded into a fresh gopher-lua VM on every Next call.
//
//go:embed fennel/compiler.lua
var fennelCompilerSource string

// FennelProvider is a PatternProvider that compiles and evaluates a Fennel
// (Lisp) script through an embedded gopher-lua VM.
type FennelProvider struct {
	source string
}

// New constructs a FennelProvider holding source. It does not compile or
// evaluate source; Next re-evaluates the held source fresh on every call (see
// Next's doc comment).
func New(source string) (*FennelProvider, error) {
	return &FennelProvider{source: source}, nil
}

// Next re-evaluates fp's currently held Fennel source in a fresh gopher-lua
// VM state on every call — never compiled once and cached — so a later hot
// reload can simply swap the held source string without changing Next's
// behavior. It loads the vendored Fennel compiler, evaluates the source
// (whose final top-level form must be a reference to the script's pattern
// function), calls pattern(bar), and maps the returned hit tables to Hits.
func (fp *FennelProvider) Next(bar int) ([]Hit, int, int, error) {
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
		hits = append(hits, Hit{
			Step:     int(lua.LVAsNumber(row.RawGetString("step"))),
			Sample:   lua.LVAsString(row.RawGetString("sample")),
			Pan:      float64(lua.LVAsNumber(row.RawGetString("pan"))),
			Velocity: float64(lua.LVAsNumber(row.RawGetString("velocity"))),
		})
	}

	return hits, bpm, stepsPerBar, nil
}
