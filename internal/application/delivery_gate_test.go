package application

import (
	"context"
	"errors"
	"testing"
)

func TestDeliveryGateNeverAdmitsAnAlreadyCanceledCallerOrLosesItsToken(t *testing.T) {
	gate := NewDeliveryGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for attempt := 0; attempt < 1000; attempt++ {
		release, err := gate.Acquire(ctx)
		if !errors.Is(err, context.Canceled) {
			if err == nil {
				release()
			}
			t.Fatalf("attempt %d acquired with an already canceled context: %v", attempt, err)
		}
	}
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("canceled callers consumed the authorization token: %v", err)
	}
	release()
}
