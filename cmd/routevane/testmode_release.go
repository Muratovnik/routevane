//go:build !routevane_testmode

package main

import (
	"github.com/Muratovnik/routevane/internal/application"
	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	"github.com/Muratovnik/routevane/internal/infrastructure/secretstore"
)

// testBuild is false in every binary built without the routevane_testmode tag,
// which includes every release, installer and desktop core (ADR 0052).
const testBuild = false

// buildSuffix is what `routevane version` appends; a release reports its
// version alone.
const buildSuffix = ""

// newSecretStore is the operating system's credential store (ADR 0014).
func newSecretStore() application.SecretStore { return secretstore.New() }

// defaultDeviceDialer leaves the deployers on the standard library dialer.
func defaultDeviceDialer() deployerkeenetic.Dialer { return nil }
