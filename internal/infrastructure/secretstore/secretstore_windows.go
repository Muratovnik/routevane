//go:build windows

// Package secretstore keeps a device credential where the operating system
// keeps credentials, and nowhere else. There is no file fallback: a password in
// a file this product wrote is worse than a password the operator types, so a
// platform without a store refuses unattended delivery instead (ADR 0014).
package secretstore

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNotFound is returned when the store holds nothing under this key. It is
// separate from a failure: an absent credential is a fact the caller acts on.
var ErrNotFound = errors.New("no credential is stored under this key")

// Store is Windows Credential Manager, reached through advapi32. It is the
// operating system's own store: an operator can see, audit and delete what this
// product wrote using a tool they already have.
type Store struct{}

func New() Store { return Store{} }

// Available reports that this platform has a store. The Windows build always
// does; the fallback build for every other platform reports otherwise.
func (Store) Available() bool { return true }

const (
	credTypeGeneric      = 1
	credPersistLocalUser = 2
	// maxSecretBytes is the documented Credential Manager blob bound. A larger
	// secret is refused rather than truncated: half a password stored is a
	// password that fails at the worst moment.
	maxSecretBytes = 2560
)

var (
	advapi32     = windows.NewLazySystemDLL("advapi32.dll")
	credWriteW   = advapi32.NewProc("CredWriteW")
	credReadW    = advapi32.NewProc("CredReadW")
	credDeleteW  = advapi32.NewProc("CredDeleteW")
	credFreeProc = advapi32.NewProc("CredFree")
)

// credential mirrors CREDENTIALW. The layout is fixed by the platform, so the
// field order here is not a style choice.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func (Store) PutSecret(_ context.Context, key, account, secret string) error {
	if key == "" || secret == "" {
		return fmt.Errorf("invalid credential")
	}
	blob := []byte(secret)
	if len(blob) > maxSecretBytes {
		return fmt.Errorf("credential exceeds the %d byte bound the store accepts", maxSecretBytes)
	}
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("invalid credential key: %w", err)
	}
	// An empty account is legitimate: some devices authenticate with a password
	// alone. The store still wants a pointer, so it gets an empty string.
	user, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return fmt.Errorf("invalid credential account: %w", err)
	}
	entry := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)), // #nosec G115 -- the bound above refuses anything over maxSecretBytes, so this fits.
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalUser,
		UserName:           user,
	}
	// #nosec G103 -- CredWriteW takes a CREDENTIALW pointer; a syscall binding
	// is the only way to reach the operating system store this ADR requires.
	status, _, callErr := credWriteW.Call(uintptr(unsafe.Pointer(&entry)), 0)
	if status == 0 {
		return fmt.Errorf("the credential store refused the write: %w", callErr)
	}
	return nil
}

func (Store) Secret(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("invalid credential key")
	}
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return "", fmt.Errorf("invalid credential key: %w", err)
	}
	var entry *credential
	// #nosec G103 -- CredReadW writes a CREDENTIALW pointer the platform owns.
	status, _, callErr := credReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&entry)))
	if status == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("the credential store refused the read: %w", callErr)
	}
	// #nosec G103 -- the buffer belongs to the platform and CredFree is how it
	// is returned; not calling it would leak the credential into this process.
	defer func() { _, _, _ = credFreeProc.Call(uintptr(unsafe.Pointer(entry))) }()
	if entry == nil || entry.CredentialBlob == nil || entry.CredentialBlobSize == 0 {
		return "", ErrNotFound
	}
	if entry.CredentialBlobSize > maxSecretBytes {
		return "", fmt.Errorf("the stored credential exceeds the %d byte bound", maxSecretBytes)
	}
	// #nosec G103 -- the blob is a length-prefixed platform buffer, and the
	// length was bounded above before it is read.
	return string(unsafe.Slice(entry.CredentialBlob, entry.CredentialBlobSize)), nil
}

// DeleteSecret removes the entry. An entry that is already absent is success:
// the caller asked for it to be gone, and it is.
func (Store) DeleteSecret(_ context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("invalid credential key")
	}
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("invalid credential key: %w", err)
	}
	// #nosec G103 -- CredDeleteW takes the same UTF-16 target name.
	status, _, callErr := credDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if status == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return fmt.Errorf("the credential store refused the delete: %w", callErr)
	}
	return nil
}
