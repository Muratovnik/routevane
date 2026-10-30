package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ADR 0050: a saved password is its own fact. These tests cover its lifecycle
// independent of automatic delivery.

func TestSavingACredentialIsIndependentOfAutomaticDelivery(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	if device.CredentialStored {
		t.Fatal("a freshly registered device holds no credential")
	}

	saved, _, err := devices.SaveCredential(context.Background(), device.ID, "first")
	if err != nil || !saved.CredentialStored || saved.AutoDeliver {
		t.Fatalf("saved = %#v err = %v", saved, err)
	}
	if store.devices[0] != saved || secrets.secrets[secretKey(device.ID)] != "first" {
		t.Fatalf("stored = %#v secrets = %#v", store.devices[0], secrets.secrets)
	}
	if credential, err := devices.DeviceCredential(context.Background(), device.ID); err != nil || credential != "first" {
		t.Fatalf("credential = %q err = %v", credential, err)
	}

	// Replacing needs no flag write: the fact was already true.
	store.updateErr = errors.New("disk full")
	replaced, _, err := devices.SaveCredential(context.Background(), device.ID, "second")
	if err != nil || !replaced.CredentialStored || secrets.secrets[secretKey(device.ID)] != "second" {
		t.Fatalf("replaced = %#v secrets = %#v err = %v", replaced, secrets.secrets, err)
	}
}

func TestSavingACredentialRefusesWhatCannotBeKept(t *testing.T) {
	tests := []struct {
		name      string
		available bool
		secret    string
		target    string
		wantErr   error
	}{
		{name: "empty", available: true, secret: "", wantErr: ErrCredentialInvalid},
		{name: "overlong", available: true, secret: strings.Repeat("я", 513), wantErr: ErrCredentialInvalid},
		{name: "no secret store", available: false, secret: "пароль", wantErr: ErrSecretStoreUnavailable},
		{name: "deployer takes no credential", available: true, secret: "пароль", target: "no-credential"},
		{name: "no deployer", available: true, secret: "пароль", target: "download-only", wantErr: ErrDeployerUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: test.available}
			devices, store := deviceService(t, secrets)
			devices.config.Deployable = func(id string) bool { return id != "download-only" }
			devices.config.NeedsCredential = func(id string) bool { return id == "keenetic" }
			device := registered(t, devices)
			if test.target != "" {
				store.devices[0].TargetID = test.target
			}
			_, _, err := devices.SaveCredential(context.Background(), device.ID, test.secret)
			if err == nil || test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("err = %v, want refusal %v", err, test.wantErr)
			}
			if store.devices[0].CredentialStored || len(secrets.secrets) != 0 {
				t.Fatalf("a refused save left state: device = %#v secrets = %#v", store.devices[0], secrets.secrets)
			}
		})
	}
}

// A first save whose flag write fails takes the stored secret with it, so no
// password is held that the registry does not say it holds.
func TestAFailedFirstSaveTakesTheStoredSecretWithIt(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	store.updateErr = errors.New("disk full")
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "пароль"); !errors.Is(err, store.updateErr) {
		t.Fatalf("err = %v", err)
	}
	if len(secrets.secrets) != 0 || store.devices[0].CredentialStored {
		t.Fatalf("secrets = %#v device = %#v", secrets.secrets, store.devices[0])
	}
}

func TestRemovingACredentialDeletesItAndEndsDependentAutomaticDelivery(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, "пароль"); err != nil {
		t.Fatal(err)
	}

	secrets.deleteErr = errors.New("store locked")
	if _, err := devices.RemoveCredential(context.Background(), device.ID); err == nil {
		t.Fatal("expected removal to fail while the store refuses the delete")
	}
	if !store.devices[0].CredentialStored || !store.devices[0].AutoDeliver {
		t.Fatalf("a failed removal changed the recorded state: %#v", store.devices[0])
	}

	secrets.deleteErr = nil
	removed, err := devices.RemoveCredential(context.Background(), device.ID)
	if err != nil || removed.CredentialStored || removed.AutoDeliver {
		t.Fatalf("removed = %#v err = %v", removed, err)
	}
	if len(secrets.secrets) != 0 || store.devices[0] != removed {
		t.Fatalf("secrets = %#v stored = %#v", secrets.secrets, store.devices[0])
	}
	if _, err := devices.DeviceCredential(context.Background(), device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed credential still readable: %v", err)
	}
	// Removing again is not an error: the postcondition already holds.
	if _, err := devices.RemoveCredential(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEnablingAutomaticDeliveryNeedsASavedCredentialWhenTheDeployerTakesOne(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)

	if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, ""); !errors.Is(err, ErrCredentialRequired) {
		t.Fatalf("err = %v, want ErrCredentialRequired", err)
	}
	if store.devices[0].AutoDeliver {
		t.Fatal("automatic delivery was enabled without a saved credential")
	}

	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "saved"); err != nil {
		t.Fatal(err)
	}
	enabled, err := devices.EnableAutoDelivery(context.Background(), device.ID, "")
	if err != nil || !enabled.AutoDeliver || !enabled.CredentialStored || secrets.secrets[secretKey(device.ID)] != "saved" {
		t.Fatalf("enabled = %#v secrets = %#v err = %v", enabled, secrets.secrets, err)
	}

	// After removal it cannot be turned on again without a new password.
	if _, err := devices.RemoveCredential(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, ""); !errors.Is(err, ErrCredentialRequired) {
		t.Fatalf("err = %v, want ErrCredentialRequired", err)
	}
}

// A credential sent with the enable request is saved first, as the credential
// route would, and replaces an earlier one.
func TestEnablingWithASuppliedCredentialSavesItFirst(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, _ := deviceService(t, secrets)
	device := registered(t, devices)
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "old"); err != nil {
		t.Fatal(err)
	}
	enabled, err := devices.EnableAutoDelivery(context.Background(), device.ID, "new")
	if err != nil || !enabled.AutoDeliver || !enabled.CredentialStored || secrets.secrets[secretKey(device.ID)] != "new" {
		t.Fatalf("enabled = %#v secrets = %#v err = %v", enabled, secrets.secrets, err)
	}
	if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, strings.Repeat("x", 1025)); err == nil {
		t.Fatal("an overlong credential was accepted")
	}
	if secrets.secrets[secretKey(device.ID)] != "new" {
		t.Fatal("a refused credential replaced the saved one")
	}
}

// A deployer that takes no credential has no fact to report: removing a
// credential from it changes nothing about automatic delivery.
func TestACredentialFreeDeviceNeverReportsASavedCredential(t *testing.T) {
	const id = "33333333333333333333333333333333"
	secrets := &fakeSecretStore{available: false}
	store := &fakeDeviceStore{devices: []Device{{ID: id, TargetID: "singbox", Name: "sing-box", Address: "file:///tmp/config.json", AutoDeliver: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	devices, err := NewDeviceService(DeviceConfig{
		Store: store, Secrets: secrets,
		Targets:    func() []TargetOption { return []TargetOption{{ID: "singbox"}} },
		Deployable: func(string) bool { return true }, NeedsCredential: func(string) bool { return false },
		NeedsInterface:           func(string) bool { return false },
		ValidateStoredConnection: func(string, Connection) error { return nil },
		RetireManagedRoutes:      func(context.Context, string, Connection) error { return nil },
		CheckCredential:          func(context.Context, string, Connection) error { return nil },
		Clock:                    ClockFunc(time.Now), Entropy: bytes.NewReader(bytes.Repeat([]byte{1}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := devices.SaveCredential(context.Background(), id, "secret"); err == nil {
		t.Fatal("a credential was saved for a deployer that takes none")
	}
	removed, err := devices.RemoveCredential(context.Background(), id)
	if err != nil || removed.CredentialStored || !removed.AutoDeliver {
		t.Fatalf("removed = %#v err = %v", removed, err)
	}
	if credential, err := devices.DeviceCredential(context.Background(), id); err != nil || credential != "" {
		t.Fatalf("credential = %q err = %v", credential, err)
	}
}

// Forgetting a device deletes a credential that was saved without automatic
// delivery, not only one that consent stored.
func TestForgettingADeviceRemovesACredentialSavedWithoutAutomaticDelivery(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "saved"); err != nil {
		t.Fatal(err)
	}
	if err := devices.ForgetDevice(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.devices) != 0 || len(secrets.secrets) != 0 {
		t.Fatalf("devices = %#v secrets = %#v", store.devices, secrets.secrets)
	}
}

// A changed connection deletes a credential saved without automatic delivery
// too: it names the old address and account.
func TestChangingAConnectionDeletesACredentialSavedWithoutAutomaticDelivery(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, _ := deviceService(t, secrets)
	device := registered(t, devices)
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "saved"); err != nil {
		t.Fatal(err)
	}
	updated, err := devices.UpdateDevice(context.Background(), device.ID, device.Name, "http://10.255.0.2", device.Account, device.Interface)
	if err != nil || updated.CredentialStored || len(secrets.secrets) != 0 {
		t.Fatalf("updated = %#v secrets = %#v err = %v", updated, secrets.secrets, err)
	}
}

// The scheduler needs consent AND a saved credential; either alone delivers
// nothing.
func TestUnattendedDeliveryRequiresConsentAndASavedCredential(t *testing.T) {
	tests := []struct {
		name     string
		auto     bool
		saved    bool
		wantSent bool
		wantCode string
		leftover bool
	}{
		{name: "consent and a saved credential", auto: true, saved: true, wantSent: true},
		{name: "a saved credential without consent", saved: true},
		{name: "consent without a saved credential", auto: true, leftover: true, wantCode: "credential_unavailable"},
		{name: "neither", leftover: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
			devices, store := deviceService(t, secrets)
			device := registered(t, devices)
			store.devices[0].AutoDeliver, store.devices[0].CredentialStored = test.auto, test.saved
			if test.saved || test.leftover {
				secrets.secrets[secretKey(device.ID)] = "stored-secret"
			}
			outputs := automaticDeviceSource{outputs: map[string]Output{"output": {ID: "output", DeviceID: device.ID}}}
			deployments := &automaticDeploymentTarget{}
			delivery, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{Devices: devices, Deployments: deployments, Outputs: outputs, Gate: NewDeliveryGate()})
			if err != nil {
				t.Fatal(err)
			}
			runs := delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{
				{OutputID: "output", DeviceID: device.ID, ArtifactID: "artifact"},
			}}})
			if sent := len(deployments.commands) == 1; sent != test.wantSent {
				t.Fatalf("deployments = %#v, want sent=%v", deployments.commands, test.wantSent)
			}
			if test.wantSent && deployments.commands[0].Connection.Password != "stored-secret" {
				t.Fatalf("command = %#v", deployments.commands[0])
			}
			var code string
			if len(runs[0].DeliveryFailures) == 1 {
				code = runs[0].DeliveryFailures[0].Code
			}
			if code != test.wantCode {
				t.Fatalf("failure code = %q, want %q", code, test.wantCode)
			}
		})
	}
}
