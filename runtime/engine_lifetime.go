package runtime

import (
	"context"
	"fmt"

	"github.com/josephjohncox/effectus/schema/ledger"
)

// Entries exist only while calls use or wait for them. They do not cache
// execution state; the ledger is authoritative on every request.
type executionGate struct {
	permit chan struct{}
	users  int
}

type historicalGeneration struct {
	ready      chan struct{}
	generation *Generation
	err        error
	users      int
}

func (engine *Engine) beginExecution(ctx context.Context, id string) (func(), Observer, error) {
	engine.mu.Lock()
	if engine.closed {
		engine.mu.Unlock()
		return nil, nil, fmt.Errorf("engine is closed")
	}
	engine.started = true
	engine.active.Add(1)
	gate := engine.executions[id]
	if gate == nil {
		gate = &executionGate{permit: make(chan struct{}, 1)}
		engine.executions[id] = gate
	}
	gate.users++
	observer := engine.observer
	engine.mu.Unlock()
	release := func() {
		engine.mu.Lock()
		gate.users--
		if gate.users == 0 {
			delete(engine.executions, id)
		}
		engine.mu.Unlock()
		engine.active.Done()
	}
	select {
	case gate.permit <- struct{}{}:
		return func() { <-gate.permit; release() }, observer, nil
	case <-ctx.Done():
		release()
		return nil, observer, ctx.Err()
	}
}

func (engine *Engine) acquireHistorical(ctx context.Context, artifact ledger.ExecutionArtifact) (*Generation, func(), error) {
	engine.mu.Lock()
	entry := engine.historical[artifact.GenerationDigest]
	leader := entry == nil
	if leader {
		entry = &historicalGeneration{ready: make(chan struct{})}
		engine.historical[artifact.GenerationDigest] = entry
	}
	entry.users++
	engine.mu.Unlock()
	release := func() { engine.releaseHistorical(artifact.GenerationDigest, entry) }
	if leader {
		var generation *Generation
		var err error
		if engine.resolver == nil {
			err = fmt.Errorf("no immutable artifact resolver is configured")
		} else {
			generation, err = engine.resolver.ResolveGeneration(ctx, artifact)
		}
		if err == nil && (generation == nil || generation.Checked() == nil || generation.Closed() || generation.Digest() != artifact.GenerationDigest) {
			err = fmt.Errorf("resolved generation does not match the pinned executable artifact")
		}
		if err != nil && generation != nil && generation != engine.generation {
			engine.recordCloseError(generation.Close())
			generation = nil
		}
		entry.generation, entry.err = generation, err
		close(entry.ready)
	}
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case <-entry.ready:
		if entry.err != nil {
			release()
			return nil, nil, entry.err
		}
		return entry.generation, release, nil
	}
}

func (engine *Engine) releaseHistorical(digest string, entry *historicalGeneration) {
	engine.mu.Lock()
	entry.users--
	last := entry.users == 0
	if last {
		delete(engine.historical, digest)
	}
	engine.mu.Unlock()
	if last && entry.generation != nil && entry.generation != engine.generation {
		engine.recordCloseError(entry.generation.Close())
	}
}

func (engine *Engine) recordCloseError(err error) {
	if err == nil {
		return
	}
	engine.mu.Lock()
	// Historical generations can be resolved indefinitely over the process
	// lifetime. Retain the first failure, not an unbounded errors.Join chain.
	if engine.closeErr == nil {
		engine.closeErr = err
	}
	engine.mu.Unlock()
}
