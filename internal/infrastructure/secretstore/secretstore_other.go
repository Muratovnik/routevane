//go:build !windows

// Package secretstore keeps a device credential where the operating system
// keeps credentials, and nowhere else.
package secretstore

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("no credential is stored under this key")

// ErrUnavailable is what every operation answers on a platform this build has
// no store for. Writing the credential to a file instead would be the one thing
// ADR 0014 refuses: a password this product wrote to disk is worse than a
// password the operator types, because nothing in the operating system knows to
// protect it and nobody thinks to look for it.
var ErrUnavailable = errors.New("this build has no operating system secret store")

type Store struct{}

func New() Store { return Store{} }

// Available reports that unattended delivery cannot be offered here, so the
// surface never presents a choice it could not keep.
func (Store) Available() bool { return false }

func (Store) PutSecret(_ context.Context, _, _, _ string) error { return ErrUnavailable }
func (Store) Secret(_ context.Context, _ string) (string, error) {
	return "", ErrUnavailable
}

// DeleteSecret succeeds because there is nothing stored to remove. Refusing
// would leave a device that can never be forgotten.
func (Store) DeleteSecret(_ context.Context, _ string) error { return nil }
