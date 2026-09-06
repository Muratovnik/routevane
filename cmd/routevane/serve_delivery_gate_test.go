package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	deliveryGateArtifactID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deliveryGateOutputID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	deliveryGateSnapshotID = "cccccccccccccccccccccccccccccccc"
	deliveryGateRendererID = "controlled-delivery"
)

type deliveryGateArtifacts struct {
	payload  application.ArtifactPayload
	output   application.Output
	target   domain.TargetProfile
	snapshot application.PlanSnapshotRecord
}

func newDeliveryGateArtifacts() *deliveryGateArtifacts {
	target := domain.TargetProfile{
		ID:          "controlled-target",
		Title:       "Controlled target",
		ProfileKey:  "controlled-profile-v1",
		RendererID:  deliveryGateRendererID,
		Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1},
	}
	return &deliveryGateArtifacts{
		payload: application.ArtifactPayload{
			Artifact: application.ArtifactBuildRecord{
				ID:             deliveryGateArtifactID,
				OutputID:       deliveryGateOutputID,
				PlanSnapshotID: deliveryGateSnapshotID,
				RendererID:     deliveryGateRendererID,
				ArtifactHash:   strings.Repeat("a", 64),
				ContentType:    "application/octet-stream",
				SizeBytes:      1,
			},
			Payload: []byte("x"),
		},
		output: application.Output{ID: deliveryGateOutputID, TargetID: target.ID},
		target: target,
		snapshot: application.PlanSnapshotRecord{
			ID:              deliveryGateSnapshotID,
			OutputID:        deliveryGateOutputID,
			RoutingPlanHash: strings.Repeat("b", 64),
			Status:          "valid",
		},
	}
}

func (s *deliveryGateArtifacts) Artifact(context.Context, string) (application.ArtifactPayload, error) {
	return s.payload, nil
}

func (s *deliveryGateArtifacts) Output(context.Context, string) (application.Output, error) {
	return s.output, nil
}

func (s *deliveryGateArtifacts) Snapshot(context.Context, string) (application.PlanSnapshotRecord, error) {
	return s.snapshot, nil
}

func (s *deliveryGateArtifacts) TargetProfile(string) (domain.TargetProfile, error) {
	return s.target, nil
}

func (s *deliveryGateArtifacts) Targets() []application.TargetOption {
	return []application.TargetOption{{ID: s.target.ID, Title: s.target.Title, RendererID: s.target.RendererID}}
}

type deliveryGateBackupStore struct{}

func (deliveryGateBackupStore) PutBackup(context.Context, string, time.Time, []byte) (application.BackupRef, error) {
	return application.BackupRef{
		ID:        "dddddddddddddddddddddddddddddddd",
		Path:      "controlled-backup",
		Hash:      strings.Repeat("c", 64),
		SizeBytes: 1,
		CreatedAt: time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC),
	}, nil
}

type controlledDeliveryDeployer struct {
	mu        sync.Mutex
	active    int
	maxActive int
	entered   chan string
	releases  map[string]<-chan struct{}
}

func (d *controlledDeliveryDeployer) ID() string { return deliveryGateRendererID }

func (*controlledDeliveryDeployer) Requirements() application.ConnectionRequirements {
	return application.ConnectionRequirements{}
}

func (*controlledDeliveryDeployer) ValidateStoredConnection(application.Connection) error { return nil }

func (*controlledDeliveryDeployer) ValidateConnection(application.Connection) error { return nil }

func (*controlledDeliveryDeployer) Probe(context.Context, application.Connection) (application.DeviceInfo, error) {
	return application.DeviceInfo{ProfileKey: "controlled-profile-v1"}, nil
}

func (*controlledDeliveryDeployer) Backup(context.Context, application.DeviceInfo, application.Connection) (application.BackupPayload, error) {
	return application.BackupPayload{Payload: []byte("x")}, nil
}

func (d *controlledDeliveryDeployer) Deploy(ctx context.Context, _ application.DeviceInfo, connection application.Connection, _ application.DeployArtifact) error {
	d.mu.Lock()
	d.active++
	if d.active > d.maxActive {
		d.maxActive = d.active
	}
	d.mu.Unlock()

	d.entered <- connection.URL
	release := d.releases[connection.URL]
	var err error
	select {
	case <-release:
	case <-ctx.Done():
		err = ctx.Err()
	}

	d.mu.Lock()
	d.active--
	d.mu.Unlock()
	return err
}

func (*controlledDeliveryDeployer) Verify(context.Context, application.DeviceInfo, application.Connection, application.DeployArtifact) error {
	return nil
}

func (*controlledDeliveryDeployer) Rollback(context.Context, application.DeviceInfo, application.Connection, application.BackupPayload) error {
	return nil
}

func (d *controlledDeliveryDeployer) activity() (active, maxActive int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active, d.maxActive
}

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type deliveryGateAutomaticSource struct {
	device application.Device
	output application.Output
}

func (s deliveryGateAutomaticSource) Device(context.Context, string) (application.Device, error) {
	return s.device, nil
}

func (deliveryGateAutomaticSource) DeviceCredential(context.Context, string) (string, error) {
	return "controlled-secret", nil
}

func (s deliveryGateAutomaticSource) Output(context.Context, string) (application.Output, error) {
	return s.output, nil
}

func TestServeBackendSerializesEveryDeliveryThroughOneGate(t *testing.T) {
	tests := []struct {
		name       string
		ownerURL   string
		startOwner func(*testing.T, serveBackend, *application.DeliveryGate, string) <-chan error
	}{
		{
			name:     "manual waits for manual",
			ownerURL: "http://manual-owner",
			startOwner: func(_ *testing.T, backend serveBackend, _ *application.DeliveryGate, ownerURL string) <-chan error {
				done := make(chan error, 1)
				go func() {
					_, err := backend.Deploy(context.Background(), deliveryGateCommand(ownerURL))
					done <- err
				}()
				return done
			},
		},
		{
			name:     "manual waits for automatic",
			ownerURL: "http://automatic-owner",
			startOwner: func(t *testing.T, backend serveBackend, gate *application.DeliveryGate, ownerURL string) <-chan error {
				t.Helper()
				source := deliveryGateAutomaticSource{
					device: application.Device{
						ID: "device", Address: ownerURL, AutoDeliver: true,
					},
					output: application.Output{ID: deliveryGateOutputID, DeviceID: "device"},
				}
				automatic, err := application.NewAutomaticDeliveryService(application.AutomaticDeliveryConfig{
					Devices: source, Deployments: backend.deployments, Outputs: source, Gate: gate,
				})
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					runs := automatic.Deliver(context.Background(), []application.ScheduledRun{{Publications: []application.ScheduledPublication{{
						OutputID: deliveryGateOutputID, DeviceID: "device", ArtifactID: deliveryGateArtifactID,
					}}}})
					if runs[0].Delivered != 1 || len(runs[0].DeliveryFailures) != 0 {
						done <- errors.New("automatic delivery did not complete")
						return
					}
					done <- nil
				}()
				return done
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerRelease := make(chan struct{})
			deployer := &controlledDeliveryDeployer{
				entered: make(chan string, 2),
				releases: map[string]<-chan struct{}{
					test.ownerURL:          ownerRelease,
					"http://manual-waiter": make(chan struct{}),
				},
			}
			deploymentService, err := application.NewDeploymentService(application.DeploymentConfig{
				Artifacts: newDeliveryGateArtifacts(),
				Deployers: application.DeployerRegistry{deployer.ID(): deployer},
				Backups:   deliveryGateBackupStore{},
				Clock: application.ClockFunc(func() time.Time {
					return time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			gate := application.NewDeliveryGate()
			backend := serveBackend{deployments: deploymentService, deliveryGate: gate}
			ownerDone := test.startOwner(t, backend, gate, test.ownerURL)

			select {
			case entered := <-deployer.entered:
				if entered != test.ownerURL {
					t.Fatalf("first deployment = %q, want %q", entered, test.ownerURL)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("first deployment did not reach the controlled device")
			}

			waiterBase, cancelWaiter := context.WithCancel(context.Background())
			waiterCtx := &observedDoneContext{Context: waiterBase, observed: make(chan struct{})}
			waiterDone := make(chan error, 1)
			go func() {
				_, err := backend.Deploy(waiterCtx, deliveryGateCommand("http://manual-waiter"))
				waiterDone <- err
			}()

			select {
			case <-waiterCtx.observed:
			case <-time.After(2 * time.Second):
				cancelWaiter()
				close(ownerRelease)
				t.Fatal("waiting deployment never observed cancellation")
			}
			active, maxActive := deployer.activity()
			cancelWaiter()
			waiterErr := <-waiterDone
			close(ownerRelease)
			ownerErr := <-ownerDone

			if active != 1 || maxActive != 1 {
				t.Fatalf("device deployments overlapped: active=%d max_active=%d", active, maxActive)
			}
			if !errors.Is(waiterErr, context.Canceled) {
				t.Fatalf("canceled waiter error = %v", waiterErr)
			}
			if ownerErr != nil {
				t.Fatalf("owning deployment failed: %v", ownerErr)
			}
			if active, maxActive := deployer.activity(); active != 0 || maxActive != 1 {
				t.Fatalf("final device activity: active=%d max_active=%d", active, maxActive)
			}
		})
	}
}

func deliveryGateCommand(url string) application.DeployCommand {
	return application.DeployCommand{
		ArtifactID: deliveryGateArtifactID,
		Connection: application.Connection{URL: url},
		Confirm:    true,
	}
}
