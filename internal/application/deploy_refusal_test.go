package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A deployer reports what the device did by wrapping a device-neutral reason
// beside its own error; the code names that reason unless a step's own
// outcome outranks it.
func TestDeploymentFailureCodeNamesWhatTheDeviceRefused(t *testing.T) {
	deployerSays := func(reason error) error { return fmt.Errorf("vendor transport: %w: status 401", reason) }
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"rejected credential", fmt.Errorf("%w: %w", ErrDeployFailed, deployerSays(ErrDeviceAuthRejected)), "device_auth_rejected"},
		{"unreachable", fmt.Errorf("%w: %w", ErrDeployFailed, deployerSays(ErrDeviceUnreachable)), "device_unreachable"},
		{"rejected command", fmt.Errorf("%w: %w", ErrDeployFailed, deployerSays(ErrDeviceCommandRejected)), "device_command_rejected"},
		{"unusable answer", fmt.Errorf("%w: %w", ErrDeployFailed, deployerSays(ErrDeviceAnswerInvalid)), "device_answer_invalid"},
		// A missing interface is reported as an answer too; the narrower code wins.
		{"missing interface", errors.Join(ErrDeviceAnswerInvalid, ErrDeviceInterfaceMissing), "device_interface_missing"},
		{"read-only failure", errors.Join(ErrDeviceUnchanged, deployerSays(ErrDeviceUnreachable)), "device_unreachable"},
		{"unclassified", fmt.Errorf("%w: %w", ErrDeployFailed, errors.New("something else")), "deploy_failed"},
		// The step's own outcome is what the operator must act on first.
		{"failed rollback", errors.Join(ErrRollbackFailed, ErrDeviceCommandRejected), "rollback_failed"},
		{"failed verification", errors.Join(ErrVerifyFailed, ErrDeviceUnreachable), "verify_failed"},
		{"missing backup", errors.Join(ErrBackupRequired, ErrDeviceUnreachable), "backup_unavailable"},
		{"refused connection", errors.Join(ErrConnectionInvalid, ErrDeviceCommandRejected), "connection_invalid"},
		{"ownership conflict", errors.Join(ErrFQDNOwnershipConflict, ErrDeviceAnswerInvalid), "fqdn_ownership_conflict"},
	} {
		if got := DeploymentFailureCode(test.err); got != test.want {
			t.Errorf("%s: code = %q, want %q", test.name, got, test.want)
		}
	}
}

// The lifecycle used to flatten a probe and a deploy failure into text, which
// left every device refusal reading as an unclassified failure.
func TestALifecycleFailureKeepsTheDevicesReasonAndItsStep(t *testing.T) {
	refused := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), probeErr: fmt.Errorf("login: %w", ErrDeviceAuthRejected)}
	result, err := DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{refused.ID(): refused}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrDeviceAuthRejected) || DeploymentFailureCode(err) != "device_auth_rejected" || result.FailedStep() != StepProbe {
		t.Fatalf("refused login: err=%v code=%s step=%q", err, DeploymentFailureCode(err), result.FailedStep())
	}
	if strings.Join(refused.calls, ",") != "probe" {
		t.Fatalf("a refused login went past the probe: %v", refused.calls)
	}

	rejected := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), deployErr: fmt.Errorf("batch: %w", ErrDeviceCommandRejected)}
	result, err = DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{rejected.ID(): rejected}, &memoryBackups{}, fixedClock())
	if DeploymentFailureCode(err) != "device_command_rejected" || result.FailedStep() != StepDeploy || !result.RolledBack {
		t.Fatalf("rejected command: err=%v code=%s step=%q result=%#v", err, DeploymentFailureCode(err), result.FailedStep(), result)
	}

	stuck := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config"), deployErr: fmt.Errorf("batch: %w", ErrDeviceCommandRejected), rollbackErr: fmt.Errorf("restore: %w", ErrDeviceUnreachable)}
	result, err = DeployToDevice(context.Background(), deployTestRequest(), DeployerRegistry{stuck.ID(): stuck}, &memoryBackups{}, fixedClock())
	if DeploymentFailureCode(err) != "rollback_failed" || result.FailedStep() != StepRollback {
		t.Fatalf("failed rollback: err=%v code=%s step=%q", err, DeploymentFailureCode(err), result.FailedStep())
	}

	if step := (DeployResult{Events: []DeployEvent{{Step: StepProbe, Outcome: "success"}}}).FailedStep(); step != "" {
		t.Fatalf("a result without a failed step named %q", step)
	}
}

// fqdnSpyDeployer reads the device on a preview, as the DNS deployer does.
type fqdnSpyDeployer struct {
	*spyDeployer
	previewErr error
}

func (*fqdnSpyDeployer) FQDNEndpoint(Connection) (string, error) { return "device-endpoint", nil }
func (*fqdnSpyDeployer) DesiredFQDNGroups(DeviceInfo, DeployArtifact) ([]ManagedFQDNGroup, error) {
	return nil, nil
}
func (f *fqdnSpyDeployer) PreviewFQDNGroups(context.Context, DeviceInfo, Connection, DeployArtifact) ([]string, error) {
	f.calls = append(f.calls, StepPreview)
	return nil, f.previewErr
}

type emptyFQDNLedger struct{}

func (emptyFQDNLedger) ManagedFQDNOwnership(context.Context, string, string) (ManagedFQDNOwnership, error) {
	return ManagedFQDNOwnership{}, nil
}
func (emptyFQDNLedger) ReplaceManagedFQDNOwnership(context.Context, ManagedFQDNOwnership) error {
	return nil
}
func (emptyFQDNLedger) RetireManagedFQDNOwnership(context.Context, string, string) error {
	return nil
}

func TestAPreviewRefusalNamesTheStepThatReachedTheDevice(t *testing.T) {
	for _, test := range []struct {
		name               string
		probeErr, preview  error
		wantCode, wantStep string
	}{
		{"refused login", fmt.Errorf("login: %w", ErrDeviceAuthRejected), nil, "device_auth_rejected", StepProbe},
		{"missing interface", errors.Join(ErrDeviceAnswerInvalid, ErrDeviceInterfaceMissing), nil, "device_interface_missing", StepProbe},
		{"unusable observation", nil, fmt.Errorf("groups: %w", ErrDeviceAnswerInvalid), "device_answer_invalid", StepPreview},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifacts := deploymentTestArtifacts()
			deployer := &fqdnSpyDeployer{spyDeployer: &spyDeployer{formatKey: artifacts.target.FormatKey, probeErr: test.probeErr}, previewErr: test.preview}
			deployments, err := NewDeploymentService(DeploymentConfig{
				Artifacts: artifacts, Deployers: DeployerRegistry{deployer.ID(): deployer},
				Backups: &memoryBackups{}, ManagedFQDN: emptyFQDNLedger{}, Clock: fixedClock(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = deployments.Plan(context.Background(), deploymentTestCommand(false))
			if !errors.Is(err, ErrDeployFailed) || DeploymentFailureCode(err) != test.wantCode || RefusalStep(err) != test.wantStep {
				t.Fatalf("err=%v code=%s step=%q", err, DeploymentFailureCode(err), RefusalStep(err))
			}
			for _, call := range deployer.calls {
				if call == StepBackup || call == StepDeploy {
					t.Fatalf("a preview wrote: %v", deployer.calls)
				}
			}
		})
	}
	if step := RefusalStep(errors.New("no step")); step != "" {
		t.Fatalf("an error without a step named %q", step)
	}
}
