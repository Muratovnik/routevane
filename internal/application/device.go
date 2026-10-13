package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrSecretStoreUnavailable is returned when the operating system offers no
	// place to keep a credential. It is not a reason to fall back to a file:
	// a password in a file this product wrote is worse than a password the
	// operator types (ADR 0014).
	ErrSecretStoreUnavailable = errors.New("the operating system offers no secret store")
	ErrDeviceComposition      = errors.New("invalid device service composition")
	// ErrSecretCompensationFailed marks a failure of the compensating delete
	// that follows a failed flag write in EnableAutoDelivery. It never carries
	// the secret itself -- only the fact that the store may still hold it.
	ErrSecretCompensationFailed = errors.New("removing the stored credential after a failed write also failed")
	// ErrCredentialRequired is returned when automatic delivery is requested for
	// a device whose deployer needs a password and none is saved (ADR 0050).
	ErrCredentialRequired = errors.New("the device needs a saved credential")
)

// Device is a registered instance of a catalog target: what the operator calls
// it, where it is, whether a password is saved for it, and whether they asked
// for unattended delivery. The credential itself is deliberately absent -- it
// lives in the operating system's secret store or in one attempt, and nowhere
// this product persists.
type Device struct {
	ID       string `json:"id"`
	TargetID string `json:"target_id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Account  string `json:"account"`
	// Interface is the device-side interface the deployer attaches routes to.
	// It is connection metadata, not a credential, and is kept with the device
	// so an unattended attempt has the same inputs as a manual one.
	Interface string `json:"interface,omitempty"`
	// CredentialStored is true while the operating system's store holds a
	// password the operator explicitly saved for this device (ADR 0050). It
	// reports a fact: a deployer that needs no credential never has one.
	CredentialStored bool `json:"credential_stored"`
	// AutoDeliver is true only after explicit consent and, when the deployer
	// needs one, while a saved credential is held for this device. It is a
	// report of stored state, never an intention.
	AutoDeliver bool      `json:"auto_deliver"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DeviceCard is one device as a screen states it, with the catalog identity
// resolved. A device whose target left the catalog still appears under its
// stored identity, because hiding it would misreport what the store holds.
type DeviceCard struct {
	Device
	TargetTitle string `json:"target_title"`
	// Deployable says this build can actually apply a file to this kind of
	// device. A target with no deployer is a download, and saying so is better
	// than offering a button that cannot work.
	Deployable bool `json:"deployable"`
}

// DeviceRepository is the stored side of the registry. A device is editable and
// removable: unlike a published artifact it promises nothing to anyone, and an
// operator who replaced their router should not be stuck with its entry.
type DeviceRepository interface {
	CreateDevice(context.Context, Device) error
	Device(context.Context, string) (Device, error)
	Devices(context.Context) ([]Device, error)
	UpdateDevice(context.Context, Device) error
	DeleteDevice(context.Context, string) error
}

// SecretStore is the operating system's credential store. It is an interface
// because the store is the platform's, and a build for a platform without one
// implements it by refusing rather than by inventing a substitute.
type SecretStore interface {
	// Available reports whether this platform has a store at all. It is asked
	// before the operator is offered to save a password, so the offer is only
	// made where it can be kept.
	Available() bool
	PutSecret(ctx context.Context, key, account, secret string) error
	Secret(ctx context.Context, key string) (string, error)
	DeleteSecret(ctx context.Context, key string) error
}

type DeviceConfig struct {
	Store   DeviceRepository
	Secrets SecretStore
	// Targets and Deployable resolve what a device is an instance of. They are
	// functions rather than a list so the registry does not gain a
	// dependency on publication or deployment.
	Targets    func() []TargetOption
	Deployable func(targetID string) bool
	// NeedsCredential distinguishes authenticated routers from local file
	// targets. Unattended delivery for the latter records consent without
	// inventing a password entry in the operating-system store.
	NeedsCredential func(targetID string) bool
	// NeedsInterface keeps deployer connection requirements authoritative even
	// for callers that bypass the browser form and use the local API directly.
	NeedsInterface func(targetID string) bool
	// ValidateStoredConnection applies the deployer's destination policy before
	// non-secret connection metadata is persisted.
	ValidateStoredConnection func(targetID string, connection Connection) error
	// RetireManagedRoutes revokes deletion authority for the old exact route
	// surface when connection metadata changes or a device is forgotten. It
	// never contacts the device.
	RetireManagedRoutes func(context.Context, string, Connection) error
	Clock               Clock
	Entropy             interface{ Read([]byte) (int, error) }
}

type DeviceService struct{ config DeviceConfig }

func NewDeviceService(config DeviceConfig) (*DeviceService, error) {
	if config.Store == nil || config.Secrets == nil || config.Targets == nil || config.Deployable == nil || config.NeedsCredential == nil || config.NeedsInterface == nil || config.ValidateStoredConnection == nil || config.RetireManagedRoutes == nil || config.Clock == nil || config.Entropy == nil {
		return nil, ErrDeviceComposition
	}
	return &DeviceService{config: config}, nil
}

// SecretStoreAvailable says whether a password can be saved. A deployer that
// takes no credential does not depend on it.
func (s *DeviceService) SecretStoreAvailable() bool { return s.config.Secrets.Available() }

const maxDeviceNameRunes = 120

func validDeviceName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxDeviceNameRunes {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

// validAddress keeps the candidate printable and bounded before the target's
// deployer applies its destination policy at the persistence boundary.
func validAddress(address string) (string, bool) {
	address = strings.TrimSpace(address)
	if address == "" || len(address) > 512 {
		return "", false
	}
	for _, r := range address {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return address, true
}

func validAccount(account string) (string, bool) {
	account = strings.TrimSpace(account)
	if utf8.RuneCountInString(account) > maxDeviceNameRunes {
		return "", false
	}
	for _, r := range account {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return account, true
}

func (s *DeviceService) knownTarget(targetID string) bool {
	for _, target := range s.config.Targets() {
		if target.ID == targetID {
			return true
		}
	}
	return false
}

// ValidateTransferDevice applies the same target and connection boundary as a
// hand-authored registration without reading or writing the credential store.
// Imported devices are persisted with automatic delivery disabled.
func (s *DeviceService) ValidateTransferDevice(candidate TransferDevice) error {
	name, ok := validDeviceName(candidate.Name)
	if !ok || name != candidate.Name {
		return fmt.Errorf("invalid device name")
	}
	address, ok := validAddress(candidate.Address)
	if !ok || address != candidate.Address {
		return fmt.Errorf("invalid device address")
	}
	account, ok := validAccount(candidate.Account)
	if !ok || account != candidate.Account {
		return fmt.Errorf("invalid device account")
	}
	interfaceName, ok := validAccount(candidate.Interface)
	if !ok || interfaceName != candidate.Interface {
		return fmt.Errorf("invalid device interface")
	}
	if !s.knownTarget(candidate.TargetID) {
		return fmt.Errorf("unknown target")
	}
	if s.config.NeedsCredential(candidate.TargetID) != (account != "") {
		return fmt.Errorf("device account does not match target requirements")
	}
	if s.config.NeedsInterface(candidate.TargetID) != (interfaceName != "") {
		return fmt.Errorf("device interface does not match target requirements")
	}
	return s.config.ValidateStoredConnection(candidate.TargetID, Connection{URL: address, Username: account, Interface: interfaceName})
}

// RegisterDevice records a device by hand. There is no discovery: a product
// that scanned the network would be doing something the operator did not ask
// for, on a network it was not invited to inspect.
func (s *DeviceService) RegisterDevice(ctx context.Context, targetID, name, address, account, interfaceName string) (Device, error) {
	cleanName, ok := validDeviceName(name)
	if !ok {
		return Device{}, fmt.Errorf("invalid device name")
	}
	cleanAddress, ok := validAddress(address)
	if !ok {
		return Device{}, fmt.Errorf("invalid device address")
	}
	cleanAccount, ok := validAccount(account)
	if !ok {
		return Device{}, fmt.Errorf("invalid device account")
	}
	cleanInterface, ok := validAccount(interfaceName)
	if !ok {
		return Device{}, fmt.Errorf("invalid device interface")
	}
	if !s.knownTarget(targetID) {
		return Device{}, fmt.Errorf("unknown target")
	}
	if s.config.NeedsCredential(targetID) != (cleanAccount != "") {
		return Device{}, fmt.Errorf("device account does not match target requirements")
	}
	if s.config.NeedsInterface(targetID) != (cleanInterface != "") {
		return Device{}, fmt.Errorf("device interface does not match target requirements")
	}
	if err := s.config.ValidateStoredConnection(targetID, Connection{URL: cleanAddress, Username: cleanAccount, Interface: cleanInterface}); err != nil {
		return Device{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		id, err := randomHex(s.config.Entropy, 16)
		if err != nil {
			return Device{}, fmt.Errorf("generate device identity: %w", err)
		}
		device := Device{ID: id, TargetID: targetID, Name: cleanName, Address: cleanAddress, Account: cleanAccount, Interface: cleanInterface, CreatedAt: now, UpdatedAt: now}
		if err := s.config.Store.CreateDevice(ctx, device); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return Device{}, err
		}
		return device, nil
	}
	return Device{}, ErrIdentityCollision
}

func (s *DeviceService) Device(ctx context.Context, id string) (Device, error) {
	if !isHexID(id) {
		return Device{}, ErrNotFound
	}
	return s.config.Store.Device(ctx, id)
}

// UpdateDevice replaces the editable parts. The target is not one of them: a
// device that became a different kind of device is a different device, and
// changing it under a stored credential would point that credential somewhere
// the operator never approved.
func (s *DeviceService) UpdateDevice(ctx context.Context, id, name, address, account, interfaceName string) (Device, error) {
	current, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	cleanName, ok := validDeviceName(name)
	if !ok {
		return Device{}, fmt.Errorf("invalid device name")
	}
	cleanAddress, ok := validAddress(address)
	if !ok {
		return Device{}, fmt.Errorf("invalid device address")
	}
	cleanAccount, ok := validAccount(account)
	if !ok {
		return Device{}, fmt.Errorf("invalid device account")
	}
	cleanInterface, ok := validAccount(interfaceName)
	if !ok {
		return Device{}, fmt.Errorf("invalid device interface")
	}
	if s.config.NeedsCredential(current.TargetID) != (cleanAccount != "") {
		return Device{}, fmt.Errorf("device account does not match target requirements")
	}
	if s.config.NeedsInterface(current.TargetID) != (cleanInterface != "") {
		return Device{}, fmt.Errorf("device interface does not match target requirements")
	}
	if err := s.config.ValidateStoredConnection(current.TargetID, Connection{URL: cleanAddress, Username: cleanAccount, Interface: cleanInterface}); err != nil {
		return Device{}, err
	}
	connectionChanged := current.Address != cleanAddress || current.Account != cleanAccount || current.Interface != cleanInterface
	if connectionChanged {
		// A saved credential and consent name one exact destination and account.
		// Revoke both and remove the credential before storing another
		// connection; a failed metadata write may leave the old address visible,
		// but never opted in or secret-backed.
		revoked, err := s.revokeCredentialAndConsent(ctx, current)
		if err != nil {
			return Device{}, err
		}
		current.AutoDeliver, current.CredentialStored = revoked.AutoDeliver, revoked.CredentialStored
		if err := s.config.RetireManagedRoutes(ctx, current.TargetID, Connection{URL: current.Address, Username: current.Account, Interface: current.Interface}); err != nil {
			return Device{}, fmt.Errorf("retire managed routes: %w", err)
		}
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, fmt.Errorf("clock returned zero time")
	}
	current.Name, current.Address, current.Account, current.Interface, current.UpdatedAt = cleanName, cleanAddress, cleanAccount, cleanInterface, now
	if err := s.config.Store.UpdateDevice(ctx, current); err != nil {
		return Device{}, err
	}
	return current, nil
}

// DeviceCards lists every registered device with its catalog identity resolved.
func (s *DeviceService) DeviceCards(ctx context.Context) ([]DeviceCard, error) {
	devices, err := s.config.Store.Devices(ctx)
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, 8)
	for _, target := range s.config.Targets() {
		titles[target.ID] = target.Title
	}
	cards := make([]DeviceCard, 0, len(devices))
	for _, device := range devices {
		title, known := titles[device.TargetID]
		if !known {
			// The stored identity is the honest answer for a target that left
			// the catalog. Inventing a friendly name would hide the fact.
			title = device.TargetID
		}
		cards = append(cards, DeviceCard{Device: device, TargetTitle: title, Deployable: known && s.config.Deployable(device.TargetID)})
	}
	return cards, nil
}

// ForgetDevice removes the entry and the credential it may hold. The credential
// goes first: a failure there must leave the device visible, because a device
// nobody can see is a credential nobody can revoke.
//
// The delete is unconditional, not gated on the CredentialStored flag: a secret
// can outlive that flag when a flag write failed after the store already
// accepted the secret (see SaveCredential), or when the process died between
// the two writes. DeleteSecret already treats an absent credential as success, so
// asking it to remove nothing costs nothing.
func (s *DeviceService) ForgetDevice(ctx context.Context, id string) error {
	device, err := s.Device(ctx, id)
	if err != nil {
		return err
	}
	if err := s.config.Secrets.DeleteSecret(ctx, secretKey(device.ID)); err != nil {
		return fmt.Errorf("remove the stored credential: %w", err)
	}
	if err := s.config.RetireManagedRoutes(ctx, device.TargetID, Connection{URL: device.Address, Username: device.Account, Interface: device.Interface}); err != nil {
		return fmt.Errorf("retire managed routes: %w", err)
	}
	return s.config.Store.DeleteDevice(ctx, id)
}

// secretKey is the name this product uses in the operating system's store. It
// carries the product and the device identity so an operator reading their
// credential manager can tell what an entry is and delete it themselves.
func secretKey(deviceID string) string { return "Routevane device " + deviceID }

const maxCredentialBytes = 1024

// SaveCredential keeps a password for one device in the operating system's
// store and records that fact (ADR 0050). Saving is the operator's explicit
// consent to storage and is independent of automatic delivery. A saved
// credential replaces any earlier one. A deployer that takes no credential has
// nothing to save, so the flag can never claim a password that does not exist.
func (s *DeviceService) SaveCredential(ctx context.Context, id, secret string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if !s.config.Deployable(device.TargetID) {
		return Device{}, fmt.Errorf("%w: %q", ErrDeployerUnavailable, device.TargetID)
	}
	if !s.config.NeedsCredential(device.TargetID) {
		return Device{}, fmt.Errorf("the device's deployer takes no credential")
	}
	if secret == "" || len(secret) > maxCredentialBytes {
		return Device{}, fmt.Errorf("invalid credential")
	}
	if !s.config.Secrets.Available() {
		return Device{}, ErrSecretStoreUnavailable
	}
	if err := s.config.Secrets.PutSecret(ctx, secretKey(device.ID), device.Account, secret); err != nil {
		return Device{}, fmt.Errorf("store the credential: %w", err)
	}
	if device.CredentialStored {
		// The store now holds the replacement and the recorded fact was already
		// true, so there is nothing to write.
		return device, nil
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, s.compensateStoredSecret(ctx, device.ID, fmt.Errorf("clock returned zero time"))
	}
	device.CredentialStored, device.UpdatedAt = true, now
	if err := s.config.Store.UpdateDevice(ctx, device); err != nil {
		return Device{}, s.compensateStoredSecret(ctx, device.ID, err)
	}
	return device, nil
}

// compensateStoredSecret removes a secret whose flag was never recorded. It is
// used only when the device had no credential before, because a replaced
// secret cannot be restored and its flag is already correct. A failed delete
// is reported together with the original failure, never with the secret.
func (s *DeviceService) compensateStoredSecret(ctx context.Context, id string, cause error) error {
	if compErr := s.config.Secrets.DeleteSecret(ctx, secretKey(id)); compErr != nil {
		return errors.Join(fmt.Errorf("%w: %v", ErrSecretCompensationFailed, compErr), cause)
	}
	return cause
}

// RemoveCredential deletes the saved password. The secret goes first: a
// failure keeps the recorded fact true rather than claiming a deletion that did
// not happen. When the deployer needs a credential, automatic delivery cannot
// continue without it and is turned off in the same write.
func (s *DeviceService) RemoveCredential(ctx context.Context, id string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	return s.clearCredential(ctx, device, s.config.NeedsCredential(device.TargetID))
}

// revokeCredentialAndConsent is the ADR 0031 rule for a changed connection: the
// credential and the consent both name the old destination, whatever the
// deployer needs.
func (s *DeviceService) revokeCredentialAndConsent(ctx context.Context, device Device) (Device, error) {
	return s.clearCredential(ctx, device, true)
}

// clearCredential deletes the secret unconditionally, because one can outlive
// its flag (a crash between the two writes), and records that none is held.
func (s *DeviceService) clearCredential(ctx context.Context, device Device, disableAuto bool) (Device, error) {
	if err := s.config.Secrets.DeleteSecret(ctx, secretKey(device.ID)); err != nil {
		return Device{}, fmt.Errorf("remove the stored credential: %w", err)
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, fmt.Errorf("clock returned zero time")
	}
	device.CredentialStored, device.UpdatedAt = false, now
	if disableAuto {
		device.AutoDeliver = false
	}
	if err := s.config.Store.UpdateDevice(ctx, device); err != nil {
		return Device{}, err
	}
	return device, nil
}

// EnableAutoDelivery turns unattended delivery on. A deployer that needs a
// credential requires one to be saved already (ADR 0050); a non-empty secret is
// saved first, the same as SaveCredential, so a caller that sends both in one
// request still works. A deployer that takes no credential removes an obsolete
// entry and records only the consent flag.
func (s *DeviceService) EnableAutoDelivery(ctx context.Context, id, secret string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if !s.config.Deployable(device.TargetID) {
		return Device{}, fmt.Errorf("%w: %q", ErrDeployerUnavailable, device.TargetID)
	}
	if s.config.NeedsCredential(device.TargetID) {
		if secret != "" {
			if device, err = s.SaveCredential(ctx, id, secret); err != nil {
				return Device{}, err
			}
		}
		if !device.CredentialStored {
			return Device{}, ErrCredentialRequired
		}
	} else if err := s.config.Secrets.DeleteSecret(ctx, secretKey(device.ID)); err != nil {
		return Device{}, fmt.Errorf("remove an obsolete stored credential: %w", err)
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, fmt.Errorf("clock returned zero time")
	}
	device.AutoDeliver, device.UpdatedAt = true, now
	if err := s.config.Store.UpdateDevice(ctx, device); err != nil {
		return Device{}, err
	}
	return device, nil
}

// DisableAutoDelivery turns unattended delivery off and keeps the saved
// credential: the password is its own fact (ADR 0050) and manual delivery
// still uses it. Removing it is RemoveCredential.
func (s *DeviceService) DisableAutoDelivery(ctx context.Context, id string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Device{}, fmt.Errorf("clock returned zero time")
	}
	device.AutoDeliver, device.UpdatedAt = false, now
	if err := s.config.Store.UpdateDevice(ctx, device); err != nil {
		return Device{}, err
	}
	return device, nil
}

// DeviceCredential reads the saved credential for one device. It is the only
// reader. It is gated on the recorded fact, not on automatic delivery: a manual
// delivery uses it too, and the scheduler checks consent itself before asking.
// A secret left behind without the fact -- a failed or interrupted write -- is
// not usable by asking. A deployer that takes no credential needs none.
func (s *DeviceService) DeviceCredential(ctx context.Context, id string) (string, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return "", err
	}
	if !s.config.NeedsCredential(device.TargetID) {
		return "", nil
	}
	if !device.CredentialStored {
		return "", ErrNotFound
	}
	return s.config.Secrets.Secret(ctx, secretKey(device.ID))
}
