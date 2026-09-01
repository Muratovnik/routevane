// Package singboxlocal applies an already-validated sing-box rule-set artifact
// to a sing-box installation on this machine.
//
// It is the only deployer that reaches nothing: there is no address, no
// credential, and no network call in this package. The destination is a file
// path the operator's own sing-box configuration already declares, and the whole
// deployment is a bounded read, an atomic write, and a read-back.
//
// What it verifies and what it does not:
//
//   - It proves the file on disk is byte-identical to the published artifact and
//     that the artifact still satisfies the sing-box source rule-set validator.
//   - It does not prove a running sing-box reloaded the file. sing-box reads a
//     local rule-set when its configuration is loaded, so the operator restarts
//     or reloads the process. Claiming otherwise would need a control API this
//     package deliberately does not speak.
package singboxlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// DeployerID is keyed to the renderer whose artifacts this deployer installs.
const DeployerID = singbox.ID

const (
	// MaxConfigBytes bounds the configuration this deployer reads. A sing-box
	// configuration is hand-written; a file this large is not one.
	MaxConfigBytes = 4 << 20
	// MaxRuleSetBytes bounds the rule-set file read back for verification and
	// stored as a backup.
	MaxRuleSetBytes = singbox.MaxArtifactSize
	// Scheme is the only connection scheme this deployer accepts.
	Scheme = "file://"
	// localFormat and localType are the rule-set declarations this deployer can
	// write. Any other combination means the configuration expects different
	// bytes than the artifact carries.
	localFormat = "source"
	localType   = "local"
	// Vendor is what a local installation can honestly report as its maker.
	Vendor = "sing-box"
)

var (
	ErrConnectionUnsupported = errors.New("connection is not a local sing-box configuration")
	ErrConfigUnusable        = errors.New("sing-box configuration is not usable")
	ErrRuleSetMissing        = errors.New("sing-box configuration declares no writable local rule set")
	ErrArtifactMismatch      = errors.New("artifact is not a sing-box source rule set")
	ErrBackupUnusable        = errors.New("backup is not usable for a rollback")
	ErrVerifyMismatch        = errors.New("the file on disk does not match the artifact")
)

// Deployer installs a rule-set artifact into a local sing-box configuration.
type Deployer struct{}

// New returns the deployer. It has no options: everything it needs comes from
// the configuration the operator already wrote.
func New() *Deployer { return &Deployer{} }

func (*Deployer) ID() string { return DeployerID }

// Requirements describes a destination on this machine: a configuration path and
// nothing else. Declaring no credential is what stops a caller from asking for
// one that would have nowhere to go.
func (*Deployer) Requirements() application.ConnectionRequirements {
	return application.ConnectionRequirements{
		AddressLabel:    "Путь к конфигурации sing-box",
		AddressExample:  "file:///C:/sing-box/config.json",
		NeedsCredential: false,
		NeedsInterface:  false,
	}
}

// ValidateConnection accepts a file URL naming an absolute configuration path
// and refuses a credential. A local deployment that carried a password would
// invite an operator to put one where nothing can use it.
func (d *Deployer) ValidateConnection(connection application.Connection) error {
	return d.ValidateStoredConnection(connection)
}

// ValidateStoredConnection is identical to full validation because this local
// transport never accepts a secret. The name makes the persistence boundary
// explicit to the device registry.
func (*Deployer) ValidateStoredConnection(connection application.Connection) error {
	if _, err := configPath(connection.URL); err != nil {
		return err
	}
	if connection.Username != "" || connection.Password != "" {
		return fmt.Errorf("%w: a local deployment takes no credential", ErrConnectionUnsupported)
	}
	if connection.Interface != "" {
		// The outbound a rule set selects is decided by the configuration, not
		// by this command.
		return fmt.Errorf("%w: a local deployment takes no interface", ErrConnectionUnsupported)
	}
	return nil
}

// configPath turns the connection URL into an absolute configuration path.
func configPath(raw string) (string, error) {
	if strings.TrimSpace(raw) != raw || !strings.HasPrefix(raw, Scheme) || len(raw) <= len(Scheme) || strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("%w: expected a %s path", ErrConnectionUnsupported, Scheme)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" || parsed.User != nil || parsed.Host != "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path == "" {
		return "", fmt.Errorf("%w: expected a local file URL without authority, query, or fragment", ErrConnectionUnsupported)
	}
	trimmed := parsed.Path
	// A file URL always separates with forward slashes, including on Windows,
	// where it also carries a leading slash before the drive letter.
	if len(trimmed) > 2 && trimmed[0] == '/' && trimmed[2] == ':' {
		trimmed = trimmed[1:]
	}
	path := filepath.FromSlash(trimmed)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: the configuration path must be absolute", ErrConnectionUnsupported)
	}
	if strings.HasPrefix(path, `\\`) {
		return "", fmt.Errorf("%w: a network path is not local", ErrConnectionUnsupported)
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return "", fmt.Errorf("%w: the configuration path must be in canonical form", ErrConnectionUnsupported)
	}
	return cleaned, nil
}

// wireConfig is the narrow projection of a sing-box configuration this deployer
// reads. Everything else in the file is none of its business, so unknown fields
// are ignored rather than refused.
type wireConfig struct {
	Route struct {
		RuleSet []struct {
			Tag    string `json:"tag"`
			Type   string `json:"type"`
			Format string `json:"format"`
			Path   string `json:"path"`
		} `json:"rule_set"`
	} `json:"route"`
}

// Probe reads the configuration and reports the one local source rule set this
// deployer can write.
//
// There is no firmware to interrogate on this machine, so the compatibility fact
// is the configuration's own declaration: a rule set of type local and format
// source is what accepts the bytes this product publishes. A configuration that
// declares anything else reports an empty profile key, which is what stops the
// deployment before a file is touched.
func (*Deployer) Probe(_ context.Context, connection application.Connection) (application.DeviceInfo, error) {
	path, err := configPath(connection.URL)
	if err != nil {
		return application.DeviceInfo{}, err
	}
	payload, err := readBounded(path, MaxConfigBytes)
	if err != nil {
		return application.DeviceInfo{}, fmt.Errorf("%w: %v", ErrConfigUnusable, err)
	}
	var config wireConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return application.DeviceInfo{}, fmt.Errorf("%w: %v", ErrConfigUnusable, err)
	}
	info := application.DeviceInfo{Vendor: Vendor, Model: filepath.Base(path)}
	for _, entry := range config.Route.RuleSet {
		if entry.Type != localType || entry.Format != localFormat || entry.Tag == "" || entry.Path == "" {
			continue
		}
		resolved := entry.Path
		if !filepath.IsAbs(resolved) {
			// A relative rule-set path is resolved against the configuration, as
			// sing-box itself does.
			resolved = filepath.Join(filepath.Dir(path), resolved)
		}
		resolved = filepath.Clean(resolved)
		if info.Interface != "" && info.Interface != resolved {
			return application.DeviceInfo{}, fmt.Errorf("%w: the configuration declares more than one local source rule set", ErrConfigUnusable)
		}
		info.Interface = resolved
		info.FirmwareVersion = entry.Tag
		info.ProfileKey = singbox.Version
	}
	if info.ProfileKey == "" {
		return info, ErrRuleSetMissing
	}
	return info, nil
}

// backupEnvelope is what a rollback needs: the exact previous state of the file,
// including the case where there was no file. Storing only bytes would make an
// absent file indistinguishable from an empty one.
type backupEnvelope struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256"`
	Payload []byte `json:"payload,omitempty"`
}

// Backup captures the rule-set file the deployment will replace.
func (*Deployer) Backup(_ context.Context, device application.DeviceInfo, _ application.Connection) (application.BackupPayload, error) {
	path := device.Interface
	if path == "" {
		return application.BackupPayload{}, ErrRuleSetMissing
	}
	envelope := backupEnvelope{Path: path}
	payload, err := readBounded(path, MaxRuleSetBytes)
	switch {
	case err == nil:
		digest := sha256.Sum256(payload)
		envelope.Present = true
		envelope.SHA256 = hex.EncodeToString(digest[:])
		envelope.Payload = payload
	case errors.Is(err, os.ErrNotExist):
		// A first deployment has nothing to preserve, and that absence is itself
		// what a rollback must restore.
		envelope.Present = false
	default:
		return application.BackupPayload{}, fmt.Errorf("%w: %v", ErrBackupUnusable, err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return application.BackupPayload{}, fmt.Errorf("%w: %v", ErrBackupUnusable, err)
	}
	return application.BackupPayload{Payload: encoded}, nil
}

// Deploy writes the artifact atomically so a crash cannot leave a partial file
// where sing-box expects a complete rule set.
func (*Deployer) Deploy(_ context.Context, device application.DeviceInfo, _ application.Connection, artifact application.DeployArtifact) error {
	if artifact.RendererID != DeployerID {
		return fmt.Errorf("%w: renderer %q", ErrArtifactMismatch, artifact.RendererID)
	}
	// The publication path already validated these bytes; validating again here
	// is what makes it impossible for this deployer to be the step that writes
	// something sing-box cannot read.
	if err := singbox.Validate(artifact.Payload); err != nil {
		return fmt.Errorf("%w: %v", ErrArtifactMismatch, err)
	}
	path := device.Interface
	if path == "" {
		return ErrRuleSetMissing
	}
	return writeAtomic(path, artifact.Payload)
}

// Verify reads the file back and proves it is the artifact, byte for byte.
func (*Deployer) Verify(_ context.Context, device application.DeviceInfo, _ application.Connection, artifact application.DeployArtifact) error {
	payload, err := readBounded(device.Interface, MaxRuleSetBytes)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVerifyMismatch, err)
	}
	if len(payload) != len(artifact.Payload) {
		return fmt.Errorf("%w: %d bytes on disk, %d published", ErrVerifyMismatch, len(payload), len(artifact.Payload))
	}
	for index := range payload {
		if payload[index] != artifact.Payload[index] {
			return fmt.Errorf("%w: the file differs at byte %d", ErrVerifyMismatch, index)
		}
	}
	if err := singbox.Validate(payload); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifyMismatch, err)
	}
	return nil
}

// Rollback restores exactly what Backup captured, including an absent file.
func (*Deployer) Rollback(_ context.Context, device application.DeviceInfo, _ application.Connection, backup application.BackupPayload) error {
	var envelope backupEnvelope
	if err := json.Unmarshal(backup.Payload, &envelope); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupUnusable, err)
	}
	if envelope.Path == "" || envelope.Path != device.Interface {
		return fmt.Errorf("%w: the backup describes a different file", ErrBackupUnusable)
	}
	if !envelope.Present {
		if err := os.Remove(envelope.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrBackupUnusable, err)
		}
		return nil
	}
	digest := sha256.Sum256(envelope.Payload)
	if hex.EncodeToString(digest[:]) != envelope.SHA256 {
		return fmt.Errorf("%w: the stored bytes do not match their digest", ErrBackupUnusable)
	}
	return writeAtomic(envelope.Path, envelope.Payload)
}

// readBounded reads a file through a root scoped to its own directory and
// refuses anything past the bound, so neither a symlink nor a large file can
// pull this deployer somewhere it was not pointed.
func readBounded(path string, limit int) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("a bounded read needs an absolute path")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("%s is larger than the %d byte bound", path, limit)
	}
	payload, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > limit {
		return nil, fmt.Errorf("%s is larger than the %d byte bound", path, limit)
	}
	return payload, nil
}

// writeAtomic writes through a temporary file in the destination directory and
// renames it into place, so a reader never observes a partial rule set.
func writeAtomic(path string, payload []byte) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, filepath.Base(path)+".routevane-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	written, writeErr := file.Write(payload)
	chmodErr := file.Chmod(0o600)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || chmodErr != nil || syncErr != nil || closeErr != nil || written != len(payload) {
		_ = os.Remove(temporary)
		return errors.Join(writeErr, chmodErr, syncErr, closeErr)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
