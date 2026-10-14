package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type observedDeploymentDevices struct {
	deploymentDeviceSource
	credentialReads int
}

func (s *observedDeploymentDevices) DeviceCredential(ctx context.Context, id string) (string, error) {
	s.credentialReads++
	return s.deploymentDeviceSource.DeviceCredential(ctx, id)
}

func registeredDeploymentCommand(device Device) DeployCommand {
	return DeployCommand{
		ArtifactID: deploymentArtifactID,
		DeviceID:   device.ID,
		ExpectedConnection: &DeviceConnectionSnapshot{
			Address: device.Address, Account: device.Account, Interface: device.Interface,
		},
		Confirm: true,
	}
}

// ADR 0050: a saved credential serves a manual delivery whether or not
// automatic delivery is on, a credential in the request wins for that attempt,
// and a secret that is not recorded as saved is never read.
func TestRegisteredDeploymentUsesOnlyTheSelectedAccessAndTheSavedSecret(t *testing.T) {
	tests := []struct {
		name       string
		consent    bool
		saved      bool
		stored     string
		oneUse     string
		wantSecret string
		wantReads  int
		wantErr    error
	}{
		{name: "one-use credential", oneUse: "one-use-secret", wantSecret: "one-use-secret"},
		{name: "saved credential without automatic delivery", saved: true, stored: "saved-secret", wantSecret: "saved-secret", wantReads: 1},
		{name: "saved credential with automatic delivery", consent: true, saved: true, stored: "saved-secret", wantSecret: "saved-secret", wantReads: 1},
		{name: "one-use overrides saved credential", saved: true, stored: "saved-secret", oneUse: "one-use-secret", wantSecret: "one-use-secret"},
		{name: "orphaned secret that is not recorded as saved", stored: "orphaned-secret", wantErr: ErrConnectionInvalid},
		{name: "consent alone is not a saved credential", consent: true, stored: "orphaned-secret", wantErr: ErrConnectionInvalid},
		{name: "saved fact but the store lost the secret", saved: true, wantReads: 1, wantErr: ErrConnectionInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
			devices, store := deviceService(t, secrets)
			device := registered(t, devices)
			store.devices[0].AutoDeliver, store.devices[0].CredentialStored = test.consent, test.saved
			if test.stored != "" {
				secrets.secrets[secretKey(device.ID)] = test.stored
			}
			observed := &observedDeploymentDevices{deploymentDeviceSource: devices}
			artifacts := deploymentTestArtifacts()
			deployer := &spyDeployer{formatKey: artifacts.target.FormatKey, backup: []byte("previous device state")}
			deployments := deploymentTestService(t, artifacts, deployer)
			command := registeredDeploymentCommand(device)
			command.Connection.Password = test.oneUse

			resolved, err := deployments.ResolveDeviceConnection(context.Background(), command, observed)
			if !errors.Is(err, test.wantErr) || observed.credentialReads != test.wantReads {
				t.Fatalf("resolve error=%v, secret reads=%d; want error=%v reads=%d", err, observed.credentialReads, test.wantErr, test.wantReads)
			}
			if test.wantErr == nil {
				want := Connection{URL: device.Address, Username: device.Account, Interface: device.Interface, Password: test.wantSecret}
				if resolved.Connection != want || resolved.DeviceID != "" || resolved.ExpectedConnection != nil || resolved.ArtifactID != command.ArtifactID || resolved.Confirm != command.Confirm {
					t.Fatal("registered access was not resolved to the exact selected connection")
				}
				result, deployErr := deployments.Deploy(context.Background(), resolved)
				if deployErr != nil || !result.Applied || len(deployer.seenPasswords) != 1 || deployer.seenPasswords[0] != test.wantSecret {
					t.Fatalf("deployment applied=%v error=%v, password uses=%d", result.Applied, deployErr, len(deployer.seenPasswords))
				}
				encoded, marshalErr := json.Marshal(result)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if strings.Contains(string(encoded), test.wantSecret) {
					t.Fatal("deployment audit disclosed the credential")
				}
			} else if len(deployer.calls) != 0 {
				t.Fatalf("refused selection contacted the device: %v", deployer.calls)
			}
			if store.devices[0].AutoDeliver != test.consent || store.devices[0].CredentialStored != test.saved || secrets.secrets[secretKey(device.ID)] != test.stored || artifacts.output.DeviceID != "" {
				t.Fatal("manual delivery changed consent, the saved credential, or an output binding")
			}
		})
	}
}

func TestRegisteredDeploymentRefusesStaleOrMixedMetadataBeforeReadingASecret(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DeployCommand, *fakeDeviceStore, *stubArtifacts)
		wantErr error
	}{
		{"changed address", func(_ *DeployCommand, store *fakeDeviceStore, _ *stubArtifacts) {
			store.devices[0].Address = "http://10.255.0.2"
		}, ErrConnectionInvalid},
		{"changed account", func(_ *DeployCommand, store *fakeDeviceStore, _ *stubArtifacts) {
			store.devices[0].Account = "another-admin"
		}, ErrConnectionInvalid},
		{"changed interface", func(_ *DeployCommand, store *fakeDeviceStore, _ *stubArtifacts) {
			store.devices[0].Interface = "Wireguard1"
		}, ErrConnectionInvalid},
		{"raw address mixed in", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) {
			command.Connection.URL = "http://10.255.0.2"
		}, ErrConnectionInvalid},
		{"raw account mixed in", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) {
			command.Connection.Username = "admin"
		}, ErrConnectionInvalid},
		{"raw interface mixed in", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) {
			command.Connection.Interface = "Wireguard0"
		}, ErrConnectionInvalid},
		{"missing snapshot", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) { command.ExpectedConnection = nil }, ErrConnectionInvalid},
		{"snapshot is not normalized", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) {
			command.ExpectedConnection.Address += " "
		}, ErrConnectionInvalid},
		{"another target with the same renderer", func(_ *DeployCommand, _ *fakeDeviceStore, artifacts *stubArtifacts) {
			artifacts.target.ID = "another-keenetic-target"
			artifacts.output.TargetID = artifacts.target.ID
		}, ErrDeviceIncompatible},
		{"deleted registration", func(command *DeployCommand, _ *fakeDeviceStore, _ *stubArtifacts) {
			command.DeviceID = strings.Repeat("b", 32)
		}, ErrNotFound},
		{"display rename preserves the access", func(_ *DeployCommand, store *fakeDeviceStore, _ *stubArtifacts) {
			store.devices[0].Name = "Renamed router"
		}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
			devices, store := deviceService(t, secrets)
			device := registered(t, devices)
			store.devices[0].CredentialStored = true
			secrets.secrets[secretKey(device.ID)] = "saved-secret"
			observed := &observedDeploymentDevices{deploymentDeviceSource: devices}
			artifacts := deploymentTestArtifacts()
			deployer := &spyDeployer{formatKey: artifacts.target.FormatKey}
			deployments := deploymentTestService(t, artifacts, deployer)
			command := registeredDeploymentCommand(device)
			test.mutate(&command, store, artifacts)

			_, err := deployments.ResolveDeviceConnection(context.Background(), command, observed)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("resolve error=%v; want %v", err, test.wantErr)
			}
			wantReads := 0
			if test.wantErr == nil {
				wantReads = 1
			}
			if observed.credentialReads != wantReads || len(deployer.calls) != 0 {
				t.Fatalf("secret reads=%d, device calls=%v", observed.credentialReads, deployer.calls)
			}
		})
	}
}

func TestRegisteredDeploymentRechecksTheDisplayedAccessAfterPreview(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Device)
	}{
		{"address", func(device *Device) { device.Address = "http://10.255.0.2" }},
		{"account", func(device *Device) { device.Account = "another-admin" }},
		{"interface", func(device *Device) { device.Interface = "Wireguard1" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
			devices, store := deviceService(t, secrets)
			device := registered(t, devices)
			store.devices[0].CredentialStored = true
			secrets.secrets[secretKey(device.ID)] = "saved-secret"
			observed := &observedDeploymentDevices{deploymentDeviceSource: devices}
			artifacts := deploymentTestArtifacts()
			deployer := &spyDeployer{formatKey: artifacts.target.FormatKey}
			deployments := deploymentTestService(t, artifacts, deployer)
			command := registeredDeploymentCommand(device)
			command.Confirm = false
			resolved, err := deployments.ResolveDeviceConnection(context.Background(), command, observed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := deployments.Plan(context.Background(), resolved); err != nil {
				t.Fatal(err)
			}
			test.mutate(&store.devices[0])
			observed.credentialReads = 0
			command.Confirm = true
			if _, err := deployments.ResolveDeviceConnection(context.Background(), command, observed); !errors.Is(err, ErrConnectionInvalid) {
				t.Fatalf("changed %s after preview: %v", test.name, err)
			}
			if observed.credentialReads != 0 || len(deployer.calls) != 0 {
				t.Fatal("a stale preview reached a credential or device")
			}
		})
	}
}

func TestRawDeploymentRemainsUsableAndUnresolvedDeviceReferencesAreRefused(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	deployments := deploymentTestService(t, artifacts, &spyDeployer{formatKey: artifacts.target.FormatKey})
	command := deploymentTestCommand(false)
	resolved, err := deployments.ResolveDeviceConnection(context.Background(), command, nil)
	if err != nil || resolved != command {
		t.Fatalf("raw connection changed: %v", err)
	}
	if _, err := deployments.Plan(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	command.ExpectedConnection = &DeviceConnectionSnapshot{}
	if _, err := deployments.ResolveDeviceConnection(context.Background(), command, nil); !errors.Is(err, ErrConnectionInvalid) {
		t.Fatalf("stray snapshot accepted: %v", err)
	}
	command.DeviceID = strings.Repeat("d", 32)
	if _, err := deployments.Plan(context.Background(), command); !errors.Is(err, ErrConnectionInvalid) {
		t.Fatalf("unresolved device selection accepted: %v", err)
	}
}
