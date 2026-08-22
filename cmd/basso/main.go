// Command basso is a live-coding player: it plays a Fennel pattern file
// continuously through the audio device, bar by bar, hot-reloading it at
// the next bar boundary on save, until interrupted.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nyelonong/basso/internal/engine"
)

// closablePatternProvider is engine.PatternProvider plus the Close lifecycle
// method engine.FennelProvider (from 5.1) adds to stop its fsnotify watcher.
// run() depends on this narrower interface, rather than *engine.FennelProvider
// directly, so tests can substitute a stub with no file or watcher at all.
type closablePatternProvider interface {
	engine.PatternProvider
	Close() error
}

// providerConstructor builds a closablePatternProvider from a .fnl file
// path and a diagnostic reporter. Production code passes newFennelProvider
// (below); tests inject a stub that records the path instead of reading a
// real file.
type providerConstructor func(path string, onDiagnostic engine.DiagnosticReporter) (closablePatternProvider, error)

// newFennelProvider adapts engine.FennelProvider.NewFromFile to
// providerConstructor's signature.
func newFennelProvider(path string, onDiagnostic engine.DiagnosticReporter) (closablePatternProvider, error) {
	inventory, err := engine.LoadSoundInventory("sound/808")
	if err != nil {
		return nil, err
	}
	evaluator := engine.NewEvaluator(inventory, 250*time.Millisecond)
	return engine.NewFromFile(path, evaluator, onDiagnostic)
}

func stderrDiagnosticReporter(stderr io.Writer) engine.DiagnosticReporter {
	return func(diagnostic engine.Diagnostic) {
		if diagnostic.Bar == nil {
			fmt.Fprintf(
				stderr,
				"revision %s %s: %v\n",
				diagnostic.RevisionSHA256,
				diagnostic.Phase,
				diagnostic.Err,
			)
			return
		}
		fmt.Fprintf(
			stderr,
			"revision %s bar %d %s: %v\n",
			diagnostic.RevisionSHA256,
			*diagnostic.Bar,
			diagnostic.Phase,
			diagnostic.Err,
		)
	}
}

// newBeepSink adapts engine.NewBeepSink to a zero-arg constructor, so run()
// can also take a stub in place of it.
func newBeepSink() engine.AudioSink {
	return engine.NewBeepSink("sound/808/")
}

// playbackObservers receives playback events from run(): one OnBar call per
// completed bar with its active bpm and steps per bar, and every engine
// diagnostic as reported. Nil fields are skipped. Both callbacks run on the
// engine's single goroutine; implementations must not block it.
type playbackObservers struct {
	onBar        func(bar, bpm, stepsPerBar int)
	onDiagnostic engine.DiagnosticReporter
}

// observingProvider decorates a PatternProvider, forwarding each successful
// Next call to onBar before returning the same values unmodified otherwise.
// Engine.Run only ever calls Next from its own single goroutine, so this
// needs no synchronization.
type observingProvider struct {
	engine.PatternProvider
	onBar func(bar, bpm, stepsPerBar int)
}

func (p *observingProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	hits, bpm, stepsPerBar, err := p.PatternProvider.Next(bar)
	if err == nil && p.onBar != nil {
		p.onBar(bar, bpm, stepsPerBar)
	}
	return hits, bpm, stepsPerBar, err
}

// resolveFile extracts the target .fnl path from CLI args (os.Args[1:]),
// accepting two forms that resolve to the same path:
//
//	["play", file] → file (exactly one arg after "play")
//	[file]         → file (alias form; file must not be "play")
//
// Anything else — no args, "play" with no file or extra args, more than one
// bare arg, etc. — is an error.
func resolveFile(args []string) (string, error) {
	switch len(args) {
	case 1:
		if args[0] == "play" {
			return "", fmt.Errorf(`basso: "play" requires a file argument`)
		}
		return args[0], nil
	case 2:
		if args[0] != "play" {
			return "", fmt.Errorf("basso: unrecognized arguments: %v", args)
		}
		return args[1], nil
	default:
		return "", fmt.Errorf("basso: usage: basso play <file.fnl> (or basso <file.fnl>)")
	}
}

// run is main()'s testable core: it resolves args to a file path and plays
// it through an Engine until ctx is cancelled or the provider errors,
// forwarding events to observers.
func run(ctx context.Context, args []string, observers playbackObservers, newProvider providerConstructor, newSink func() engine.AudioSink) error {
	path, err := resolveFile(args)
	if err != nil {
		return err
	}
	return playSource(ctx, path, observers, newProvider, newSink)
}

// playSource plays one resolved source path until ctx is cancelled or the
// provider errors, forwarding bar progress and diagnostics to observers. It
// owns no argument parsing.
func playSource(ctx context.Context, path string, observers playbackObservers, newProvider providerConstructor, newSink func() engine.AudioSink) error {
	diagnostics := observers.onDiagnostic
	if diagnostics == nil {
		diagnostics = func(engine.Diagnostic) {}
	}
	provider, err := newProvider(path, diagnostics)
	if err != nil {
		return err
	}
	defer provider.Close()

	e := engine.NewEngine(newSink())
	loud := &observingProvider{PatternProvider: provider, onBar: observers.onBar}

	if err := e.Run(ctx, loud); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := defaultCommandDependencies(os.Stdout, os.Stderr)
	if err == nil {
		err = runCommand(ctx, os.Args[1:], deps)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "basso:", err)
		os.Exit(1)
	}
}
