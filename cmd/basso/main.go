// Command basso is a live-coding player: it plays a pattern continuously
// through the audio device, bar by bar, until interrupted.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nyelonong/basso/internal/engine"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sink := engine.NewAtomixSink()
	e := engine.NewEngine(sink)
	provider := &engine.StaticProvider{}

	if err := e.Run(ctx, provider); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, "basso:", err)
		os.Exit(1)
	}
}
