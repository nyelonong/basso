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
// path. Production code passes newFennelProvider (below); tests inject a
// stub that records the path instead of reading a real file.
type providerConstructor func(path string) (closablePatternProvider, error)

// newFennelProvider adapts engine.FennelProvider.NewFromFile to
// providerConstructor's signature.
func newFennelProvider(path string) (closablePatternProvider, error) {
	return engine.NewFromFile(path)
}

// newBeepSink adapts engine.NewBeepSink to a zero-arg constructor, so run()
// can also take a stub in place of it.
func newBeepSink() engine.AudioSink {
	return engine.NewBeepSink("sound/808/")
}

// progressProvider decorates a PatternProvider, printing the bar number and
// active bpm to out after each successful Next call, and returning the same
// values unmodified otherwise. Engine.Run only ever calls Next from its own
// single goroutine, so this needs no synchronization.
type progressProvider struct {
	engine.PatternProvider
	out io.Writer
}

func (p *progressProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	hits, bpm, stepsPerBar, err := p.PatternProvider.Next(bar)
	if err == nil {
		fmt.Fprintf(p.out, "bar %d bpm %d\n", bar, bpm)
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

// run is main()'s testable core: it resolves args to a file path,
// constructs a provider via newProvider, wraps it to print bar/bpm progress
// to out, and plays it through an Engine backed by newSink until ctx is
// cancelled or the provider errors. Tests call it with stub newProvider and
// newSink, so no real file, watcher, or audio device is touched.
func run(ctx context.Context, args []string, out io.Writer, newProvider providerConstructor, newSink func() engine.AudioSink) error {
	path, err := resolveFile(args)
	if err != nil {
		return err
	}

	provider, err := newProvider(path)
	if err != nil {
		return err
	}
	defer provider.Close()

	e := engine.NewEngine(newSink())
	loud := &progressProvider{PatternProvider: provider, out: out}

	if err := e.Run(ctx, loud); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, newFennelProvider, newBeepSink); err != nil {
		fmt.Fprintln(os.Stderr, "basso:", err)
		os.Exit(1)
	}
}
