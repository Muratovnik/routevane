package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	deployersingboxlocal "github.com/Muratovnik/routevane/internal/deployers/singboxlocal"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

// DevicePasswordVariable is the only place a device password is read from. It is
// never a command-line flag, because a flag appears in the process list and in
// shell history.
const DevicePasswordVariable = "ROUTEVANE_DEVICE_PASSWORD"

type deployOptions struct {
	ArtifactID  string
	Target      string
	DeviceURL   string
	Username    string
	Interface   string
	CatalogDir  string
	DataDir     string
	Confirm     bool
	InsecureTLS bool
}

func parseDeploy(args []string) (deployOptions, bool) {
	options := deployOptions{}
	set := flag.NewFlagSet("deploy", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&options.ArtifactID, "artifact", "", "published artifact identity")
	set.StringVar(&options.Target, "target", "", "catalog target the artifact belongs to")
	set.StringVar(&options.DeviceURL, "device", "", "device address, for example http://192.168.1.1, or file:///path/to/config.json for a local target")
	set.StringVar(&options.Username, "user", "", "device account, when the target's deployer authenticates")
	set.StringVar(&options.Interface, "interface", "", "device interface routes attach to, when the target has one")
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	set.BoolVar(&options.Confirm, "confirm", false, "apply the artifact to the device")
	set.BoolVar(&options.InsecureTLS, "device-tls-untrusted", false, "accept the device's own certificate for its private address")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return deployOptions{}, false
	}
	// Which of the remaining values a deployment needs is a property of the
	// deployer, not of the command line: a local target has no account and no
	// interface, and its deployer refuses one.
	if options.ArtifactID == "" || options.Target == "" || options.DeviceURL == "" {
		return deployOptions{}, false
	}
	return options, true
}

// deployReport is the audit record written to stdout. It contains identities and
// outcomes only: no credential and no device configuration content.
type deployReport struct {
	Target     string                    `json:"target"`
	ArtifactID string                    `json:"artifact_id"`
	Device     application.DeviceInfo    `json:"device"`
	Backup     application.BackupRef     `json:"backup"`
	Applied    bool                      `json:"applied"`
	RolledBack bool                      `json:"rolled_back"`
	Confirmed  bool                      `json:"confirmed"`
	Events     []application.DeployEvent `json:"events,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Hint       string                    `json:"hint,omitempty"`
}

func runDeploy(ctx context.Context, stdout io.Writer, logger *slog.Logger, options deployOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	catalog, err := catalogyaml.Load(ctx, options.CatalogDir)
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "catalog_invalid")
		return 2
	}
	resolved, err := resolveAdapters(ctx, deps, logger)
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "adapters_unavailable")
		return 1
	}
	defer resolved.Close()
	target, ok := catalogTargets(catalog, resolved.renderers)[options.Target]
	if !ok {
		logResult(logger, "deploy", "", "", "failed", 0, started, "target_invalid")
		return 2
	}
	report := deployReport{Target: target.ID, ArtifactID: options.ArtifactID}
	if !options.Confirm {
		report.Hint = "review the device and artifact above, then repeat the command with --confirm"
		if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
			logResult(logger, "deploy", "", "", "failed", 0, started, "output_failed")
			return 1
		}
		logResult(logger, "deploy", "", "", "success", 0, started, "confirmation_required")
		return 0
	}
	// The variable may legitimately be empty: a local deployment takes no
	// credential, and the deployer is what refuses a missing one.
	password := os.Getenv(DevicePasswordVariable)

	root, err := filesystem.ResolvePrivateDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "data_root_unavailable")
		return 1
	}
	store, err := sqlite.OpenExisting(ctx, root)
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "database_unavailable")
		return 1
	}
	defer store.Close()
	publication, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: catalog.Lists, Categories: catalog.Categories,
		Targets: catalogTargets(catalog, resolved.renderers), TargetRevision: catalog.TargetRevision,
		Store: store, Files: filesystem.PublishedStore{DataRoot: root},
		Renderers: resolved.renderers, Sources: resolved.sources, Clock: application.ClockFunc(deps.Now),
	})
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	payload, err := publication.Artifact(ctx, options.ArtifactID)
	if err != nil {
		logger.Warn("deploy failed", "operation", "deploy", "error", err.Error())
		logResult(logger, "deploy", "", "", "failed", 0, started, "artifact_unavailable")
		return 1
	}
	if payload.Artifact.RendererID != target.RendererID {
		logResult(logger, "deploy", "", "", "failed", 0, started, "artifact_target_mismatch")
		return 2
	}

	deployments, err := application.NewDeploymentService(application.DeploymentConfig{
		Artifacts: publication, Deployers: deployerRegistry(deps, options),
		Backups: filesystem.BackupStore{DataRoot: root}, ManagedRoutes: store, ManagedFQDN: store,
		Clock: application.ClockFunc(deps.Now),
	})
	if err != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	result, deployErr := deployments.Deploy(ctx, application.DeployCommand{
		ArtifactID: payload.Artifact.ID,
		Connection: application.Connection{
			URL: options.DeviceURL, Username: options.Username, Password: password, Interface: options.Interface,
		},
		Confirm: true,
	})
	report.Confirmed = true
	report.Device = result.Device
	report.Backup = result.Backup
	report.Applied = result.Applied
	report.RolledBack = result.RolledBack
	report.Events = result.Events
	if deployErr != nil {
		report.Error = deployErr.Error()
	}
	// The audit trail is written whether the deployment succeeded or not: a
	// failed attempt is exactly the case an operator needs a record of.
	for _, event := range result.Events {
		logger.Info("deploy step", "operation", "deploy", "step", event.Step, "outcome", event.Outcome, "detail", event.Detail, "duration_ns", int64(event.Duration))
	}
	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		logResult(logger, "deploy", "", "", "failed", 0, started, "output_failed")
		return 1
	}
	if deployErr != nil {
		logger.Warn("deploy failed", "operation", "deploy", "error", deployErr.Error())
		logResult(logger, "deploy", "", "", "failed", 0, started, deployErrorCode(deployErr))
		return 1
	}
	logResult(logger, "deploy", "", "", "success", len(result.Events), started, "")
	return 0
}

// deployerRegistry binds a renderer id to the deployer that installs its
// artifacts. It is the only place that mapping exists.
func deployerRegistry(deps runtimeDeps, options deployOptions) application.DeployerRegistry {
	return application.DeployerRegistry{
		deployerkeenetic.DeployerID: deployerkeenetic.New(deployerkeenetic.Options{
			Dialer:            deps.DeviceDialer,
			InsecureDeviceTLS: options.InsecureTLS,
		}),
		// The same router, a second artifact format. DNS-based routes are their
		// own command surface with their own firmware floor, so they are their
		// own deployer rather than a branch inside the route one.
		deployerkeenetic.FQDNDeployerID: deployerkeenetic.NewFQDNDeployer(deployerkeenetic.Options{
			Dialer:            deps.DeviceDialer,
			InsecureDeviceTLS: options.InsecureTLS,
		}),
		deployersingboxlocal.DeployerID: deployersingboxlocal.New(),
	}
}

func deployErrorCode(err error) string {
	switch {
	case errors.Is(err, application.ErrRollbackFailed):
		return "rollback_failed"
	case errors.Is(err, application.ErrFQDNOwnershipConflict):
		return "fqdn_ownership_conflict"
	case errors.Is(err, application.ErrDeviceIncompatible):
		return "device_incompatible"
	case errors.Is(err, application.ErrBackupRequired):
		return "backup_unavailable"
	case errors.Is(err, application.ErrVerifyFailed):
		return "verify_failed"
	case errors.Is(err, application.ErrDeployerUnavailable):
		return "deployer_unavailable"
	case errors.Is(err, application.ErrConnectionInvalid):
		return "connection_invalid"
	case errors.Is(err, application.ErrDeployComposition):
		return "composition_invalid"
	default:
		return "deploy_failed"
	}
}
