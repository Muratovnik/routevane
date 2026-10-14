//go:build routevane_testmode

package secretstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The test build's store keeps the contract Store keeps, without the
// operating system: an absent key is a fact, a write replaces, a delete of
// what is gone succeeds, and two stores share nothing.
func TestTestModeMemoryStoreKeepsTheStoreContract(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if !store.Available() {
		t.Fatal("the test store must offer to save a password")
	}
	if _, err := store.Secret(ctx, "device-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an absent credential must be ErrNotFound: %v", err)
	}
	for _, value := range []string{"first-value", "second-value"} {
		if err := store.PutSecret(ctx, "device-a", "admin", value); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Secret(ctx, "device-a"); err != nil || got != value {
			t.Fatalf("Secret = %q, %v; want %q", got, err, value)
		}
	}
	if _, err := NewMemory().Secret(ctx, "device-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second store must not see the first one's credential: %v", err)
	}
	if err := store.DeleteSecret(ctx, "device-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSecret(ctx, "device-a"); err != nil {
		t.Fatalf("deleting an absent credential must succeed: %v", err)
	}
	if _, err := store.Secret(ctx, "device-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the credential outlived its deletion: %v", err)
	}
}

// A refused write names neither the key's secret nor stores anything.
func TestTestModeMemoryStoreRefusesWithoutEchoingTheSecret(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	err := store.PutSecret(ctx, "", "admin", "never-echoed-value")
	if err == nil || strings.Contains(err.Error(), "never-echoed-value") {
		t.Fatalf("PutSecret with no key = %v", err)
	}
	if err := store.PutSecret(ctx, "device-b", "admin", ""); err == nil {
		t.Fatal("an empty secret must be refused")
	}
	if _, err := store.Secret(ctx, "device-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused write left something behind: %v", err)
	}
}
