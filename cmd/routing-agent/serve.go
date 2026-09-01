package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/httpapi"
	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
	"github.com/Muratovnik/routevane/internal/infrastructure/secretstore"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
)

func runServe(stdout io.Writer, logger *slog.Logger, options serveOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	catalog, err := catalogyaml.Load(deps.Context, options.CatalogDir)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "catalog_invalid")
		return 2
	}
	resolved, err := resolveAdapters(deps.Context, deps, logger)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "adapters_unavailable")
		return 2
	}
	defer resolved.Close()
	definitions := make([]domain.ServiceDefinition, 0, len(catalog.Services))
	for _, definition := range catalog.Services {
		definitions = append(definitions, definition)
	}
	if err := resolved.validateSourceDefinitions(definitions...); err != nil {
		logger.Warn("serve failed", "operation", "serve", "error", err.Error())
		logResult(logger, "serve", "", "", "failed", 0, started, "adapters_unavailable")
		return 2
	}
	targets := catalogTargets(catalog, resolved.renderers)
	if len(targets) == 0 {
		logResult(logger, "serve", "", "", "failed", 0, started, "target_invalid")
		return 2
	}
	root, err := filesystem.EnsureDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "data_root_invalid")
		return 2
	}
	lock, err := processlock.Acquire(root)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "server_lock_unavailable")
		return 1
	}
	defer lock.Close()
	store, err := sqlite.Open(deps.Context, root)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "database_unavailable")
		return 1
	}
	defer store.Close()
	service, err := application.NewPublicationService(application.PublicationConfig{Definitions: catalog.Services, Categories: catalog.Categories, Targets: targets, TargetRevision: catalog.TargetRevision, FeedURL: httpfeed.ValidateURL, Store: store, Files: filesystem.PublishedStore{DataRoot: root}, Renderers: resolved.renderers, Sources: resolved.sources, Clock: application.ClockFunc(deps.Now)})
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	// Operator-defined services and standing service corrections are part of
	// the catalog this process serves. Serving lists without them would
	// silently publish something else than stored, so a registry that cannot
	// be read is a startup failure, not a degraded mode.
	if err := service.LoadCustomServices(deps.Context); err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "custom_services_unavailable")
		return 1
	}
	if err := service.LoadServiceTuning(deps.Context); err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "service_tuning_unavailable")
		return 1
	}
	// Category membership is operator-owned over the catalog seed (ADR 0028),
	// and every route that names a category expands through it. Serving with an
	// unread overlay would publish the shipped grouping under the operator's
	// name, so it is a startup failure rather than a degraded mode.
	if err := service.LoadCategories(deps.Context); err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "categories_unavailable")
		return 1
	}
	listener, err := deps.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", options.Port))
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "listen_failed")
		return 1
	}
	defer listener.Close()
	tcp, ok := listener.Addr().(*net.TCPAddr)
	if !ok || tcp.IP.To4() == nil || !tcp.IP.IsLoopback() {
		logResult(logger, "serve", "", "", "failed", 0, started, "listen_failed")
		return 1
	}
	// The server can apply what it publishes. Deployment is composed separately
	// because it is a different decision: publication owns formats, deployment
	// owns transports.
	deployments, err := application.NewDeploymentService(application.DeploymentConfig{
		Artifacts: service,
		Deployers: deployerRegistry(deps, deployOptions{}),
		Backups:   filesystem.BackupStore{DataRoot: root},
		Clock:     application.ClockFunc(deps.Now),
	})
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	// The registry is composed here rather than inside publication: what an
	// operator installed is a fact about their network, and the credential it
	// may hold belongs to the operating system rather than to this database.
	deployableIDs := make(map[string]struct{}, len(targets))
	credentialTargets := make(map[string]struct{}, len(targets))
	interfaceTargets := make(map[string]struct{}, len(targets))
	for _, target := range deployments.DeployableTargets() {
		deployableIDs[target.TargetID] = struct{}{}
		if target.Requirements.NeedsCredential {
			credentialTargets[target.TargetID] = struct{}{}
		}
		if target.Requirements.NeedsInterface {
			interfaceTargets[target.TargetID] = struct{}{}
		}
	}
	devices, err := application.NewDeviceService(application.DeviceConfig{
		Store:                    store,
		Secrets:                  secretstore.New(),
		Targets:                  service.Targets,
		Deployable:               func(id string) bool { _, ok := deployableIDs[id]; return ok },
		NeedsCredential:          func(id string) bool { _, ok := credentialTargets[id]; return ok },
		NeedsInterface:           func(id string) bool { _, ok := interfaceTargets[id]; return ok },
		ValidateStoredConnection: deployments.ValidateStoredConnection,
		Clock:                    application.ClockFunc(deps.Now),
		Entropy:                  rand.Reader,
	})
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	deliveryGate := application.NewDeliveryGate()
	automaticDelivery, err := application.NewAutomaticDeliveryService(application.AutomaticDeliveryConfig{
		Devices: devices, Deployments: deployments, Outputs: service, Gate: deliveryGate,
	})
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	origin := "http://" + listener.Addr().String()
	server, err := httpapi.New(origin, serveBackend{PublicationService: service, deployments: deployments, devices: devices, deliveryGate: deliveryGate}, logger)
	if err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "composition_invalid")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, origin); err != nil {
		logResult(logger, "serve", "", "", "failed", 0, started, "output_failed")
		return 1
	}
	// A binary built without the generated control surface still serves the API.
	// Saying so here is the difference between a one-line fix and an operator
	// concluding the product is broken. The record carries fields rather than a
	// message because this logger deliberately drops the message key.
	if server.UIAvailable() {
		logger.Info("serve", "operation", "serve", "origin", origin, "control_surface", "embedded")
	} else {
		logger.Warn("serve", "operation", "serve", "origin", origin, "control_surface", "absent",
			"detail", "the API is available and the browser page is not",
			"remedy", "pwsh -NoLogo -NoProfile -File tools/dev.ps1 up")
	}
	ctx, cancel := deps.SignalContext(deps.Context)
	defer cancel()
	// The timer lives inside the process that serves the files, because a
	// separate worker would need its own copy of the same decisions. It starts
	// unconditionally and does nothing until a list says it should: the default
	// rule is off, so nothing reaches the network on a timer until an operator
	// says so.
	scheduler := startScheduler(ctx, service, automaticDelivery, logger, deps)
	defer scheduler()
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		if err != nil {
			logResult(logger, "serve", "", "", "failed", 0, started, "serve_failed")
			return 1
		}
		return 0
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logResult(logger, "serve", "", "", "failed", 0, started, "shutdown_failed")
			return 1
		}
		if err := <-served; err != nil {
			logResult(logger, "serve", "", "", "failed", 0, started, "serve_failed")
			return 1
		}
		return 0
	}
}

// schedulerTick is how often the process asks what is due. It is not the
// refresh interval: the coarsest rule is daily, and a minute of drift on a
// daily list is not worth a wake-up that reads the clock more often.
const schedulerTick = time.Minute

// startScheduler runs the due-refresh loop until the context ends and returns
// the function that waits for it. Each tick is bounded and its outcome logged,
// so an operator reading the log can tell a quiet timer from a broken one.
func startScheduler(ctx context.Context, service *application.PublicationService, deliveries *application.AutomaticDeliveryService, logger *slog.Logger, deps runtimeDeps) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ticker := deps.NewDelayTicker(schedulerTick)
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.Chan():
			}
			ticker.Stop()
			publicationCtx, cancelPublication := context.WithTimeout(ctx, 10*time.Minute)
			runs, err := service.RunDueRefreshes(publicationCtx)
			cancelPublication()
			if err != nil {
				logger.Warn("scheduler", "operation", "schedule", "status", "failed", "error_code", "due_refresh_failed")
				continue
			}
			// Delivery derives independent per-attempt budgets from shutdown, not
			// from the publication deadline or a sibling's elapsed time.
			runs = deliveries.Deliver(ctx, runs)
			for _, run := range runs {
				status := "succeeded"
				if run.Error != "" || (run.Built == 0 && len(run.Failures) > 0) {
					status = "failed"
				} else if len(run.Failures) > 0 || len(run.DeliveryFailures) > 0 {
					status = "partial"
				}
				for _, failure := range run.Failures {
					logger.Warn("scheduler output", "operation", "schedule", "list", run.ListID,
						"output", failure.OutputID, "target", failure.TargetID, "status", "failed",
						"error_code", failure.Code)
				}
				for _, failure := range run.DeliveryFailures {
					logger.Warn("scheduler delivery", "operation", "schedule", "list", run.ListID,
						"output", failure.OutputID, "device", failure.DeviceID, "artifact", failure.ArtifactID,
						"status", "failed", "error_code", failure.Code)
				}
				logger.Info("scheduler", "operation", "schedule", "list", run.ListID, "status", status,
					"count", run.Built, "failures", len(run.Failures), "delivered", run.Delivered,
					"delivery_failures", len(run.DeliveryFailures))
			}
		}
	}()
	return func() { <-done }
}

// serveBackend is the one place the two services are presented as a single
// surface to the HTTP layer. It adds no behavior: each method belongs to the
// service that owns the decision.
type serveBackend struct {
	*application.PublicationService
	deployments  *application.DeploymentService
	devices      *application.DeviceService
	deliveryGate *application.DeliveryGate
}

func (b serveBackend) SecretStoreAvailable() bool { return b.devices.SecretStoreAvailable() }

func (b serveBackend) DeviceCards(ctx context.Context) ([]application.DeviceCard, error) {
	return b.devices.DeviceCards(ctx)
}

func (b serveBackend) RegisterDevice(ctx context.Context, targetID, name, address, account, interfaceName string) (application.Device, error) {
	return b.devices.RegisterDevice(ctx, targetID, name, address, account, interfaceName)
}

func (b serveBackend) UpdateDevice(ctx context.Context, id, name, address, account, interfaceName string) (application.Device, error) {
	release, err := b.deliveryGate.Acquire(ctx)
	if err != nil {
		return application.Device{}, err
	}
	defer release()
	return b.devices.UpdateDevice(ctx, id, name, address, account, interfaceName)
}

func (b serveBackend) SetOutputDevice(ctx context.Context, outputID, deviceID string) (application.Output, error) {
	release, err := b.deliveryGate.Acquire(ctx)
	if err != nil {
		return application.Output{}, err
	}
	defer release()
	if deviceID == "" {
		return b.PublicationService.SetOutputDevice(ctx, outputID, nil)
	}
	device, err := b.devices.Device(ctx, deviceID)
	if err != nil {
		return application.Output{}, err
	}
	return b.PublicationService.SetOutputDevice(ctx, outputID, &device)
}

func (b serveBackend) ForgetDevice(ctx context.Context, id string) error {
	release, err := b.deliveryGate.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return b.devices.ForgetDevice(ctx, id)
}

func (b serveBackend) EnableAutoDelivery(ctx context.Context, id, secret string) (application.Device, error) {
	release, err := b.deliveryGate.Acquire(ctx)
	if err != nil {
		return application.Device{}, err
	}
	defer release()
	return b.devices.EnableAutoDelivery(ctx, id, secret)
}

func (b serveBackend) DisableAutoDelivery(ctx context.Context, id string) (application.Device, error) {
	release, err := b.deliveryGate.Acquire(ctx)
	if err != nil {
		return application.Device{}, err
	}
	defer release()
	return b.devices.DisableAutoDelivery(ctx, id)
}

func (b serveBackend) DeployableTargets() []application.DeployableTarget {
	return b.deployments.DeployableTargets()
}

func (b serveBackend) DeployPlan(ctx context.Context, command application.DeployCommand) (application.DeployPlan, error) {
	return b.deployments.Plan(ctx, command)
}

func (b serveBackend) Deploy(ctx context.Context, command application.DeployCommand) (application.DeployResult, error) {
	return b.deployments.Deploy(ctx, command)
}
