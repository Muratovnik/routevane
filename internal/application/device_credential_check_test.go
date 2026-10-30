package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ADR 0059: a password is checked with a read-only login before it is kept.
// Only a login the device refused stops the save; every other outcome keeps the
// password and says what the check found.
func TestSavingACredentialReportsWhatTheReadOnlyLoginFound(t *testing.T) {
	tests := []struct {
		name       string
		checkErr   error
		wantResult string
		wantCode   string
	}{
		{name: "the device accepted it", wantResult: CredentialVerified},
		{name: "the deployer has no login", checkErr: ErrNoCredentialCheck, wantResult: CredentialCheckNotApplicable},
		{name: "the interface is missing", checkErr: fmt.Errorf("interface: %w", errors.Join(ErrDeviceAnswerInvalid, ErrDeviceInterfaceMissing)), wantResult: CredentialInterfaceMissing, wantCode: "device_interface_missing"},
		{name: "the device is unreachable", checkErr: fmt.Errorf("dial: %w", ErrDeviceUnreachable), wantResult: CredentialUnverified, wantCode: "device_unreachable"},
		{name: "the answer is unusable", checkErr: fmt.Errorf("version: %w", ErrDeviceAnswerInvalid), wantResult: CredentialUnverified, wantCode: "device_answer_invalid"},
		{name: "the connection does not suit the deployer", checkErr: fmt.Errorf("%w: policy", ErrConnectionInvalid), wantResult: CredentialUnverified, wantCode: "connection_invalid"},
		{name: "an unclassified failure", checkErr: errors.New("something else"), wantResult: CredentialUnverified, wantCode: "check_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets := &fakeSecretStore{available: true}
			devices, store := deviceService(t, secrets)
			device := registered(t, devices)
			var asked []Connection
			devices.config.CheckCredential = func(ctx context.Context, targetID string, connection Connection) error {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > CredentialCheckBudget {
					t.Errorf("the check is not bounded by its budget: deadline=%v ok=%v", deadline, ok)
				}
				if targetID != device.TargetID {
					t.Errorf("target = %q", targetID)
				}
				asked = append(asked, connection)
				return test.checkErr
			}

			const offered = "пароль"
			saved, check, err := devices.SaveCredential(context.Background(), device.ID, offered)
			if err != nil || check.Result != test.wantResult || check.Code != test.wantCode {
				t.Fatalf("check = %#v err = %v, want %s/%s", check, err, test.wantResult, test.wantCode)
			}
			want := Connection{URL: device.Address, Username: device.Account, Password: offered, Interface: device.Interface}
			if len(asked) != 1 || asked[0] != want {
				t.Fatalf("asked = %#v, want one check of %#v", asked, want)
			}
			if !saved.CredentialStored || !store.devices[0].CredentialStored || secrets.secrets[secretKey(device.ID)] != offered {
				t.Fatalf("the password was not kept: saved = %#v secrets = %#v", saved, secrets.secrets)
			}
		})
	}
}

// A password the device refuses is not kept, replaces nothing and enables
// nothing. The refusal names the cause and none of the device's answer.
func TestAPasswordTheDeviceRefusesIsNotKept(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	refuse := false
	devices.config.CheckCredential = func(context.Context, string, Connection) error {
		if refuse {
			return fmt.Errorf("login: authentication status 401: %w", ErrDeviceAuthRejected)
		}
		return nil
	}

	refuse = true
	_, check, err := devices.SaveCredential(context.Background(), device.ID, "wrong")
	if !errors.Is(err, ErrDeviceAuthRejected) || check != (CredentialCheck{}) || strings.Contains(err.Error(), "authentication status") {
		t.Fatalf("check = %#v err = %v", check, err)
	}
	if store.devices[0].CredentialStored || len(secrets.secrets) != 0 {
		t.Fatalf("a refused first save left state: device = %#v secrets = %#v", store.devices[0], secrets.secrets)
	}

	refuse = false
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "right"); err != nil {
		t.Fatal(err)
	}
	refuse = true
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "wrong"); !errors.Is(err, ErrDeviceAuthRejected) {
		t.Fatalf("err = %v", err)
	}
	if !store.devices[0].CredentialStored || secrets.secrets[secretKey(device.ID)] != "right" {
		t.Fatalf("a refused replacement changed the saved password: device = %#v secrets = %#v", store.devices[0], secrets.secrets)
	}
	if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, "wrong"); !errors.Is(err, ErrDeviceAuthRejected) {
		t.Fatalf("enable with a refused password: err = %v", err)
	}
	if store.devices[0].AutoDeliver || secrets.secrets[secretKey(device.ID)] != "right" {
		t.Fatalf("a refused password enabled delivery or replaced the saved one: device = %#v secrets = %#v", store.devices[0], secrets.secrets)
	}
}

// A request that ends during the check keeps nothing: nobody saw the answer.
// A platform without a store does not ask the device at all.
func TestACheckThatOutlivesItsRequestOrHasNowhereToKeepKeepsNothing(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	devices, store := deviceService(t, secrets)
	device := registered(t, devices)
	ctx, cancel := context.WithCancel(context.Background())
	devices.config.CheckCredential = func(context.Context, string, Connection) error {
		cancel()
		return fmt.Errorf("dial: %w", ErrDeviceUnreachable)
	}
	if _, _, err := devices.SaveCredential(ctx, device.ID, "пароль"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the request's cancellation", err)
	}
	if store.devices[0].CredentialStored || len(secrets.secrets) != 0 {
		t.Fatalf("a cancelled save left state: device = %#v secrets = %#v", store.devices[0], secrets.secrets)
	}

	secrets.available = false
	devices.config.CheckCredential = func(context.Context, string, Connection) error {
		t.Error("the device was asked about a password nothing could keep")
		return nil
	}
	if _, _, err := devices.SaveCredential(context.Background(), device.ID, "пароль"); !errors.Is(err, ErrSecretStoreUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
