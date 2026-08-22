package main

import (
	"context"
	"errors"
	"sync"

	"github.com/nyelonong/basso/internal/engine"
)

type studioTransportState int

const (
	transportPlaying studioTransportState = iota
	transportPaused
	transportStopped
)

func (state studioTransportState) String() string {
	switch state {
	case transportPaused:
		return "paused"
	case transportStopped:
		return "stopped"
	default:
		return "playing"
	}
}

type studioTransportControl interface {
	TogglePause() studioTransportState
	Stop() studioTransportState
	Play() studioTransportState
}

type studioTransport struct {
	parent         context.Context
	provider       closablePatternProvider
	streamProvider engine.PatternProvider
	engine         *engine.Engine
	sink           *engine.SessionSink
	pace           *engine.PausableClock
	onDone         func(error)

	mu           sync.Mutex
	state        studioTransportState
	streamCancel context.CancelFunc
	streamDone   chan error
	lastErr      error
	closed       bool
	closeOnce    sync.Once
	closeErr     error
}

func newStudioTransport(
	parent context.Context,
	path string,
	observers playbackObservers,
	newProvider providerConstructor,
	newSink func() engine.AudioSink,
	onDone func(error),
) (*studioTransport, error) {
	diagnostics := observers.onDiagnostic
	if diagnostics == nil {
		diagnostics = func(engine.Diagnostic) {}
	}
	provider, err := newProvider(path, diagnostics)
	if err != nil {
		return nil, err
	}
	pace := engine.NewPausableClock()
	sink := engine.NewSessionSink(newSink(), pace)
	observed := &observingProvider{PatternProvider: provider, onBar: observers.onBar}
	return &studioTransport{
		parent:         parent,
		provider:       provider,
		streamProvider: observed,
		engine:         engine.NewPacedEngine(sink, pace),
		sink:           sink,
		pace:           pace,
		onDone:         onDone,
		state:          transportStopped,
	}, nil
}

func (transport *studioTransport) Start() studioTransportState {
	transport.mu.Lock()
	if transport.closed || transport.state != transportStopped {
		state := transport.state
		transport.mu.Unlock()
		return state
	}
	transport.sink.Start()
	transport.state = transportPlaying
	transport.startStreamLocked()
	transport.mu.Unlock()
	return transportPlaying
}

func (transport *studioTransport) startStreamLocked() {
	ctx, cancel := context.WithCancel(transport.parent)
	done := make(chan error, 1)
	transport.streamCancel = cancel
	transport.streamDone = done
	go func() {
		err := transport.engine.Stream(ctx, transport.streamProvider)
		if err != nil && !errors.Is(err, context.Canceled) {
			transport.mu.Lock()
			transport.lastErr = err
			transport.mu.Unlock()
		}
		done <- err
		if err != nil && !errors.Is(err, context.Canceled) && transport.onDone != nil {
			transport.onDone(err)
		}
	}()
}

func (transport *studioTransport) TogglePause() studioTransportState {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	switch transport.state {
	case transportPlaying:
		transport.pace.Pause()
		transport.state = transportPaused
	case transportPaused:
		transport.pace.Resume()
		transport.state = transportPlaying
	}
	return transport.state
}

func (transport *studioTransport) Stop() studioTransportState {
	transport.mu.Lock()
	if transport.state == transportStopped {
		transport.mu.Unlock()
		return transportStopped
	}
	cancel, done := transport.streamCancel, transport.streamDone
	transport.streamCancel, transport.streamDone = nil, nil
	transport.state = transportStopped
	transport.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	transport.pace.Resume()
	if done != nil {
		<-done
	}
	return transportStopped
}

func (transport *studioTransport) Play() studioTransportState {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.closed || transport.state != transportStopped {
		return transport.state
	}
	transport.pace.Resume()
	transport.sink.Rebase()
	transport.state = transportPlaying
	transport.startStreamLocked()
	return transportPlaying
}

func (transport *studioTransport) Close() error {
	transport.closeOnce.Do(func() {
		transport.Stop()
		transport.mu.Lock()
		transport.closed = true
		transport.mu.Unlock()
		transport.sink.Teardown()
		providerErr := transport.provider.Close()
		transport.mu.Lock()
		transport.closeErr = errors.Join(transport.lastErr, providerErr)
		transport.mu.Unlock()
	})
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.closeErr
}
