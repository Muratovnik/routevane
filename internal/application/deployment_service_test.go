package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// stubArtifacts stands in for publication. It answers only what a deployment
// asks of it, so a test can withhold exactly one answer and see what a
// deployment does without it.
type stubArtifacts struct {
	payload  ArtifactPayload
	output   Output
	target   domain.TargetProfile
	snapshot PlanSnapshotRecord

	artifactErr error
	outputErr   error
	targetErr   error
	snapshotErr error

	snapshotIDs []string
}

func (s *stubArtifacts) Artifact(context.Context, string) (ArtifactPayload, error) {
	return s.payload, s.artifactErr
}

func (s *stubArtifacts) Output(context.Context, string) (Output, error) {
	return s.output, s.outputErr
}

func (s *stubArtifacts) Snapshot(_ context.Context, id string) (PlanSnapshotRecord, error) {
	s.snapshotIDs = append(s.snapshotIDs, id)
	return s.snapshot, s.snapshotErr
}

func (s *stubArtifacts) TargetProfile(string) (domain.TargetProfile, error) {
	return s.target, s.targetErr
}

func (s *stubArtifacts) Targets() []TargetOption {
	return []TargetOption{{ID: s.target.ID, Title: "Keenetic", RendererID: s.target.RendererID}}
}

const (
	deploymentArtifactID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deploymentSnapshotID = "cccccccccccccccccccccccccccccccc"
	deploymentOutputID   = "dddddddddddddddddddddddddddddddd"
)

func deploymentTestArtifacts() *stubArtifacts {
	target := deployTestTarget()
	return &stubArtifacts{
		payload: ArtifactPayload{
			Artifact: ArtifactBuildRecord{
				ID: deploymentArtifactID, OutputID: deploymentOutputID, PlanSnapshotID: deploymentSnapshotID,
				RendererID: target.RendererID, ArtifactHash: strings.Repeat("b", 64),
				ContentType: "application/x-bat", SizeBytes: 51,
			},
			Payload: []byte("route ADD 192.0.2.10 MASK 255.255.255.255 0.0.0.0\r\n"),
		},
		output: Output{ID: deploymentOutputID, TargetID: target.ID},
		target: target,
		snapshot: PlanSnapshotRecord{
			ID: deploymentSnapshotID, OutputID: deploymentOutputID,
			RoutingPlanHash: strings.Repeat("e", 64), Status: "valid",
		},
	}
}

func deploymentTestService(t *testing.T, artifacts ArtifactSource, deployer Deployer) *DeploymentService {
	t.Helper()
	service, err := NewDeploymentService(DeploymentConfig{
		Artifacts: artifacts,
		Deployers: DeployerRegistry{deployer.ID(): deployer},
		Backups:   &memoryBackups{},
		Clock:     fixedClock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func deploymentTestCommand(confirm bool) DeployCommand {
	return DeployCommand{
		ArtifactID: deploymentArtifactID,
		Connection: Connection{URL: "http://192.168.1.1", Username: "admin", Password: deployTestPassword, Interface: "Wireguard0"},
		Confirm:    confirm,
	}
}

// The audit record must name the plan that was applied, not the rendering of it
// twice. The two hashes answer different questions, and a record that repeats
// the artifact hash under the plan's name reads as provenance while carrying
// none.
func TestDeployRecordsThePlanHashAndNotTheArtifactHashAgain(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("previous device state")}
	service := deploymentTestService(t, artifacts, deployer)

	result, err := service.Deploy(context.Background(), deploymentTestCommand(true))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("result = %#v", result)
	}
	if len(artifacts.snapshotIDs) != 1 || artifacts.snapshotIDs[0] != deploymentSnapshotID {
		t.Fatalf("snapshot lookups = %#v, want the artifact's own snapshot", artifacts.snapshotIDs)
	}
	verify := auditDetail(t, result, StepVerify)
	if verify != artifacts.snapshot.RoutingPlanHash {
		t.Fatalf("verify detail = %q, want the plan hash %q", verify, artifacts.snapshot.RoutingPlanHash)
	}
	if verify == artifacts.payload.Artifact.ArtifactHash {
		t.Fatal("the plan hash and the artifact hash are the same value: provenance is lost")
	}
	if deploy := auditDetail(t, result, StepDeploy); deploy != artifacts.payload.Artifact.ArtifactHash {
		t.Fatalf("deploy detail = %q, want the artifact hash", deploy)
	}
}

// A deployment that cannot say which plan it is applying is refused rather than
// audited with a placeholder, and refused before the device is touched.
func TestDeployRefusesWhenThePlanProvenanceIsUnreadable(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*stubArtifacts)
		wantCall bool
	}{
		{"the snapshot cannot be read", func(a *stubArtifacts) { a.snapshotErr = ErrNotFound }, false},
		{"the snapshot carries no plan hash", func(a *stubArtifacts) { a.snapshot.RoutingPlanHash = "" }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			artifacts := deploymentTestArtifacts()
			test.mutate(artifacts)
			deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("previous device state")}
			service := deploymentTestService(t, artifacts, deployer)

			_, err := service.Deploy(context.Background(), deploymentTestCommand(true))
			if !errors.Is(err, ErrDeployComposition) {
				t.Fatalf("err = %v, want a composition refusal", err)
			}
			if len(deployer.calls) != 0 {
				t.Fatalf("the device was contacted anyway: %#v", deployer.calls)
			}
		})
	}
}

// Planning contacts no device and asks for no provenance: it is what a caller
// shows before the operator agrees to change anything.
func TestPlanDescribesTheDeploymentWithoutContactingTheDevice(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1"}
	service := deploymentTestService(t, artifacts, deployer)

	plan, err := service.Plan(context.Background(), deploymentTestCommand(false))
	if err != nil {
		t.Fatal(err)
	}
	if plan.ArtifactID != deploymentArtifactID || plan.TargetID != artifacts.target.ID || plan.DeployerID != deployer.ID() {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.ArtifactHash != artifacts.payload.Artifact.ArtifactHash {
		t.Fatalf("plan hash = %q", plan.ArtifactHash)
	}
	if len(deployer.calls) != 0 || len(artifacts.snapshotIDs) != 0 {
		t.Fatalf("planning reached past itself: calls=%#v snapshots=%#v", deployer.calls, artifacts.snapshotIDs)
	}
}

func TestStoredConnectionValidationUsesTheTargetsDeployerWithoutASecret(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1"}
	service := deploymentTestService(t, artifacts, deployer)
	connection := deploymentTestCommand(false).Connection.Redacted()
	if err := service.ValidateStoredConnection(artifacts.target.ID, connection); err != nil {
		t.Fatal(err)
	}
	connection.Password = deployTestPassword
	if err := service.ValidateStoredConnection(artifacts.target.ID, connection); !errors.Is(err, ErrConnectionInvalid) {
		t.Fatalf("persisted password err = %v", err)
	}
	deployer.connectionErr = errors.New("unsafe destination")
	connection.Password = ""
	if err := service.ValidateStoredConnection(artifacts.target.ID, connection); !errors.Is(err, ErrConnectionInvalid) {
		t.Fatalf("destination err = %v", err)
	}
}

func TestDeployRefusesWithoutConfirmation(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1"}
	service := deploymentTestService(t, artifacts, deployer)

	_, err := service.Deploy(context.Background(), deploymentTestCommand(false))
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("err = %v", err)
	}
	if len(deployer.calls) != 0 || len(artifacts.snapshotIDs) != 0 {
		t.Fatalf("an unconfirmed deployment did work: calls=%#v snapshots=%#v", deployer.calls, artifacts.snapshotIDs)
	}
}

// An artifact built by one renderer must not be installed through a target that
// expects another, whichever id a caller names.
func TestDeployRefusesAnArtifactBuiltForAnotherTarget(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	artifacts.payload.Artifact.RendererID = "singbox-rule-set"
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1"}
	service := deploymentTestService(t, artifacts, deployer)

	_, err := service.Deploy(context.Background(), deploymentTestCommand(true))
	if !errors.Is(err, ErrDeployComposition) {
		t.Fatalf("err = %v", err)
	}
	if len(deployer.calls) != 0 {
		t.Fatalf("the device was contacted anyway: %#v", deployer.calls)
	}
}

// The rollback must survive the cancellation that failed the deployment. A
// caller who gave up, or a request whose deadline expired mid-write, is exactly
// when the device is already changed and the backup is the only way back.
func TestRollbackRunsAfterTheCallerContextIsCancelled(t *testing.T) {
	deployer := &cancellingDeployer{spyDeployer: spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("previous device state")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deployer.cancel = cancel

	result, err := DeployToDevice(ctx, deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if err == nil {
		t.Fatal("a cancelled deployment reported success")
	}
	if errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("the rollback inherited the cancellation that failed the deploy: %v", err)
	}
	if !result.RolledBack {
		t.Fatalf("the device was left changed: result = %#v events = %#v", result, result.Events)
	}
	if deployer.rollbackCtxErr != nil {
		t.Fatalf("the rollback ran on a dead context: %v", deployer.rollbackCtxErr)
	}
	if deployer.rollbackDeadline.IsZero() {
		t.Fatal("the rollback ran without a deadline of its own")
	}
}

// cancellingDeployer cancels the caller's context from inside the deploy step,
// which is how a request deadline or a closed screen actually arrives: after the
// device has been touched.
type cancellingDeployer struct {
	spyDeployer
	cancel context.CancelFunc

	rollbackCtxErr   error
	rollbackDeadline time.Time
}

func (c *cancellingDeployer) Deploy(ctx context.Context, device DeviceInfo, connection Connection, artifact DeployArtifact) error {
	c.cancel()
	if err := c.spyDeployer.Deploy(ctx, device, connection, artifact); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *cancellingDeployer) Rollback(ctx context.Context, device DeviceInfo, connection Connection, backup BackupPayload) error {
	c.rollbackCtxErr = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		c.rollbackDeadline = deadline
	}
	return c.spyDeployer.Rollback(ctx, device, connection, backup)
}

func auditDetail(t *testing.T, result DeployResult, step string) string {
	t.Helper()
	for _, event := range result.Events {
		if event.Step == step {
			return event.Detail
		}
	}
	t.Fatalf("no %q event in %#v", step, result.Events)
	return ""
}
