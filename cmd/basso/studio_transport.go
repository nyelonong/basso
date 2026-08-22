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
	transportStopping
	transportStopped
)

func (state studioTransportState) String() string {
	switch state {
	case transportPaused:
		return "paused"
	case transportStopping:
		return "stopping"
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
	ArmCandidate(string) error
	CommitCandidate() error
	ReleaseCandidate() error
}

type providerSwitch struct {
	candidate bool
	done      chan struct{}
}

type switchingProvider struct {
	mu sync.Mutex

	real            engine.PatternProvider
	candidate       engine.PatternProvider
	activeCandidate bool
	pending         *providerSwitch
}

func newSwitchingProvider(real engine.PatternProvider) *switchingProvider {
	return &switchingProvider{real: real}
}

func (provider *switchingProvider) Arm(candidate engine.PatternProvider, immediate bool) (<-chan struct{}, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.candidate != nil {
		return nil, errors.New("candidate provider is already armed")
	}
	provider.candidate = candidate
	done := make(chan struct{})
	if immediate {
		provider.activeCandidate = true
		close(done)
	} else {
		provider.pending = &providerSwitch{candidate: true, done: done}
	}
	return done, nil
}

func (provider *switchingProvider) UseReal(immediate bool) <-chan struct{} {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	done := make(chan struct{})
	if provider.pending != nil {
		close(provider.pending.done)
		provider.pending = nil
	}
	if immediate || provider.candidate == nil {
		provider.activeCandidate = false
		close(done)
	} else {
		provider.pending = &providerSwitch{done: done}
	}
	return done
}

func (provider *switchingProvider) ClearCandidate() {
	provider.mu.Lock()
	provider.candidate = nil
	provider.activeCandidate = false
	provider.mu.Unlock()
}

func (provider *switchingProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	provider.mu.Lock()
	if provider.pending != nil {
		provider.activeCandidate = provider.pending.candidate
		close(provider.pending.done)
		provider.pending = nil
	}
	active := provider.real
	if provider.activeCandidate {
		active = provider.candidate
	}
	provider.mu.Unlock()
	return active.Next(bar)
}

type studioTransport struct {
	parent            context.Context
	provider          closablePatternProvider
	candidateProvider closablePatternProvider
	switcher          *switchingProvider
	newProvider       providerConstructor
	diagnostics       engine.DiagnosticReporter
	streamProvider    engine.PatternProvider
	engine            *engine.Engine
	sink              *engine.SessionSink
	pace              *engine.PausableClock
	onDone            func(error)

	mu           sync.Mutex
	state        studioTransportState
	streamCancel context.CancelFunc
	streamDone   chan error
	stopDone     chan struct{}
	lastErr      error
	releasing    bool
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
	switcher := newSwitchingProvider(provider)
	observed := &observingProvider{PatternProvider: switcher, onBar: observers.onBar}
	return &studioTransport{
		parent:         parent,
		provider:       provider,
		switcher:       switcher,
		newProvider:    newProvider,
		diagnostics:    diagnostics,
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
		if transport.releasing {
			transport.switcher.UseReal(true)
		}
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
	if transport.state == transportStopping {
		stopped := transport.stopDone
		transport.mu.Unlock()
		<-stopped
		return transportStopped
	}
	boundary := transport.pace.PauseAtBoundary()
	cancel, streamDone := transport.streamCancel, transport.streamDone
	stopped := make(chan struct{})
	transport.stopDone = stopped
	transport.state = transportStopping
	if transport.releasing {
		transport.switcher.UseReal(true)
	}
	transport.mu.Unlock()

	streamEnded := false
	if streamDone != nil {
		select {
		case <-boundary:
		case <-streamDone:
			streamEnded = true
		}
	}
	if cancel != nil {
		cancel()
	}
	transport.pace.Resume()
	if streamDone != nil && !streamEnded {
		<-streamDone
	}

	transport.mu.Lock()
	transport.streamCancel, transport.streamDone = nil, nil
	transport.state = transportStopped
	close(stopped)
	transport.stopDone = nil
	transport.mu.Unlock()
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

func (transport *studioTransport) ArmCandidate(path string) error {
	candidate, err := transport.newProvider(path, transport.diagnostics)
	if err != nil {
		return err
	}

	transport.mu.Lock()
	if transport.closed || transport.candidateProvider != nil {
		transport.mu.Unlock()
		_ = candidate.Close()
		return errors.New("candidate provider is already armed")
	}
	immediate := transport.state != transportPlaying
	if _, err := transport.switcher.Arm(candidate, immediate); err != nil {
		transport.mu.Unlock()
		_ = candidate.Close()
		return err
	}
	transport.candidateProvider = candidate
	transport.mu.Unlock()
	return nil
}

func (transport *studioTransport) CommitCandidate() error {
	var refreshErr error
	if provider, ok := transport.provider.(interface{ Refresh() error }); ok {
		refreshErr = provider.Refresh()
	}
	return errors.Join(refreshErr, transport.ReleaseCandidate())
}

func (transport *studioTransport) ReleaseCandidate() error {
	transport.mu.Lock()
	candidate := transport.candidateProvider
	if candidate == nil {
		transport.mu.Unlock()
		return nil
	}
	if transport.releasing {
		transport.mu.Unlock()
		return errors.New("candidate release is already in progress")
	}
	transport.releasing = true
	switched := transport.switcher.UseReal(transport.state != transportPlaying)
	transport.mu.Unlock()

	<-switched
	transport.switcher.ClearCandidate()
	closeErr := candidate.Close()

	transport.mu.Lock()
	transport.releasing = false
	if closeErr == nil && transport.candidateProvider == candidate {
		transport.candidateProvider = nil
	}
	transport.mu.Unlock()
	return closeErr
}

func (transport *studioTransport) Close() error {
	transport.closeOnce.Do(func() {
		transport.Stop()
		candidateErr := transport.ReleaseCandidate()
		transport.mu.Lock()
		transport.closed = true
		transport.mu.Unlock()
		transport.sink.Teardown()
		providerErr := transport.provider.Close()
		transport.mu.Lock()
		transport.closeErr = errors.Join(transport.lastErr, candidateErr, providerErr)
		transport.mu.Unlock()
	})
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.closeErr
}
