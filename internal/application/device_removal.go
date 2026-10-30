package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ErrRemovalUnsupported refuses a removal from a deployer that keeps no exact
// ownership ledger, or keeps one but cannot remove what it names. Such a
// deployer never recorded an object as Routevane's, so there is nothing it may
// remove.
var ErrRemovalUnsupported = errors.New("the deployer cannot remove what it delivered")

// RemovalRequest removes from one device what one output's ownership ledger
// names there (ADR 0060). The connection decides the surface: its canonical
// address is the FQDN ledger endpoint, and for static routes its address,
// target and interface are the route scope.
type RemovalRequest struct {
	Target        domain.TargetDefinition
	Connection    Connection
	OutputID      string
	ManagedRoutes ManagedRouteRepository
	ManagedFQDN   ManagedFQDNRepository
}

// removal is what a request resolved to before anything on the device changes.
type removal struct {
	deployer Deployer
	device   DeviceInfo

	// FQDN surface: the endpoint and the exact groups this output owns there.
	fqdn     ManagedFQDNRemover
	endpoint string
	owned    []ManagedFQDNGroup

	// Static-route surface: the whole scope's ledger, because whether a route
	// may go depends on every other output's claims on the same surface.
	routes ManagedRouteDeployer
	prior  ManagedRouteOwnership
}

// nothingOwned reports a surface where this output's ledger names nothing, so
// no device needs to be reached at all.
func (r removal) nothingOwned(outputID string) bool {
	if r.fqdn != nil {
		return len(r.owned) == 0
	}
	for _, claim := range r.prior.Claims {
		if claim.OutputID == outputID {
			return false
		}
	}
	return true
}

// PreviewRemoval runs the read-only part of a removal: the connection, the
// ledger, the probe and the firmware check, and for FQDN groups a read of the
// device that refuses an owned object this deployer cannot remove safely. It
// never writes. A refusal names its step.
func PreviewRemoval(ctx context.Context, request RemovalRequest, deployers DeployerRegistry, clock Clock) error {
	_, err := prepareRemoval(ctx, request, deployers, clock, func(string, time.Time, error, string) {})
	return err
}

// RemoveFromDevice removes what one output delivered to one device, in the
// order a send uses: probe, compatibility, verified backup, one bounded change,
// read-back, ownership commit, and a detached rollback when the change may have
// been made and did not verify (ADR 0008, ADR 0020).
//
// Only the output's own ledger authorizes a removal. A static route another
// output still claims, or one the router had before Routevane claimed it,
// stays (ADR 0032); an FQDN group outside the output's ledger is never read
// as removable (ADR 0040). The ledger loses this output's rows on that surface
// in the ownership step, after the device was read back, so it never names an
// object the device no longer holds, and a sibling's next send cannot recreate
// a route only the removed output claimed.
func RemoveFromDevice(ctx context.Context, request RemovalRequest, deployers DeployerRegistry, backups BackupStore, clock Clock) (DeployResult, error) {
	var result DeployResult
	if backups == nil || clock == nil {
		return result, ErrDeployComposition
	}
	record := func(step string, started time.Time, err error, detail string) {
		outcome := "success"
		if err != nil {
			outcome = "failed"
		}
		result.Events = append(result.Events, DeployEvent{Step: step, Outcome: outcome, Detail: detail, Duration: clock.Now().Sub(started)})
	}
	plan, err := prepareRemoval(ctx, request, deployers, clock, record)
	result.Device = plan.device
	if err != nil {
		return result, err
	}
	if plan.nothingOwned(request.OutputID) {
		result.Applied = true
		return result, nil
	}
	backup, err := storeVerifiedBackup(ctx, plan.deployer, plan.device, request.Connection, backups, clock, record)
	if err != nil {
		return result, err
	}
	result.Backup = backup.Ref

	started := clock.Now()
	var (
		removeErr      error
		mayHaveChanged bool
		mutation       ManagedRouteMutation
		next           ManagedRouteOwnership
	)
	if plan.fqdn != nil {
		mayHaveChanged, removeErr = plan.fqdn.RemoveFQDNGroups(ctx, plan.device, request.Connection, plan.owned)
	} else {
		var current []ManagedRouteSpec
		current, removeErr = plan.routes.CurrentManagedRoutes(ctx, plan.device, request.Connection)
		if removeErr == nil {
			// The same reconciliation as a send with nothing desired: this
			// output's claims go, and a route goes only when Routevane created
			// it and no claim is left.
			mutation, next, removeErr = reconcileManagedRoutes(plan.prior, request.OutputID, nil, current)
		}
		if removeErr != nil {
			removeErr = errors.Join(ErrDeviceUnchanged, removeErr)
		} else if len(mutation.Upsert)+len(mutation.Remove) > 0 {
			mayHaveChanged = true
			removeErr = plan.routes.ApplyManagedRoutes(ctx, plan.device, request.Connection, mutation)
		}
	}
	record(StepRemove, started, removeErr, request.OutputID)

	var failure error
	if removeErr != nil {
		failure = fmt.Errorf("%w: remove: %w", ErrDeployFailed, removeErr)
	} else {
		started = clock.Now()
		var verifyErr error
		if plan.fqdn != nil {
			verifyErr = plan.fqdn.VerifyFQDNGroupsRemoved(ctx, plan.device, request.Connection, plan.owned)
		} else {
			var current []ManagedRouteSpec
			current, verifyErr = plan.routes.CurrentManagedRoutes(ctx, plan.device, request.Connection)
			if verifyErr == nil {
				verifyErr = verifyManagedRouteMutation(next, mutation, current)
			}
		}
		record(StepVerify, started, verifyErr, request.OutputID)
		if verifyErr != nil {
			failure = fmt.Errorf("%w: %v", ErrVerifyFailed, verifyErr)
		} else {
			started = clock.Now()
			var persistErr error
			if plan.fqdn != nil {
				persistErr = request.ManagedFQDN.ReplaceManagedFQDNOwnership(ctx, ManagedFQDNOwnership{Endpoint: plan.endpoint, OutputID: request.OutputID})
			} else {
				persistErr = request.ManagedRoutes.ReplaceManagedRouteOwnership(ctx, next)
			}
			record(StepOwnership, started, persistErr, request.OutputID)
			if persistErr == nil {
				result.Applied = true
				return result, nil
			}
			failure = fmt.Errorf("%w: %v", ErrOwnershipPersist, persistErr)
		}
	}

	// A refusal proven to come before the first mutation, and a removal that
	// found nothing to remove, must not restore a backup over another writer's
	// later changes (ADR 0040).
	if !mayHaveChanged || errors.Is(removeErr, ErrDeviceUnchanged) {
		return result, failure
	}
	if rollbackErr := restoreDetached(ctx, plan.deployer, plan.device, request.Connection, backup, clock, record); rollbackErr != nil {
		return result, errors.Join(fmt.Errorf("%w: %v", ErrRollbackFailed, rollbackErr), failure)
	}
	result.RolledBack = true
	return result, failure
}

// prepareRemoval resolves the deployer, reads the output's ledger on the
// connection's surface and, when it names something, probes the device and
// previews the change. Nothing here writes.
func prepareRemoval(ctx context.Context, request RemovalRequest, deployers DeployerRegistry, clock Clock, record stepRecorder) (removal, error) {
	if ctx == nil || clock == nil || len(deployers) == 0 || !isHexID(request.OutputID) || request.Connection.URL == "" ||
		request.Target.ID == "" || request.Target.RendererID == "" || request.Target.FormatKey == "" {
		return removal{}, ErrDeployComposition
	}
	deployer, registered := deployers[request.Target.RendererID]
	if !registered || deployer == nil {
		return removal{}, fmt.Errorf("%w: %q", ErrDeployerUnavailable, request.Target.RendererID)
	}
	if err := deployer.ValidateConnection(request.Connection); err != nil {
		return removal{}, fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
	}
	plan := removal{deployer: deployer}
	fqdnOwner, ownsFQDN := deployer.(ManagedFQDNDeployer)
	routeOwner, ownsRoutes := deployer.(ManagedRouteDeployer)
	switch {
	case ownsFQDN:
		remover, removes := deployer.(ManagedFQDNRemover)
		if !removes || request.ManagedFQDN == nil {
			return removal{}, fmt.Errorf("%w: %q", ErrRemovalUnsupported, deployer.ID())
		}
		endpoint, err := fqdnOwner.FQDNEndpoint(request.Connection.Redacted())
		if err != nil {
			return removal{}, fmt.Errorf("%w: %w", ErrConnectionInvalid, err)
		}
		prior, err := request.ManagedFQDN.ManagedFQDNOwnership(ctx, endpoint, request.OutputID)
		if err != nil {
			return removal{}, fmt.Errorf("%w: read FQDN ownership: %v", ErrDeployComposition, err)
		}
		if prior.Endpoint != endpoint || prior.OutputID != request.OutputID || prior.Validate() != nil {
			return removal{}, fmt.Errorf("%w: FQDN ownership", ErrDeployComposition)
		}
		plan.fqdn, plan.endpoint, plan.owned = remover, endpoint, prior.Groups
	case ownsRoutes:
		if request.ManagedRoutes == nil {
			return removal{}, fmt.Errorf("%w: %q", ErrRemovalUnsupported, deployer.ID())
		}
		scope, err := routeOwner.ManagedRouteScope(request.Target, request.Connection.Redacted())
		if err != nil || !validManagedRouteScope(scope) {
			return removal{}, fmt.Errorf("%w: managed route scope: %v", ErrDeployComposition, err)
		}
		prior, err := request.ManagedRoutes.ManagedRouteOwnership(ctx, scope)
		if err != nil {
			return removal{}, fmt.Errorf("%w: read managed route ownership: %v", ErrDeployComposition, err)
		}
		if prior.Scope != scope || validateManagedRouteOwnership(prior) != nil {
			return removal{}, fmt.Errorf("%w: managed route ownership", ErrDeployComposition)
		}
		plan.routes, plan.prior = routeOwner, prior
	default:
		return removal{}, fmt.Errorf("%w: %q", ErrRemovalUnsupported, deployer.ID())
	}
	if plan.nothingOwned(request.OutputID) {
		return plan, nil
	}

	started := clock.Now()
	device, err := deployer.Probe(ctx, request.Connection)
	record(StepProbe, started, err, device.FirmwareVersion)
	if err != nil {
		return plan, &StepError{Step: StepProbe, Err: fmt.Errorf("%w: probe: %w", ErrDeployFailed, err)}
	}
	device.DeployerID = deployer.ID()
	plan.device = device
	if device.FormatKey == "" || device.FormatKey != request.Target.FormatKey {
		return plan, &StepError{Step: StepProbe, Err: fmt.Errorf("%w: firmware %q reports format %q, target requires %q", ErrDeviceIncompatible, device.FirmwareVersion, device.FormatKey, request.Target.FormatKey)}
	}
	if plan.fqdn != nil {
		if _, err := plan.fqdn.PreviewFQDNRemoval(ctx, device, request.Connection, plan.owned); err != nil {
			return plan, &StepError{Step: StepPreview, Err: fmt.Errorf("%w: %w", ErrDeployFailed, err)}
		}
	}
	return plan, nil
}
