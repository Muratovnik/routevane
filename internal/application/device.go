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
)

// Device is a registered instance of a catalog target: what the operator calls
// it, where it is, and whether they asked for unattended delivery. The
// credential is deliberately absent -- it lives in the operating system's
// secret store or in one attempt, and nowhere this product persists.
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
	// AutoDeliver is true only after explicit consent and, when the deployer
	// needs one, while a credential is actually held for this device. It is a
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
	// before the operator is offered unattended delivery, so the offer is only
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
	// functions rather than a service so the registry does not gain a
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

// SecretStoreAvailable says whether credential-backed unattended delivery can
// be offered. A deployer that takes no credential does not depend on it.
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
		// Consent names one exact destination and account. Revoke it and remove
		// the credential before storing another connection; a failed metadata
		// write may leave the old address visible, but never opted in or secret-
		// backed.
		disabled, err := s.DisableAutoDelivery(ctx, current.ID)
		if err != nil {
			return Device{}, err
		}
		current.AutoDeliver = disabled.AutoDeliver
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
// The delete is unconditional, not gated on the AutoDeliver flag: a secret can
// outlive that flag when a flag write failed after the store already accepted
// the secret (see EnableAutoDelivery), or when the process died between the
// two writes. DeleteSecret already treats an absent credential as success, so
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

// EnableAutoDelivery stores a required credential and turns unattended
// delivery on. The order is deliberate: a credential-backed flag is only
// written after the store accepted the secret. A credential-free deployer
// removes an obsolete entry and records only the consent flag.
func (s *DeviceService) EnableAutoDelivery(ctx context.Context, id, secret string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if !s.config.Deployable(device.TargetID) {
		return Device{}, fmt.Errorf("%w: %q", ErrDeployerUnavailable, device.TargetID)
	}
	needsCredential := s.config.NeedsCredential(device.TargetID)
	if needsCredential && (secret == "" || len(secret) > 1024) {
		return Device{}, fmt.Errorf("invalid credential")
	}
	if needsCredential && !s.config.Secrets.Available() {
		return Device{}, ErrSecretStoreUnavailable
	}
	if needsCredential {
		if err := s.config.Secrets.PutSecret(ctx, secretKey(device.ID), device.Account, secret); err != nil {
			return Device{}, fmt.Errorf("store the credential: %w", err)
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
		// The secret is already in the store and the flag is not. Removing it
		// is the only state that stays true to what the operator asked for.
		// When that compensating delete also fails, the caller must learn
		// both facts -- the flag write failed, and the secret may have been
		// left behind -- without losing the original cause of the failure.
		if needsCredential {
			if compErr := s.config.Secrets.DeleteSecret(ctx, secretKey(device.ID)); compErr != nil {
				return Device{}, errors.Join(fmt.Errorf("%w: %v", ErrSecretCompensationFailed, compErr), err)
			}
		}
		return Device{}, err
	}
	return device, nil
}

// DisableAutoDelivery removes the credential and turns unattended delivery off.
// A failure to remove it is reported rather than swallowed: leaving the flag
// off while the secret stays would tell the operator their password is gone
// when it is not.
func (s *DeviceService) DisableAutoDelivery(ctx context.Context, id string) (Device, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if err := s.config.Secrets.DeleteSecret(ctx, secretKey(device.ID)); err != nil {
		return Device{}, fmt.Errorf("remove the stored credential: %w", err)
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

// DeviceCredential reads the stored credential for an unattended delivery. It
// is the only reader, and it refuses for a device the operator never marked:
// a credential left over from a disabled device must not be usable by asking.
func (s *DeviceService) DeviceCredential(ctx context.Context, id string) (string, error) {
	device, err := s.Device(ctx, id)
	if err != nil {
		return "", err
	}
	if !device.AutoDeliver {
		return "", ErrNotFound
	}
	if !s.config.NeedsCredential(device.TargetID) {
		return "", nil
	}
	return s.config.Secrets.Secret(ctx, secretKey(device.ID))
}
