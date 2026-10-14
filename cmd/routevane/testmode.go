//go:build routevane_testmode

package main

import (
	"context"
	"errors"
	"net"

	"github.com/Muratovnik/routevane/internal/application"
	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	"github.com/Muratovnik/routevane/internal/infrastructure/secretstore"
)

// This file is compiled only with the routevane_testmode tag, into the binary
// the browser suites drive (ADR 0052). It is never a release binary: the
// release, installer and desktop builds pass no tags.

const testBuild = true

// buildSuffix lets a suite refuse a stale release-shaped binary by asking it,
// rather than by failing later on an address it does not accept.
const buildSuffix = " (test build)"

// newSecretStore keeps saved passwords in this process only, so a suite never
// writes to the workstation's credential store.
func newSecretStore() application.SecretStore { return secretstore.NewMemory() }

// errTestBuildDial is every device connection a test build attempts. The
// documentation addresses it accepts answer nowhere, and a test build must
// never reach a real device either, so it opens no device connection at all.
var errTestBuildDial = errors.New("a test build opens no device connection")

type refusingDeviceDialer struct{}

func (refusingDeviceDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errTestBuildDial
}

func defaultDeviceDialer() deployerkeenetic.Dialer { return refusingDeviceDialer{} }
