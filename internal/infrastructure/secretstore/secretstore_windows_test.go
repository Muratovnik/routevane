//go:build windows

package secretstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The store is the operating system's, so this exercises the real one under a
// key that names the test. It cleans up after itself in every path.
func TestCredentialRoundTripsThroughTheOperatingSystemStore(t *testing.T) {
	store := New()
	if !store.Available() {
		t.Fatal("the Windows build must have a store")
	}
	key := "Routevane test " + t.Name()
	t.Cleanup(func() { _ = store.DeleteSecret(context.Background(), key) })

	if _, err := store.Secret(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an absent credential must be a fact, not a failure: %v", err)
	}
	if err := store.PutSecret(context.Background(), key, "admin", "пароль-1"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Secret(context.Background(), key)
	if err != nil || value != "пароль-1" {
		t.Fatalf("value = %q err = %v", value, err)
	}
	// A second write replaces rather than duplicates: the operator changed the
	// password, they did not acquire a second one.
	if err := store.PutSecret(context.Background(), key, "admin", "пароль-2"); err != nil {
		t.Fatal(err)
	}
	value, err = store.Secret(context.Background(), key)
	if err != nil || value != "пароль-2" {
		t.Fatalf("value = %q err = %v", value, err)
	}
	if err := store.DeleteSecret(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Secret(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the credential outlived its deletion: %v", err)
	}
}

// Removing what is already gone is what the caller asked for.
func TestDeletingAnAbsentCredentialSucceeds(t *testing.T) {
	if err := New().DeleteSecret(context.Background(), "Routevane test absent "+t.Name()); err != nil {
		t.Fatal(err)
	}
}

// Half a password stored is a password that fails at the worst moment.
func TestAnOversizedCredentialIsRefusedRatherThanTruncated(t *testing.T) {
	key := "Routevane test " + t.Name()
	t.Cleanup(func() { _ = New().DeleteSecret(context.Background(), key) })
	err := New().PutSecret(context.Background(), key, "admin", strings.Repeat("x", maxSecretBytes+1))
	if err == nil {
		t.Fatal("expected an oversized credential to be refused")
	}
	if _, err := New().Secret(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused write left something behind: %v", err)
	}
}
