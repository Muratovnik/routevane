package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

const deployTestPassword = "device-secret-value"

// spyDeployer records the exact order of the lifecycle so a test can prove that
// a step which must not happen did not happen.
type spyDeployer struct {
	calls []string

	profileKey string
	backup     []byte

	connectionErr error
	probeErr      error
	backupErr     error
	deployErr     error
	verifyErr     error
	rollbackErr   error

	seenPasswords []string
}

func (s *spyDeployer) ID() string { return "keenetic-route-bat" }

func (s *spyDeployer) Requirements() ConnectionRequirements {
	return ConnectionRequirements{
		AddressLabel: "Device", AddressExample: "http://192.168.1.1",
		NeedsCredential: true, NeedsInterface: true, InterfaceLabel: "Interface",
	}
}

func (s *spyDeployer) ValidateStoredConnection(connection Connection) error {
	if s.connectionErr != nil {
		return s.connectionErr
	}
	if connection.URL == "" || connection.Username == "" || connection.Password != "" {
		return errors.New("non-secret device metadata is required")
	}
	return nil
}

// ValidateConnection stands in for an authenticated device transport: it is the
// deployer, not this package, that decides a credential is required. It records
// nothing, because refusing a connection touches no device.
func (s *spyDeployer) ValidateConnection(connection Connection) error {
	if s.connectionErr != nil {
		return s.connectionErr
	}
	if connection.URL == "" || connection.Username == "" || connection.Password == "" {
		return errors.New("a device address and credential are required")
	}
	return nil
}

func (s *spyDeployer) Probe(_ context.Context, connection Connection) (DeviceInfo, error) {
	s.calls = append(s.calls, StepProbe)
	s.seenPasswords = append(s.seenPasswords, connection.Password)
	if s.probeErr != nil {
		return DeviceInfo{}, s.probeErr
	}
	return DeviceInfo{Vendor: "Keenetic", Model: "Giga", FirmwareVersion: "5.1.2", ProfileKey: s.profileKey, Interface: "Wireguard0"}, nil
}

func (s *spyDeployer) Backup(context.Context, DeviceInfo, Connection) (BackupPayload, error) {
	s.calls = append(s.calls, StepBackup)
	if s.backupErr != nil {
		return BackupPayload{}, s.backupErr
	}
	return BackupPayload{Payload: s.backup}, nil
}

func (s *spyDeployer) Deploy(context.Context, DeviceInfo, Connection, DeployArtifact) error {
	s.calls = append(s.calls, StepDeploy)
	return s.deployErr
}

func (s *spyDeployer) Verify(context.Context, DeviceInfo, Connection, DeployArtifact) error {
	s.calls = append(s.calls, StepVerify)
	return s.verifyErr
}

func (s *spyDeployer) Rollback(_ context.Context, _ DeviceInfo, _ Connection, backup BackupPayload) error {
	s.calls = append(s.calls, StepRollback)
	if len(backup.Payload) == 0 {
		return errors.New("rollback was given no backup")
	}
	return s.rollbackErr
}

type memoryBackups struct {
	stored [][]byte
	err    error
	broken bool
}

func (m *memoryBackups) PutBackup(_ context.Context, _ string, takenAt time.Time, payload []byte) (BackupRef, error) {
	if m.err != nil {
		return BackupRef{}, m.err
	}
	m.stored = append(m.stored, append([]byte(nil), payload...))
	if m.broken {
		// A store that cannot describe what it wrote is not a usable backup.
		return BackupRef{ID: "incomplete"}, nil
	}
	return BackupRef{ID: "backup-1", Path: "backups/keenetic/one.conf", Hash: strings.Repeat("c", 64), SizeBytes: int64(len(payload)), CreatedAt: takenAt}, nil
}

func deployTestTarget() domain.TargetProfile {
	return domain.TargetProfile{ID: "keenetic", ProfileKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat"}
}

func deployTestRequest() DeployRequest {
	return DeployRequest{
		Target:     deployTestTarget(),
		Connection: Connection{URL: "http://192.168.1.1", Username: "admin", Password: deployTestPassword, Interface: "Wireguard0"},
		Artifact: DeployArtifact{
			ArtifactID: strings.Repeat("a", 32), RendererID: "keenetic-route-bat",
			ArtifactHash: strings.Repeat("b", 64), ContentType: "application/x-bat",
			Payload: []byte("route ADD 192.0.2.10 MASK 255.255.255.255 0.0.0.0\r\n"),
		},
	}
}

func fixedClock() Clock {
	moment := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return ClockFunc(func() time.Time {
		moment = moment.Add(time.Millisecond)
		return moment
	})
}

func TestDeploySucceedsOnlyAfterProbeBackupDeployAndVerify(t *testing.T) {
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	backups := &memoryBackups{}
	result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, backups, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deployer.calls, ",") != "probe,backup,deploy,verify" {
		t.Fatalf("lifecycle = %v", deployer.calls)
	}
	if !result.Applied || result.RolledBack {
		t.Fatalf("result = %#v", result)
	}
	if len(backups.stored) != 1 || string(backups.stored[0]) != "startup-config" {
		t.Fatalf("stored backups = %#v", backups.stored)
	}
	if result.Backup.Hash == "" || result.Device.DeployerID != deployer.ID() {
		t.Fatalf("result = %#v", result)
	}
	steps := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		steps = append(steps, event.Step+":"+event.Outcome)
		if event.Duration <= 0 {
			t.Fatalf("event %#v has no measured duration", event)
		}
	}
	if strings.Join(steps, ",") != "probe:success,backup:success,deploy:success,verify:success" {
		t.Fatalf("audit = %v", steps)
	}
}

func TestDeployRefusesAnIncompatibleDeviceBeforeTouchingIt(t *testing.T) {
	cases := map[string]string{
		"firmware reports no profile":      "",
		"firmware reports another profile": "keenetic-bat-ipv4-v2",
	}
	for name, profileKey := range cases {
		t.Run(name, func(t *testing.T) {
			deployer := &spyDeployer{profileKey: profileKey, backup: []byte("startup-config")}
			backups := &memoryBackups{}
			result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, backups, fixedClock())
			if !errors.Is(err, ErrDeviceIncompatible) {
				t.Fatalf("err = %v, want ErrDeviceIncompatible", err)
			}
			// Nothing beyond the probe may run, so nothing on the device changed
			// and no backup was even taken.
			if strings.Join(deployer.calls, ",") != "probe" {
				t.Fatalf("lifecycle = %v", deployer.calls)
			}
			if len(backups.stored) != 0 {
				t.Fatal("an incompatible device must not produce a backup")
			}
			if result.Applied || result.RolledBack {
				t.Fatalf("result = %#v", result)
			}
			// The version it found is reported, which is what makes the refusal
			// actionable.
			if result.Device.FirmwareVersion != "5.1.2" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestDeployNeverStartsWithoutAVerifiedBackup(t *testing.T) {
	cases := map[string]func(*spyDeployer, *memoryBackups){
		"the device cannot produce one": func(deployer *spyDeployer, _ *memoryBackups) {
			deployer.backupErr = errors.New("device refused")
		},
		"the device produced an empty one": func(deployer *spyDeployer, _ *memoryBackups) {
			deployer.backup = nil
		},
		"the store cannot keep it": func(_ *spyDeployer, backups *memoryBackups) {
			backups.err = errors.New("disk full")
		},
		"the stored copy is not verifiable": func(_ *spyDeployer, backups *memoryBackups) {
			backups.broken = true
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
			backups := &memoryBackups{}
			arrange(deployer, backups)
			_, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, backups, fixedClock())
			if !errors.Is(err, ErrBackupRequired) {
				t.Fatalf("err = %v, want ErrBackupRequired", err)
			}
			for _, call := range deployer.calls {
				if call == StepDeploy {
					t.Fatalf("a deployment started without a verified backup: %v", deployer.calls)
				}
			}
		})
	}
}

func TestAFailedVerifyRollsBackFromTheBackupTakenBefore(t *testing.T) {
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), verifyErr: errors.New("two routes missing")}
	result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("err = %v, want ErrVerifyFailed", err)
	}
	if strings.Join(deployer.calls, ",") != "probe,backup,deploy,verify,rollback" {
		t.Fatalf("lifecycle = %v", deployer.calls)
	}
	if result.Applied || !result.RolledBack {
		t.Fatalf("result = %#v", result)
	}
	outcomes := map[string]string{}
	for _, event := range result.Events {
		outcomes[event.Step] = event.Outcome
	}
	if outcomes[StepVerify] != "failed" || outcomes[StepRollback] != "success" {
		t.Fatalf("audit = %#v", result.Events)
	}
}

func TestAFailedDeployRollsBackAndAFailedRollbackReportsBoth(t *testing.T) {
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), deployErr: errors.New("device refused a command")}
	result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrDeployFailed) {
		t.Fatalf("err = %v, want ErrDeployFailed", err)
	}
	if strings.Join(deployer.calls, ",") != "probe,backup,deploy,rollback" {
		t.Fatalf("a failed deploy must not be verified: %v", deployer.calls)
	}
	if !result.RolledBack {
		t.Fatalf("result = %#v", result)
	}

	stuck := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), deployErr: errors.New("refused"), rollbackErr: errors.New("device unreachable")}
	stuckResult, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{stuck.ID(): stuck}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("err = %v, want ErrRollbackFailed", err)
	}
	if stuckResult.RolledBack {
		t.Fatal("a failed rollback must not be reported as rolled back")
	}
	// Both failures are reported: the operator needs to know the device was left
	// changed and why the restore did not work.
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("the deploy failure was lost: %v", err)
	}
}

func TestDeployRefusesAnIncoherentComposition(t *testing.T) {
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	registry := DeployerRegistry{deployer.ID(): deployer}
	base := deployTestRequest()
	cases := map[string]func(*DeployRequest){
		"no artifact bytes":    func(r *DeployRequest) { r.Artifact.Payload = nil },
		"no artifact identity": func(r *DeployRequest) { r.Artifact.ArtifactID = "" },
		"no artifact hash":     func(r *DeployRequest) { r.Artifact.ArtifactHash = "" },
		"foreign renderer":     func(r *DeployRequest) { r.Artifact.RendererID = "singbox-ruleset-json" },
		"no device address":    func(r *DeployRequest) { r.Connection.URL = "" },
		"no device account":    func(r *DeployRequest) { r.Connection.Username = "" },
		"no device credential": func(r *DeployRequest) { r.Connection.Password = "" },
		"no target":            func(r *DeployRequest) { r.Target = domain.TargetProfile{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			request := base
			mutate(&request)
			if _, err := DeployToDevice(context.Background(), request, registry, &memoryBackups{}, fixedClock()); err == nil {
				t.Fatal("an incoherent deployment was accepted")
			}
			if len(deployer.calls) != 0 {
				t.Fatalf("an incoherent deployment reached the device: %v", deployer.calls)
			}
		})
	}
	// A target whose renderer has no deployer is refused before the device is
	// contacted.
	unknown := base
	unknown.Target.RendererID = "raw-json"
	unknown.Artifact.RendererID = "raw-json"
	if _, err := DeployToDevice(context.Background(), unknown, registry, &memoryBackups{}, fixedClock()); !errors.Is(err, ErrDeployerUnavailable) {
		t.Fatalf("err = %v, want ErrDeployerUnavailable", err)
	}
}

func TestTheAuditTrailNeverCarriesTheDeviceCredential(t *testing.T) {
	deployer := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), verifyErr: errors.New("mismatch")}
	result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if err == nil {
		t.Fatal("expected the verification to fail")
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), deployTestPassword) {
		t.Fatalf("the audit record leaked the credential: %s", encoded)
	}
	if strings.Contains(err.Error(), deployTestPassword) {
		t.Fatalf("the error leaked the credential: %v", err)
	}
	// The deployer does receive the credential, which is why the redaction helper
	// exists for anything that is reported.
	if len(deployer.seenPasswords) == 0 || deployer.seenPasswords[0] != deployTestPassword {
		t.Fatalf("the deployer never received the credential: %#v", deployer.seenPasswords)
	}
	redacted := deployTestRequest().Connection.Redacted()
	if redacted.Password != "" || redacted.Username != "admin" {
		t.Fatalf("redacted = %#v", redacted)
	}
}
