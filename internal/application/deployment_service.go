package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrDeploymentComposition = errors.New("invalid deployment service composition")
	// ErrConfirmationRequired is returned by a plan. It is not a failure: it is
	// the deliberate stop between deciding what would happen and doing it.
	ErrConfirmationRequired = errors.New("deployment requires confirmation")
	// ErrDeploymentNotStarted identifies a refusal before the attempt journal
	// or a device was reached. An empty attempt alone cannot establish this:
	// a journal error may occur after its claim was persisted.
	ErrDeploymentNotStarted = errors.New("deployment was refused before starting an attempt")
)

// ArtifactSource is what a deployment needs from publication: the exact bytes
// that were published, the target they were built for, and the plan snapshot
// they represent. It is declared here because this is the consumer; publication
// does not know deployment exists.
type ArtifactSource interface {
	Artifact(context.Context, string) (ArtifactPayload, error)
	Output(context.Context, string) (Output, error)
	// Snapshot resolves the plan the artifact's bytes were rendered from. The
	// audit trail records what was applied, and the bytes alone cannot say
	// which plan they represent: the artifact hash identifies the rendering,
	// the plan's semantic hash identifies the routing decision.
	Snapshot(context.Context, string) (PlanSnapshotRecord, error)
	TargetDefinition(string) (domain.TargetDefinition, error)
	Targets() []TargetOption
}

// DeploymentConfig composes the list. Every field is required: a deployment
// that cannot store a backup or cannot resolve an artifact must fail at
// composition rather than half-way through a device change.
type DeploymentConfig struct {
	Artifacts     ArtifactSource
	Deployers     DeployerRegistry
	Backups       BackupStore
	ManagedRoutes ManagedRouteRepository
	ManagedFQDN   ManagedFQDNRepository
	Clock         Clock
}

// DeploymentService turns one published artifact plus one connection into an
// audited deployment. It owns no transport: which deployer applies which format
// is the registry's decision, and what a connection must carry is the
// deployer's.
type DeploymentService struct {
	config DeploymentConfig
}

func NewDeploymentService(config DeploymentConfig) (*DeploymentService, error) {
	if config.Artifacts == nil || config.Backups == nil || config.Clock == nil || len(config.Deployers) == 0 {
		return nil, ErrDeploymentComposition
	}
	for id, deployer := range config.Deployers {
		if id == "" || deployer == nil || deployer.ID() != id {
			return nil, fmt.Errorf("%w: deployer %q", ErrDeploymentComposition, id)
		}
	}
	return &DeploymentService{config: config}, nil
}

// DeployableTarget is one target an operator can deploy to, with what its
// deployer needs in order to be used.
type DeployableTarget struct {
	TargetID     string                 `json:"target_id"`
	Title        string                 `json:"title"`
	DeployerID   string                 `json:"deployer_id"`
	Requirements ConnectionRequirements `json:"requirements"`
}

// DeployableTargets lists the targets this build can actually deploy to. A
// target whose format has no deployer is omitted rather than offered, for the
// same reason an unserviceable target is not selectable at all.
func (s *DeploymentService) DeployableTargets() []DeployableTarget {
	options := s.config.Artifacts.Targets()
	deployable := make([]DeployableTarget, 0, len(options))
	for _, option := range options {
		deployer, registered := s.config.Deployers[option.RendererID]
		if !registered || deployer == nil {
			continue
		}
		deployable = append(deployable, DeployableTarget{
			TargetID: option.ID, Title: option.Title,
			DeployerID: deployer.ID(), Requirements: deployer.Requirements(),
		})
	}
	slices.SortFunc(deployable, func(a, b DeployableTarget) int { return cmp.Compare(a.TargetID, b.TargetID) })
	return deployable
}

// ValidateStoredConnection asks the target's deployer whether the non-secret
// metadata is safe and complete enough to persist. Registration is a storage
// boundary, so it must apply the destination policy before an address can
// outlive the request that supplied it.
func (s *DeploymentService) ValidateStoredConnection(targetID string, connection Connection) error {
	target, err := s.config.Artifacts.TargetDefinition(targetID)
	if err != nil {
		return err
	}
	deployer, registered := s.config.Deployers[target.RendererID]
	if !registered || deployer == nil {
		return fmt.Errorf("%w: %q", ErrDeployerUnavailable, target.RendererID)
	}
	if err := deployer.ValidateStoredConnection(connection); err != nil {
		return fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
	}
	return nil
}

// DeployCommand is one deployment attempt. Confirm is the caller's explicit
// intent to change the device; without it the list reports what would happen
// and stops.
type DeployCommand struct {
	ArtifactID string
	// DeviceID selects registered metadata. ResolveDeviceConnection consumes it
	// under the delivery gate before a plan or a durable attempt is created.
	DeviceID           string
	ExpectedConnection *DeviceConnectionSnapshot
	Connection         Connection
	Confirm            bool
}

// DeviceConnectionSnapshot is the non-secret access the operator selected.
// It refuses a stale registration before any credential is read or device used.
type DeviceConnectionSnapshot struct {
	Address   string `json:"device"`
	Account   string `json:"username"`
	Interface string `json:"interface"`
}

type deploymentDeviceSource interface {
	Device(context.Context, string) (Device, error)
	DeviceCredential(context.Context, string) (string, error)
}

// ResolveDeviceConnection turns one registered access into the canonical
// connection used for this request. The caller holds the delivery gate through
// resolution and deployment so an address edit or a credential removal cannot
// separate the credential check from its use. It does not change a binding or
// persist the optional one-use password.
func (s *DeploymentService) ResolveDeviceConnection(ctx context.Context, command DeployCommand, devices deploymentDeviceSource) (DeployCommand, error) {
	if command.DeviceID == "" {
		if command.ExpectedConnection != nil {
			return DeployCommand{}, ErrConnectionInvalid
		}
		return command, nil
	}
	if devices == nil || command.ExpectedConnection == nil || command.Connection.URL != "" || command.Connection.Username != "" || command.Connection.Interface != "" {
		return DeployCommand{}, ErrConnectionInvalid
	}
	device, err := devices.Device(ctx, command.DeviceID)
	if err != nil {
		return DeployCommand{}, err
	}
	current := DeviceConnectionSnapshot{Address: device.Address, Account: device.Account, Interface: device.Interface}
	if *command.ExpectedConnection != current {
		return DeployCommand{}, ErrConnectionInvalid
	}
	_, target, err := s.artifactTarget(ctx, command.ArtifactID)
	if err != nil {
		return DeployCommand{}, err
	}
	if device.TargetID != target.ID {
		return DeployCommand{}, ErrDeviceIncompatible
	}
	deployer, registered := s.config.Deployers[target.RendererID]
	if !registered || deployer == nil {
		return DeployCommand{}, ErrDeployerUnavailable
	}
	connection := Connection{URL: device.Address, Username: device.Account, Interface: device.Interface, Password: command.Connection.Password}
	if connection.Password == "" && deployer.Requirements().NeedsCredential {
		// A saved credential serves a manual delivery whether or not automatic
		// delivery is on (ADR 0050); a password in the request still wins above.
		if !device.CredentialStored {
			return DeployCommand{}, ErrConnectionInvalid
		}
		credential, credentialErr := devices.DeviceCredential(ctx, device.ID)
		if credentialErr != nil || credential == "" {
			return DeployCommand{}, fmt.Errorf("%w: stored credential is unavailable", ErrConnectionInvalid)
		}
		connection.Password = credential
	}
	if err := deployer.ValidateConnection(connection); err != nil {
		return DeployCommand{}, fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
	}
	command.DeviceID = ""
	command.ExpectedConnection = nil
	command.Connection = connection
	return command, nil
}

// DeployPlan is what a deployment would do. It carries identities only, so it
// can be logged and returned: never the credential it was resolved with.
type DeployPlan struct {
	ArtifactID   string                 `json:"artifact_id"`
	ArtifactHash string                 `json:"artifact_hash"`
	TargetID     string                 `json:"target_id"`
	Title        string                 `json:"title"`
	DeployerID   string                 `json:"deployer_id"`
	SizeBytes    int64                  `json:"size_bytes"`
	FQDNChanges  []string               `json:"fqdn_changes,omitzero"`
	Requirements ConnectionRequirements `json:"requirements"`
}

// Plan resolves the artifact, its target, and its deployer, and validates the
// connection. DNS deployment additionally reads device state to preview exact
// changes; it never writes before confirmation.
func (s *DeploymentService) Plan(ctx context.Context, command DeployCommand) (DeployPlan, error) {
	payload, target, deployer, err := s.resolve(ctx, command)
	if err != nil {
		return DeployPlan{}, err
	}
	title := target.Title
	if title == "" {
		title = target.ID
	}
	plan := DeployPlan{
		ArtifactID: payload.Artifact.ID, ArtifactHash: payload.Artifact.ArtifactHash,
		TargetID: target.ID, Title: title, DeployerID: deployer.ID(),
		SizeBytes: payload.Artifact.SizeBytes, Requirements: deployer.Requirements(),
	}
	if fqdn, ok := deployer.(ManagedFQDNDeployer); ok {
		if s.config.ManagedFQDN == nil {
			return DeployPlan{}, fmt.Errorf("%w: FQDN ownership ledger required", ErrDeployComposition)
		}
		device, probeErr := deployer.Probe(ctx, command.Connection)
		if probeErr != nil {
			return DeployPlan{}, fmt.Errorf("%w: probe: %w", ErrDeployFailed, probeErr)
		}
		if device.FormatKey != target.FormatKey {
			return DeployPlan{}, ErrDeviceIncompatible
		}
		endpoint, endpointErr := fqdn.FQDNEndpoint(command.Connection.Redacted())
		if endpointErr != nil {
			return DeployPlan{}, endpointErr
		}
		prior, readErr := s.config.ManagedFQDN.ManagedFQDNOwnership(ctx, endpoint, payload.Artifact.OutputID)
		if readErr != nil {
			return DeployPlan{}, readErr
		}
		plan.FQDNChanges, err = fqdn.PreviewFQDNGroups(ctx, device, command.Connection, DeployArtifact{RendererID: payload.Artifact.RendererID, Payload: payload.Payload, OwnedFQDNGroups: prior.Groups})
		if err != nil {
			return DeployPlan{}, fmt.Errorf("%w: preview: %w", ErrDeployFailed, err)
		}
	}
	return plan, nil
}

// Deploy applies the artifact through the fixed lifecycle. It refuses without
// confirmation, so a caller cannot change a device by accident.
func (s *DeploymentService) Deploy(ctx context.Context, command DeployCommand) (DeployResult, error) {
	if !command.Confirm {
		return DeployResult{}, ErrConfirmationRequired
	}
	payload, target, _, err := s.resolve(ctx, command)
	if err != nil {
		return DeployResult{}, err
	}
	// A deployment that cannot say which plan it is applying is refused rather
	// than audited with a placeholder. An audit record that names the rendering
	// twice is worse than no record: it reads as provenance and is not.
	snapshot, err := s.config.Artifacts.Snapshot(ctx, payload.Artifact.PlanSnapshotID)
	if err != nil {
		return DeployResult{}, fmt.Errorf("%w: plan snapshot %q: %v", ErrDeployComposition, payload.Artifact.PlanSnapshotID, err)
	}
	if snapshot.RoutingPlanHash == "" {
		return DeployResult{}, fmt.Errorf("%w: plan snapshot %q carries no routing plan hash", ErrDeployComposition, payload.Artifact.PlanSnapshotID)
	}
	request := DeployRequest{
		Target:        target,
		Connection:    command.Connection,
		OutputID:      payload.Artifact.OutputID,
		ManagedRoutes: s.config.ManagedRoutes,
		ManagedFQDN:   s.config.ManagedFQDN,
		Artifact: DeployArtifact{
			ArtifactID:      payload.Artifact.ID,
			RendererID:      payload.Artifact.RendererID,
			ArtifactHash:    payload.Artifact.ArtifactHash,
			ContentType:     payload.Artifact.ContentType,
			Payload:         payload.Payload,
			PlanSnapshotID:  payload.Artifact.PlanSnapshotID,
			RoutingPlanHash: snapshot.RoutingPlanHash,
			PlanSnapshot:    append([]byte(nil), snapshot.RoutingPlanJSON...),
		},
	}
	return DeployToDevice(ctx, request, s.config.Deployers, s.config.Backups, s.config.Clock)
}

// RetireManagedRoutes revokes deletion authority for one stored connection
// identity without contacting or mutating the device. A later deployment to
// that address starts from an empty, additive ledger.
func (s *DeploymentService) RetireManagedRoutes(ctx context.Context, targetID string, connection Connection) error {
	if s.config.ManagedRoutes == nil && s.config.ManagedFQDN == nil {
		return nil
	}
	target, err := s.config.Artifacts.TargetDefinition(targetID)
	if err != nil {
		return err
	}
	deployer, registered := s.config.Deployers[target.RendererID]
	if !registered || deployer == nil {
		return fmt.Errorf("%w: %q", ErrDeployerUnavailable, target.RendererID)
	}
	managed, ok := deployer.(ManagedRouteDeployer)
	if fqdn, fqdnOK := deployer.(ManagedFQDNDeployer); fqdnOK && s.config.ManagedFQDN != nil {
		endpoint, err := fqdn.FQDNEndpoint(connection.Redacted())
		if err != nil {
			return err
		}
		return s.config.ManagedFQDN.RetireManagedFQDNOwnership(ctx, endpoint, connection.Interface)
	}
	if !ok {
		return nil
	}
	if err := deployer.ValidateStoredConnection(connection.Redacted()); err != nil {
		return fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
	}
	scope, err := managed.ManagedRouteScope(target, connection.Redacted())
	if err != nil || !validManagedRouteScope(scope) {
		return fmt.Errorf("%w: managed route scope: %v", ErrDeployComposition, err)
	}
	return s.config.ManagedRoutes.RetireManagedRouteOwnership(ctx, scope)
}

// resolve is the one place an artifact, a target, and a deployer are matched.
// An artifact built for another target is refused here rather than being sent to
// a deployer that would install the wrong bytes.
func (s *DeploymentService) resolve(ctx context.Context, command DeployCommand) (ArtifactPayload, domain.TargetDefinition, Deployer, error) {
	if command.DeviceID != "" || command.ExpectedConnection != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, nil, ErrConnectionInvalid
	}
	payload, target, err := s.artifactTarget(ctx, command.ArtifactID)
	if err != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, nil, err
	}
	deployer, registered := s.config.Deployers[target.RendererID]
	if !registered || deployer == nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, nil, fmt.Errorf("%w: %q", ErrDeployerUnavailable, target.RendererID)
	}
	if err := deployer.ValidateConnection(command.Connection); err != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, nil, fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
	}
	return payload, target, deployer, nil
}

func (s *DeploymentService) artifactTarget(ctx context.Context, artifactID string) (ArtifactPayload, domain.TargetDefinition, error) {
	payload, err := s.config.Artifacts.Artifact(ctx, artifactID)
	if err != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, err
	}
	// The target comes from the output the artifact belongs to, so a caller
	// cannot name a different one and have its bytes installed.
	output, err := s.config.Artifacts.Output(ctx, payload.Artifact.OutputID)
	if err != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, err
	}
	target, err := s.config.Artifacts.TargetDefinition(output.TargetID)
	if err != nil {
		return ArtifactPayload{}, domain.TargetDefinition{}, err
	}
	if payload.Artifact.RendererID != target.RendererID {
		return ArtifactPayload{}, domain.TargetDefinition{}, fmt.Errorf("%w: artifact was built by %q, target expects %q", ErrDeployComposition, payload.Artifact.RendererID, target.RendererID)
	}
	return payload, target, nil
}
