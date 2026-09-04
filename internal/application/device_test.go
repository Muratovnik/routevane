package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSecretStore struct {
	available bool
	secrets   map[string]string
	deleteErr error
}

func (s *fakeSecretStore) Available() bool { return s.available }
func (s *fakeSecretStore) PutSecret(_ context.Context, key, _, secret string) error {
	if s.secrets == nil {
		s.secrets = map[string]string{}
	}
	s.secrets[key] = secret
	return nil
}

func (s *fakeSecretStore) Secret(_ context.Context, key string) (string, error) {
	value, ok := s.secrets[key]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *fakeSecretStore) DeleteSecret(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.secrets, key)
	return nil
}

type fakeDeviceStore struct {
	devices   []Device
	updateErr error
}

func (s *fakeDeviceStore) CreateDevice(_ context.Context, device Device) error {
	s.devices = append(s.devices, device)
	return nil
}

func (s *fakeDeviceStore) Device(_ context.Context, id string) (Device, error) {
	for _, device := range s.devices {
		if device.ID == id {
			return device, nil
		}
	}
	return Device{}, ErrNotFound
}

func (s *fakeDeviceStore) Devices(context.Context) ([]Device, error) { return s.devices, nil }

func (s *fakeDeviceStore) UpdateDevice(_ context.Context, device Device) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	for index, stored := range s.devices {
		if stored.ID == device.ID {
			s.devices[index] = device
			return nil
		}
	}
	return ErrNotFound
}

func (s *fakeDeviceStore) DeleteDevice(_ context.Context, id string) error {
	for index, device := range s.devices {
		if device.ID == id {
			s.devices = append(s.devices[:index], s.devices[index+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func deviceService(t *testing.T, secrets *fakeSecretStore) (*DeviceService, *fakeDeviceStore) {
	t.Helper()
	store := &fakeDeviceStore{}
	service, err := NewDeviceService(DeviceConfig{
		Store:   store,
		Secrets: secrets,
		Targets: func() []TargetOption {
			return []TargetOption{{ID: "keenetic", Title: "Keenetic"}}
		},
		Deployable:      func(id string) bool { return id == "keenetic" },
		NeedsCredential: func(id string) bool { return id == "keenetic" },
		NeedsInterface:  func(id string) bool { return id == "keenetic" },
		ValidateStoredConnection: func(_ string, connection Connection) error {
			if connection.Password != "" {
				return ErrConnectionInvalid
			}
			return nil
		},
		RetireManagedRoutes: func(context.Context, string, Connection) error { return nil },
		Clock:               ClockFunc(func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }),
		Entropy:             bytes.NewReader(bytes.Repeat([]byte{7}, 512)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func registered(t *testing.T, service *DeviceService) Device {
	t.Helper()
	device, err := service.RegisterDevice(context.Background(), "keenetic", " Роутер ", " http://192.168.1.1 ", " admin ", " Wireguard0 ")
	if err != nil {
		t.Fatal(err)
	}
	return device
}

// A device is registered by hand and trimmed, and it starts without unattended
// delivery: consent is something the operator gives, never a default.
func TestARegisteredDeviceStartsWithoutUnattendedDelivery(t *testing.T) {
	service, _ := deviceService(t, &fakeSecretStore{available: true})
	device := registered(t, service)
	if device.Name != "Роутер" || device.Address != "http://192.168.1.1" || device.Account != "admin" || device.Interface != "Wireguard0" {
		t.Fatalf("device = %#v", device)
	}
	if device.AutoDeliver {
		t.Fatal("unattended delivery must not be a default")
	}
}

func TestRegistrationRefusesWhatItCannotStand(t *testing.T) {
	service, _ := deviceService(t, &fakeSecretStore{available: true})
	tests := []struct{ name, target, deviceName, address string }{
		{"unknown target", "absent", "Роутер", "http://192.168.1.1"},
		{"no name", "keenetic", "  ", "http://192.168.1.1"},
		{"no address", "keenetic", "Роутер", "  "},
		{"control character", "keenetic", "Ро\x07утер", "http://192.168.1.1"},
		{"overlong name", "keenetic", strings.Repeat("я", 121), "http://192.168.1.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.RegisterDevice(context.Background(), test.target, test.deviceName, test.address, "", ""); err == nil {
				t.Fatalf("expected %s to be refused", test.name)
			}
		})
	}
}

func TestRegistrationEnforcesTheDeployersConnectionFields(t *testing.T) {
	service, _ := deviceService(t, &fakeSecretStore{available: true})
	for name, input := range map[string]struct{ account, interfaceName string }{
		"missing account":   {interfaceName: "Wireguard0"},
		"missing interface": {account: "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.RegisterDevice(context.Background(), "keenetic", "Роутер", "http://192.168.1.1", input.account, input.interfaceName); err == nil {
				t.Fatal("expected the incomplete connection to be refused")
			}
		})
	}
}

func TestRegistrationAppliesTheDeployersStoredConnectionPolicy(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	store := &fakeDeviceStore{}
	service, err := NewDeviceService(DeviceConfig{
		Store: store, Secrets: secrets,
		Targets:         func() []TargetOption { return []TargetOption{{ID: "keenetic"}} },
		Deployable:      func(string) bool { return true },
		NeedsCredential: func(string) bool { return true },
		NeedsInterface:  func(string) bool { return true },
		ValidateStoredConnection: func(_ string, connection Connection) error {
			if connection.URL != "http://192.168.1.1" || connection.Password != "" {
				return ErrConnectionInvalid
			}
			return nil
		},
		RetireManagedRoutes: func(context.Context, string, Connection) error { return nil },
		Clock:               ClockFunc(time.Now), Entropy: bytes.NewReader(bytes.Repeat([]byte{1}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterDevice(context.Background(), "keenetic", "Router", "https://example.com", "admin", "Wireguard0"); !errors.Is(err, ErrConnectionInvalid) {
		t.Fatalf("err = %v", err)
	}
	if len(store.devices) != 0 {
		t.Fatalf("invalid destination was persisted: %#v", store.devices)
	}
}

func TestChangingAConnectionRevokesConsentCredentialAndOldRouteOwnership(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	var retiredTarget string
	var retiredConnection Connection
	service.config.RetireManagedRoutes = func(_ context.Context, target string, connection Connection) error {
		retiredTarget, retiredConnection = target, connection
		return nil
	}
	if _, err := service.EnableAutoDelivery(context.Background(), device.ID, "password"); err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateDevice(context.Background(), device.ID, "New name", "http://192.168.1.2", "operator", "Wireguard1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.AutoDeliver || updated.Address != "http://192.168.1.2" || updated.Account != "operator" || updated.Interface != "Wireguard1" {
		t.Fatalf("updated = %#v", updated)
	}
	if _, held := secrets.secrets[secretKey(device.ID)]; held {
		t.Fatal("the old connection credential survived its consent")
	}
	if store.devices[0] != updated {
		t.Fatalf("stored = %#v updated = %#v", store.devices[0], updated)
	}
	if retiredTarget != device.TargetID || retiredConnection.URL != device.Address || retiredConnection.Username != device.Account || retiredConnection.Interface != device.Interface || retiredConnection.Password != "" {
		t.Fatalf("retired target=%q connection=%#v", retiredTarget, retiredConnection)
	}
}

func TestRenamingADeviceKeepsConsentForTheSameConnection(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, _ := deviceService(t, secrets)
	device := registered(t, service)
	if _, err := service.EnableAutoDelivery(context.Background(), device.ID, "password"); err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateDevice(context.Background(), device.ID, "New name", device.Address, device.Account, device.Interface)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.AutoDeliver || secrets.secrets[secretKey(device.ID)] != "password" {
		t.Fatalf("updated = %#v secrets = %#v", updated, secrets.secrets)
	}
}

func TestAConnectionChangeStopsWhenItsCredentialCannotBeRevoked(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	if _, err := service.EnableAutoDelivery(context.Background(), device.ID, "password"); err != nil {
		t.Fatal(err)
	}
	secrets.deleteErr = errors.New("credential store locked")
	if _, err := service.UpdateDevice(context.Background(), device.ID, device.Name, "http://192.168.1.2", device.Account, device.Interface); err == nil {
		t.Fatal("the connection changed while its old credential could not be revoked")
	}
	stored := store.devices[0]
	if !stored.AutoDeliver || stored.Address != device.Address || secrets.secrets[secretKey(device.ID)] != "password" {
		t.Fatalf("stored = %#v secrets = %#v", stored, secrets.secrets)
	}
}

// The flag is written only after the store accepted the secret, so the surface
// never shows a device as unattended while nothing holds its password.
func TestUnattendedDeliveryIsOnlyOnWhenTheStoreHoldsTheSecret(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, _ := deviceService(t, secrets)
	device := registered(t, service)

	enabled, err := service.EnableAutoDelivery(context.Background(), device.ID, "пароль")
	if err != nil || !enabled.AutoDeliver {
		t.Fatalf("device = %#v err = %v", enabled, err)
	}
	if secrets.secrets[secretKey(device.ID)] != "пароль" {
		t.Fatalf("secrets = %#v", secrets.secrets)
	}
	credential, err := service.DeviceCredential(context.Background(), device.ID)
	if err != nil || credential != "пароль" {
		t.Fatalf("credential = %q err = %v", credential, err)
	}

	disabled, err := service.DisableAutoDelivery(context.Background(), device.ID)
	if err != nil || disabled.AutoDeliver {
		t.Fatalf("device = %#v err = %v", disabled, err)
	}
	if _, held := secrets.secrets[secretKey(device.ID)]; held {
		t.Fatal("the credential outlived the consent that stored it")
	}
}

func TestADeviceWithoutCredentialRequirementsCanOptInWithoutAStoredSecret(t *testing.T) {
	const id = "11111111111111111111111111111111"
	secrets := &fakeSecretStore{available: false, secrets: map[string]string{secretKey(id): "obsolete"}}
	store := &fakeDeviceStore{devices: []Device{{ID: id, TargetID: "singbox", Name: "sing-box", Address: "file:///tmp/config.json", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	service, err := NewDeviceService(DeviceConfig{
		Store: store, Secrets: secrets,
		Targets:    func() []TargetOption { return []TargetOption{{ID: "singbox"}} },
		Deployable: func(string) bool { return true }, NeedsCredential: func(string) bool { return false },
		NeedsInterface:           func(string) bool { return false },
		ValidateStoredConnection: func(string, Connection) error { return nil },
		RetireManagedRoutes:      func(context.Context, string, Connection) error { return nil },
		Clock:                    ClockFunc(time.Now), Entropy: bytes.NewReader(bytes.Repeat([]byte{1}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	device, err := service.EnableAutoDelivery(context.Background(), id, "")
	if err != nil || !device.AutoDeliver {
		t.Fatalf("device = %#v err = %v", device, err)
	}
	credential, err := service.DeviceCredential(context.Background(), id)
	if err != nil || credential != "" || len(secrets.secrets) != 0 {
		t.Fatalf("credential = %q secrets = %#v err = %v", credential, secrets.secrets, err)
	}
}

func TestADeviceWithoutADeployerCannotOptInToAutomaticDelivery(t *testing.T) {
	const id = "22222222222222222222222222222222"
	store := &fakeDeviceStore{devices: []Device{{ID: id, TargetID: "download-only", Name: "Файл", Address: "file:///tmp/config.json", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	service, err := NewDeviceService(DeviceConfig{
		Store: store, Secrets: &fakeSecretStore{available: true},
		Targets:    func() []TargetOption { return []TargetOption{{ID: "download-only"}} },
		Deployable: func(string) bool { return false }, NeedsCredential: func(string) bool { return false },
		NeedsInterface:           func(string) bool { return false },
		ValidateStoredConnection: func(string, Connection) error { return nil },
		RetireManagedRoutes:      func(context.Context, string, Connection) error { return nil },
		Clock:                    ClockFunc(time.Now), Entropy: bytes.NewReader(bytes.Repeat([]byte{1}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnableAutoDelivery(context.Background(), id, ""); !errors.Is(err, ErrDeployerUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if store.devices[0].AutoDeliver {
		t.Fatal("an undeployable target was left opted in")
	}
}

// A credential left over from a disabled device must not be usable by asking.
func TestACredentialIsUnreadableWithoutTheConsentFlag(t *testing.T) {
	secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	secrets.secrets[secretKey(device.ID)] = "leftover"
	store.devices[0].AutoDeliver = false
	if _, err := service.DeviceCredential(context.Background(), device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// A platform with no store is told so, and no file is written instead.
func TestWithoutAStoreUnattendedDeliveryIsRefused(t *testing.T) {
	service, _ := deviceService(t, &fakeSecretStore{available: false})
	device := registered(t, service)
	if service.SecretStoreAvailable() {
		t.Fatal("availability must reflect the platform")
	}
	_, err := service.EnableAutoDelivery(context.Background(), device.ID, "пароль")
	if !errors.Is(err, ErrSecretStoreUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

// The secret is already in the store and the flag is not. Removing it is the
// only state that stays true to what the operator asked for.
func TestAFailedFlagWriteTakesTheStoredSecretWithIt(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	store.updateErr = errors.New("disk full")
	if _, err := service.EnableAutoDelivery(context.Background(), device.ID, "пароль"); err == nil {
		t.Fatal("expected the write to fail")
	}
	if _, held := secrets.secrets[secretKey(device.ID)]; held {
		t.Fatal("a credential was left behind for a device that is not unattended")
	}
}

// A device nobody can see is a credential nobody can revoke, so the credential
// goes first and its failure keeps the device visible.
func TestForgettingADeviceRemovesItsCredentialFirst(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	retireCalls := 0
	service.config.RetireManagedRoutes = func(_ context.Context, target string, connection Connection) error {
		retireCalls++
		if target != device.TargetID || connection.URL != device.Address || connection.Interface != device.Interface || connection.Password != "" {
			t.Fatalf("retire target=%q connection=%#v", target, connection)
		}
		return nil
	}
	if _, err := service.EnableAutoDelivery(context.Background(), device.ID, "пароль"); err != nil {
		t.Fatal(err)
	}
	secrets.deleteErr = errors.New("store locked")
	if err := service.ForgetDevice(context.Background(), device.ID); err == nil {
		t.Fatal("expected forgetting to fail while the credential remains")
	}
	if len(store.devices) != 1 {
		t.Fatal("the device disappeared while its credential remained")
	}
	if retireCalls != 0 {
		t.Fatal("ownership retired while the credential kept forgetting from proceeding")
	}
	secrets.deleteErr = nil
	if err := service.ForgetDevice(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.devices) != 0 || len(secrets.secrets) != 0 {
		t.Fatalf("devices = %#v secrets = %#v", store.devices, secrets.secrets)
	}
	if retireCalls != 1 {
		t.Fatalf("retire calls = %d", retireCalls)
	}
}

// A secret can outlive the flag that was meant to guard it -- a failed flag
// write, or a crash between the two writes, leaves it orphaned. Forgetting the
// device must clean it up even though AutoDeliver was never set.
func TestForgettingADeviceRemovesAnOrphanedCredentialEvenWithoutTheFlag(t *testing.T) {
	secrets := &fakeSecretStore{available: true, secrets: map[string]string{}}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	if device.AutoDeliver {
		t.Fatal("a freshly registered device must not be unattended")
	}
	secrets.secrets[secretKey(device.ID)] = "orphaned"

	if err := service.ForgetDevice(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.devices) != 0 {
		t.Fatal("the device was not removed")
	}
	if _, held := secrets.secrets[secretKey(device.ID)]; held {
		t.Fatal("an orphaned credential outlived the device that never claimed it")
	}
}

// When the flag write fails and the compensating delete also fails, the
// caller must learn about both problems -- the enable did not take, and the
// secret may remain in the operating system store -- and the error text must
// never carry the secret itself.
func TestAFailedCompensationIsReportedAlongsideTheOriginalFailure(t *testing.T) {
	secrets := &fakeSecretStore{available: true}
	service, store := deviceService(t, secrets)
	device := registered(t, service)
	store.updateErr = errors.New("disk full")
	secrets.deleteErr = errors.New("store locked")

	const secret = "топ-секретный-пароль"
	_, err := service.EnableAutoDelivery(context.Background(), device.ID, secret)
	if err == nil {
		t.Fatal("expected the write to fail")
	}
	if !errors.Is(err, ErrSecretCompensationFailed) {
		t.Fatalf("err = %v, want it to report the failed compensation", err)
	}
	if !errors.Is(err, store.updateErr) {
		t.Fatalf("err = %v, want it to still carry the original flag-write failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %q leaked the secret", err.Error())
	}
}

// A target that left the catalog keeps its stored identity rather than being
// hidden, and it is not offered as deployable.
func TestADeviceOfAVanishedTargetStillAppears(t *testing.T) {
	service, store := deviceService(t, &fakeSecretStore{available: true})
	registered(t, service)
	store.devices[0].TargetID = "gone-device"
	cards, err := service.DeviceCards(context.Background())
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards = %#v err = %v", cards, err)
	}
	if cards[0].TargetTitle != "gone-device" || cards[0].Deployable {
		t.Fatalf("card = %#v", cards[0])
	}
}
