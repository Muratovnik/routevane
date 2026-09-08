package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrDeployComposition   = errors.New("invalid deployment composition")
	ErrDeviceIncompatible  = errors.New("device is not compatible with the target format")
	ErrBackupRequired      = errors.New("deployment requires a verified backup")
	ErrDeployFailed        = errors.New("deployment failed")
	ErrVerifyFailed        = errors.New("deployment verification failed")
	ErrRollbackFailed      = errors.New("rollback failed after a failed deployment")
	ErrOwnershipPersist    = errors.New("managed route ownership could not be persisted")
	ErrDeployerUnavailable = errors.New("no deployer registered for the target")
	ErrConnectionInvalid   = errors.New("connection is not usable by this deployer")
)

// Connection is how to reach one device. It carries a credential, so it is
// deliberately a separate argument rather than a field of DeviceInfo: nothing
// that is logged, serialized, or returned may hold it.
type Connection struct {
	// URL is the device's local address. It is always explicit.
	URL string
	// Username and Password authenticate to the device.
	Username string
	Password string
	// Interface is the device-side interface routes are attached to.
	Interface string
}

// Redacted returns the connection without its secret, for audit records.
func (c Connection) Redacted() Connection {
	c.Password = ""
	return c
}

// DeviceInfo is what a probe learned. It never contains a credential.
type DeviceInfo struct {
	DeployerID      string `json:"deployer_id"`
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version"`
	// FormatKey is the target format this firmware is compatible with. A
	// device that reports an unsupported version reports an empty value, which
	// is what stops a deployment before anything is changed.
	FormatKey string `json:"format_key"`
	// Interface is the interface the deployer will attach routes to.
	Interface string `json:"interface"`
}

// BackupRef identifies one stored device backup. The bytes live outside this
// value so a reference can be logged.
type BackupRef struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// Verified reports whether a backup reference is usable for a rollback.
func (b BackupRef) Verified() bool {
	return b.ID != "" && b.Path != "" && b.Hash != "" && b.SizeBytes > 0 && !b.CreatedAt.IsZero()
}

// DeployArtifact is the already-validated artifact to install. The deployment
// path never renders and never validates format: it installs bytes that the
// publication path already proved.
type DeployArtifact struct {
	ArtifactID   string
	RendererID   string
	ArtifactHash string
	ContentType  string
	Payload      []byte
	// PlanSnapshotID and RoutingPlanHash identify what the bytes represent.
	PlanSnapshotID  string
	RoutingPlanHash string
	// PlanSnapshot carries the immutable renderer-neutral decision so a device
	// transport can attach provenance without changing the published artifact.
	PlanSnapshot []byte
	// OwnedFQDNGroups is trusted persisted creation evidence supplied by the
	// lifecycle, never inferred from artifact names or device observations.
	OwnedFQDNGroups []ManagedFQDNGroup
}

// ConnectionRequirements is what a deployer needs before it can be used. A
// screen or a command asks for exactly these fields, so an operator is never
// prompted for a credential that has nowhere to go, and never left guessing
// which address form a target accepts.
type ConnectionRequirements struct {
	// AddressLabel names what the address is, and AddressExample shows one.
	AddressLabel   string `json:"address_label"`
	AddressExample string `json:"address_example"`
	// NeedsCredential and NeedsInterface say whether this transport
	// authenticates and whether it attaches to a named interface.
	NeedsCredential bool `json:"needs_credential"`
	NeedsInterface  bool `json:"needs_interface"`
	// InterfaceLabel names the interface when one is required.
	InterfaceLabel string `json:"interface_label,omitempty"`
}

// Deployer applies one already-validated artifact to one device.
//
// The interface deliberately takes the Connection on every call instead of
// hiding it inside DeviceInfo: DeviceInfo is reported and stored, and a value
// that is reported must not be able to carry a password.
type Deployer interface {
	ID() string
	// Requirements describes the connection this deployer needs. It is a
	// statement about the transport, so a caller can build the right form or
	// command line without knowing which deployer it is talking to.
	Requirements() ConnectionRequirements
	// ValidateStoredConnection validates only the non-secret connection metadata
	// that may be persisted for unattended delivery. A password is refused here:
	// it belongs in the operating-system secret store, never in Device.
	ValidateStoredConnection(Connection) error
	// ValidateConnection refuses a connection this deployer cannot use, before
	// anything is probed or changed. What a connection must carry is a property
	// of the transport: a device reached over an authenticated interface needs a
	// credential, a destination on this machine must not be given one.
	ValidateConnection(Connection) error
	// Probe reports what the device is. It must refuse an unsupported firmware
	// by returning an empty FormatKey rather than guessing.
	Probe(context.Context, Connection) (DeviceInfo, error)
	Backup(context.Context, DeviceInfo, Connection) (BackupPayload, error)
	Deploy(context.Context, DeviceInfo, Connection, DeployArtifact) error
	Verify(context.Context, DeviceInfo, Connection, DeployArtifact) error
	Rollback(context.Context, DeviceInfo, Connection, BackupPayload) error
}

// BackupPayload is a device backup in memory plus the reference to its stored
// copy. The bytes are needed for a rollback and are never logged.
type BackupPayload struct {
	Ref     BackupRef
	Payload []byte
}

// DeployerRegistry resolves a target's renderer to the deployer that can install
// its artifacts. It is keyed by renderer id because what can be installed is
// decided by the format, not by the device label.
type DeployerRegistry map[string]Deployer

// BackupStore persists a device backup outside the process.
type BackupStore interface {
	PutBackup(context.Context, string, time.Time, []byte) (BackupRef, error)
}

// rollbackBudget is how long a rollback may take, counted from the failure that
// triggered it rather than from the start of the request that asked for the
// deployment. It is deliberately generous: putting a device back is the step
// that must not be the one to run out of time.
const rollbackBudget = 2 * time.Minute

// DeployStep identities used in the audit trail.
const (
	StepProbe     = "probe"
	StepBackup    = "backup"
	StepDeploy    = "deploy"
	StepVerify    = "verify"
	StepOwnership = "ownership"
	StepRollback  = "rollback"
)

// DeployEvent is one audited step. It contains identities and outcomes only:
// never a credential, never device configuration content.
type DeployEvent struct {
	Step     string        `json:"step"`
	Outcome  string        `json:"outcome"`
	Detail   string        `json:"detail,omitempty"`
	Duration time.Duration `json:"duration_ns"`
}

// DeployResult is the audited outcome of one deployment attempt.
type DeployResult struct {
	Device     DeviceInfo    `json:"device"`
	Backup     BackupRef     `json:"backup"`
	ArtifactID string        `json:"artifact_id"`
	Applied    bool          `json:"applied"`
	RolledBack bool          `json:"rolled_back"`
	Events     []DeployEvent `json:"events"`
}

// DeployRequest is one deployment attempt.
type DeployRequest struct {
	Target     domain.TargetDefinition
	Connection Connection
	Artifact   DeployArtifact
	// OutputID is the stable claim identity for an ownership-aware deployer.
	OutputID string
	// ManagedRoutes is optional. Without a ledger, an ownership-aware deployer
	// remains additive and cannot infer deletion authority from an interface.
	ManagedRoutes ManagedRouteRepository
	ManagedFQDN   ManagedFQDNRepository
}

// DeployToDevice applies one validated artifact in the only order that is safe:
// probe, compatibility, backup, deploy, verify, and rollback when verification
// fails.
//
// Each ordering rule exists because skipping it makes a failure unrecoverable:
// without a probe the firmware may not accept the format, without a backup a
// rollback is impossible, and without a verify a silent partial write would look
// like success.
func DeployToDevice(ctx context.Context, request DeployRequest, deployers DeployerRegistry, backups BackupStore, clock Clock) (DeployResult, error) {
	result := DeployResult{ArtifactID: request.Artifact.ArtifactID}
	if ctx == nil || clock == nil || backups == nil || len(deployers) == 0 {
		return result, ErrDeployComposition
	}
	if request.Target.ID == "" || request.Target.RendererID == "" || request.Target.FormatKey == "" {
		return result, ErrDeployComposition
	}
	if request.Artifact.ArtifactID == "" || len(request.Artifact.Payload) == 0 || request.Artifact.ArtifactHash == "" ||
		request.Artifact.RendererID != request.Target.RendererID {
		return result, ErrDeployComposition
	}
	if request.Connection.URL == "" {
		return result, ErrDeployComposition
	}
	deployer, registered := deployers[request.Target.RendererID]
	if !registered || deployer == nil {
		return result, fmt.Errorf("%w: %q", ErrDeployerUnavailable, request.Target.RendererID)
	}
	if err := deployer.ValidateConnection(request.Connection); err != nil {
		return result, fmt.Errorf("%w: %v", ErrConnectionInvalid, err)
	}

	record := func(step string, started time.Time, err error, detail string) {
		outcome := "success"
		if err != nil {
			outcome = "failed"
		}
		result.Events = append(result.Events, DeployEvent{Step: step, Outcome: outcome, Detail: detail, Duration: clock.Now().Sub(started)})
	}

	started := clock.Now()
	device, err := deployer.Probe(ctx, request.Connection)
	record(StepProbe, started, err, device.FirmwareVersion)
	if err != nil {
		return result, fmt.Errorf("%w: probe: %v", ErrDeployFailed, err)
	}
	device.DeployerID = deployer.ID()
	result.Device = device
	// An incompatible firmware is refused before anything on the device changes.
	if device.FormatKey == "" || device.FormatKey != request.Target.FormatKey {
		return result, fmt.Errorf("%w: firmware %q reports format %q, target requires %q", ErrDeviceIncompatible, device.FirmwareVersion, device.FormatKey, request.Target.FormatKey)
	}

	managedDeployer, supportsManagedRoutes := deployer.(ManagedRouteDeployer)
	fqdnDeployer, supportsFQDN := deployer.(ManagedFQDNDeployer)
	var nextFQDN ManagedFQDNOwnership
	if supportsFQDN {
		if request.ManagedFQDN == nil || !isHexID(request.OutputID) {
			return result, fmt.Errorf("%w: FQDN ownership ledger required", ErrDeployComposition)
		}
		endpoint, scopeErr := fqdnDeployer.FQDNEndpoint(request.Connection.Redacted())
		if scopeErr != nil {
			return result, scopeErr
		}
		prior, readErr := request.ManagedFQDN.ManagedFQDNOwnership(ctx, endpoint, request.OutputID)
		if readErr != nil {
			return result, fmt.Errorf("%w: read FQDN ownership: %v", ErrDeployComposition, readErr)
		}
		if prior.Endpoint != endpoint || prior.OutputID != request.OutputID || prior.Validate() != nil {
			return result, ErrDeployComposition
		}
		request.Artifact.OwnedFQDNGroups = prior.Groups
		groups, desiredErr := fqdnDeployer.DesiredFQDNGroups(device, request.Artifact)
		if desiredErr != nil {
			return result, fmt.Errorf("%w: %v", ErrDeployComposition, desiredErr)
		}
		nextFQDN = ManagedFQDNOwnership{Endpoint: endpoint, OutputID: request.OutputID, Groups: groups}
		if _, previewErr := fqdnDeployer.PreviewFQDNGroups(ctx, device, request.Connection, request.Artifact); previewErr != nil {
			return result, fmt.Errorf("%w: %w", ErrDeployFailed, previewErr)
		}
	}
	manageRoutes := supportsManagedRoutes && request.ManagedRoutes != nil
	var (
		priorOwnership ManagedRouteOwnership
		desiredRoutes  []ManagedRouteSpec
		mutation       ManagedRouteMutation
		nextOwnership  ManagedRouteOwnership
	)
	if manageRoutes {
		if !isHexID(request.OutputID) {
			return result, fmt.Errorf("%w: invalid managed route output", ErrDeployComposition)
		}
		scope, scopeErr := managedDeployer.ManagedRouteScope(request.Target, request.Connection.Redacted())
		if scopeErr != nil || !validManagedRouteScope(scope) {
			return result, fmt.Errorf("%w: managed route scope: %v", ErrDeployComposition, scopeErr)
		}
		priorOwnership, err = request.ManagedRoutes.ManagedRouteOwnership(ctx, scope)
		if err != nil {
			return result, fmt.Errorf("%w: read managed route ownership: %v", ErrDeployComposition, err)
		}
		if priorOwnership.Scope != scope {
			return result, fmt.Errorf("%w: managed route scope mismatch", ErrDeployComposition)
		}
		if err := validateManagedRouteOwnership(priorOwnership); err != nil {
			return result, fmt.Errorf("%w: managed route ownership: %v", ErrDeployComposition, err)
		}
		desiredRoutes, err = managedDeployer.DesiredManagedRoutes(request.Artifact)
		if err != nil {
			return result, fmt.Errorf("%w: managed route artifact: %v", ErrDeployComposition, err)
		}
	}

	started = clock.Now()
	backup, err := deployer.Backup(ctx, device, request.Connection)
	record(StepBackup, started, err, backup.Ref.Hash)
	if err != nil {
		return result, fmt.Errorf("%w: backup: %v", ErrBackupRequired, err)
	}
	if len(backup.Payload) == 0 {
		return result, fmt.Errorf("%w: backup is empty", ErrBackupRequired)
	}
	stored, err := backups.PutBackup(ctx, device.DeployerID, clock.Now().UTC(), backup.Payload)
	if err != nil {
		return result, fmt.Errorf("%w: store backup: %v", ErrBackupRequired, err)
	}
	if !stored.Verified() {
		return result, fmt.Errorf("%w: stored backup is not verifiable", ErrBackupRequired)
	}
	backup.Ref = stored
	result.Backup = stored

	started = clock.Now()
	var deployErr error
	if manageRoutes {
		var current []ManagedRouteSpec
		current, deployErr = managedDeployer.CurrentManagedRoutes(ctx, device, request.Connection)
		if deployErr == nil {
			mutation, nextOwnership, deployErr = reconcileManagedRoutes(priorOwnership, request.OutputID, desiredRoutes, current)
		}
		if deployErr == nil {
			deployErr = managedDeployer.ApplyManagedRoutes(ctx, device, request.Connection, mutation)
		}
	} else {
		deployErr = deployer.Deploy(ctx, device, request.Connection, request.Artifact)
	}
	record(StepDeploy, started, deployErr, request.Artifact.ArtifactHash)
	// The second device read can discover a new foreign collision after the
	// preview. No mutation occurred, so restoring a full backup would itself
	// overwrite the external change we just refused to adopt.
	if errors.Is(deployErr, ErrFQDNOwnershipConflict) {
		return result, deployErr
	}
	if deployErr == nil {
		started = clock.Now()
		var verifyErr error
		if manageRoutes {
			var current []ManagedRouteSpec
			current, verifyErr = managedDeployer.CurrentManagedRoutes(ctx, device, request.Connection)
			if verifyErr == nil {
				verifyErr = verifyManagedRouteMutation(nextOwnership, mutation, current)
			}
		} else {
			verifyErr = deployer.Verify(ctx, device, request.Connection, request.Artifact)
		}
		record(StepVerify, started, verifyErr, request.Artifact.RoutingPlanHash)
		if verifyErr == nil {
			if manageRoutes {
				started = clock.Now()
				persistErr := request.ManagedRoutes.ReplaceManagedRouteOwnership(ctx, nextOwnership)
				record(StepOwnership, started, persistErr, request.OutputID)
				if persistErr != nil {
					deployErr = fmt.Errorf("%w: %v", ErrOwnershipPersist, persistErr)
				} else {
					result.Applied = true
					return result, nil
				}
			} else if supportsFQDN {
				started = clock.Now()
				persistErr := request.ManagedFQDN.ReplaceManagedFQDNOwnership(ctx, nextFQDN)
				record(StepOwnership, started, persistErr, request.OutputID)
				if persistErr != nil {
					deployErr = fmt.Errorf("%w: %v", ErrOwnershipPersist, persistErr)
				} else {
					result.Applied = true
					return result, nil
				}
			} else {
				result.Applied = true
				return result, nil
			}
		} else {
			deployErr = fmt.Errorf("%w: %v", ErrVerifyFailed, verifyErr)
		}
	}

	// A failed deploy or verify always rolls back from the backup taken above.
	//
	// The rollback runs on a context detached from the caller's, with its own
	// budget. The cancellation that failed a deployment must not also disarm the
	// recovery from it: a request deadline that expired mid-write, or an operator
	// who closed the screen, is exactly when the device is already changed and
	// the backup is the only way back. A rollback that inherited that dead
	// context would fail immediately and leave the device half-written.
	rollbackCtx, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), rollbackBudget)
	defer cancelRollback()
	started = clock.Now()
	rollbackErr := deployer.Rollback(rollbackCtx, device, request.Connection, backup)
	record(StepRollback, started, rollbackErr, stored.ID)
	if rollbackErr != nil {
		return result, errors.Join(fmt.Errorf("%w: %v", ErrRollbackFailed, rollbackErr), deployErr)
	}
	result.RolledBack = true
	if errors.Is(deployErr, ErrVerifyFailed) {
		return result, deployErr
	}
	if errors.Is(deployErr, ErrOwnershipPersist) {
		return result, deployErr
	}
	return result, fmt.Errorf("%w: deploy: %v", ErrDeployFailed, deployErr)
}
