package application

import (
	"context"
	"errors"
	"sync"
)

var ErrDeliveryGateUnavailable = errors.New("delivery authorization gate is unavailable")

// DeliveryGate serializes a scheduled delivery with changes to the binding,
// device metadata, or consent that authorized it. The token is context-aware:
// a request that stops waiting never runs its mutation later with a dead
// request context.
//
// One process-wide token is intentionally stronger than a keyed lock. Scheduled
// deliveries are already sequential, and authorization changes are rare; the
// smaller state space makes it possible to prove that no detach, rebind, edit,
// disable, or forget can interleave between the final checks and the first
// device side effect.
type DeliveryGate struct {
	token chan struct{}
}

func NewDeliveryGate() *DeliveryGate {
	gate := &DeliveryGate{token: make(chan struct{}, 1)}
	gate.token <- struct{}{}
	return gate
}

// Acquire returns an idempotent release function after obtaining the gate.
func (g *DeliveryGate) Acquire(ctx context.Context) (func(), error) {
	if g == nil || g.token == nil || ctx == nil {
		return nil, ErrDeliveryGateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.token:
	}
	// Cancellation may race with a ready token. A select is allowed to choose
	// either ready case, so check again while owning the token and put it back
	// before refusing the canceled caller.
	if err := ctx.Err(); err != nil {
		g.token <- struct{}{}
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { g.token <- struct{}{} })
	}, nil
}
