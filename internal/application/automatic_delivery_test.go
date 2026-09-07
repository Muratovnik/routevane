package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type automaticDeviceSource struct {
	devices map[string]Device
	secrets map[string]string
	outputs map[string]Output
}

func (s automaticDeviceSource) Output(_ context.Context, id string) (Output, error) {
	output, ok := s.outputs[id]
	if !ok {
		return Output{}, ErrNotFound
	}
	return output, nil
}

func (s automaticDeviceSource) Device(_ context.Context, id string) (Device, error) {
	device, ok := s.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	return device, nil
}

func (s automaticDeviceSource) DeviceCredential(_ context.Context, id string) (string, error) {
	secret, ok := s.secrets[id]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

type automaticDeploymentTarget struct {
	commands []DeployCommand
	failures map[string]error
	deploy   func(context.Context, DeployCommand) error
}

func (s *automaticDeploymentTarget) Deploy(ctx context.Context, command DeployCommand) (DeployResult, error) {
	s.commands = append(s.commands, command)
	if s.deploy != nil {
		if err := s.deploy(ctx, command); err != nil {
			return DeployResult{}, err
		}
	}
	if err := s.failures[command.ArtifactID]; err != nil {
		return DeployResult{}, err
	}
	return DeployResult{ArtifactID: command.ArtifactID, Applied: true}, nil
}

func TestAutomaticDeliveryUsesOnlyExplicitOptedInBindingsAndContinuesAfterFailure(t *testing.T) {
	devices := automaticDeviceSource{
		devices: map[string]Device{
			"off":  {ID: "off", AutoDeliver: false},
			"bad":  {ID: "bad", Address: "http://bad", Account: "admin", Interface: "Wireguard0", AutoDeliver: true},
			"good": {ID: "good", Address: "http://good", Account: "root", Interface: "wg1", AutoDeliver: true},
		},
		secrets: map[string]string{"bad": "bad-secret", "good": "good-secret"},
		outputs: map[string]Output{
			"off-output":  {ID: "off-output", DeviceID: "off"},
			"bad-output":  {ID: "bad-output", DeviceID: "bad"},
			"good-output": {ID: "good-output", DeviceID: "good"},
		},
	}
	deployments := &automaticDeploymentTarget{failures: map[string]error{"artifact-bad": ErrVerifyFailed}}
	delivery, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{Devices: devices, Deployments: deployments, Outputs: devices, Gate: NewDeliveryGate()})
	if err != nil {
		t.Fatal(err)
	}
	runs := delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{
		{OutputID: "file-only", ArtifactID: "artifact-file"},
		{OutputID: "off-output", DeviceID: "off", ArtifactID: "artifact-off"},
		{OutputID: "bad-output", DeviceID: "bad", ArtifactID: "artifact-bad"},
		{OutputID: "good-output", DeviceID: "good", ArtifactID: "artifact-good"},
	}}})
	if len(deployments.commands) != 2 || deployments.commands[0].ArtifactID != "artifact-bad" || deployments.commands[1].ArtifactID != "artifact-good" {
		t.Fatalf("commands = %#v", deployments.commands)
	}
	good := deployments.commands[1]
	if !good.Confirm || good.Connection.URL != "http://good" || good.Connection.Username != "root" || good.Connection.Password != "good-secret" || good.Connection.Interface != "wg1" {
		t.Fatalf("good command = %#v", good)
	}
	if runs[0].Delivered != 1 || len(runs[0].DeliveryFailures) != 1 {
		t.Fatalf("run = %#v", runs[0])
	}
	failure := runs[0].DeliveryFailures[0]
	if failure.OutputID != "bad-output" || failure.DeviceID != "bad" || failure.Code != "verification_failed" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestManualAndAutomaticDeliveryUseTheSameManagedRouteLedger(t *testing.T) {
	artifacts := deploymentTestArtifacts()
	artifacts.output.DeviceID = "router"
	prefix := managedPrefix("192.0.2.10/32")
	base := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("previous device state")}
	deployer := &managedSpyDeployer{
		spyDeployer: base,
		desired:     managedSpecs(prefix),
		current:     [][]ManagedRouteSpec{nil, managedSpecs(prefix), managedSpecs(prefix), managedSpecs(prefix)},
	}
	ledger := &memoryManagedRoutes{}
	deployments, err := NewDeploymentService(DeploymentConfig{
		Artifacts: artifacts, Deployers: DeployerRegistry{deployer.ID(): deployer},
		Backups: &memoryBackups{}, ManagedRoutes: ledger, Clock: fixedClock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deployments.Deploy(context.Background(), deploymentTestCommand(true)); err != nil {
		t.Fatal(err)
	}

	devices := automaticDeviceSource{
		devices: map[string]Device{"router": {ID: "router", Address: "http://192.168.1.1", Account: "admin", Interface: "Wireguard0", AutoDeliver: true}},
		secrets: map[string]string{"router": deployTestPassword},
	}
	automatic, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{
		Devices: devices, Deployments: deployments, Outputs: artifacts, Gate: NewDeliveryGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runs := automatic.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{{
		OutputID: deploymentOutputID, DeviceID: "router", ArtifactID: deploymentArtifactID,
	}}}})
	if runs[0].Delivered != 1 || len(runs[0].DeliveryFailures) != 0 {
		t.Fatalf("automatic delivery = %#v", runs)
	}
	if ledger.replaces != 2 || len(ledger.state.Routes) != 1 || ledger.state.Routes[0].Prefix != prefix || !ledger.state.Routes[0].CreatedByRoutevane {
		t.Fatalf("manual and automatic delivery did not share ownership: %#v", ledger)
	}
	if len(ledger.state.Claims) != 1 || ledger.state.Claims[0].OutputID != deploymentOutputID {
		t.Fatalf("claim = %#v", ledger.state.Claims)
	}
}

func TestAutomaticDeliveryReportsMissingCredentialsWithoutCallingADeployer(t *testing.T) {
	devices := automaticDeviceSource{
		devices: map[string]Device{"router": {ID: "router", AutoDeliver: true}},
		outputs: map[string]Output{"output": {ID: "output", DeviceID: "router"}},
	}
	deployments := &automaticDeploymentTarget{}
	delivery, _ := NewAutomaticDeliveryService(AutomaticDeliveryConfig{Devices: devices, Deployments: deployments, Outputs: devices, Gate: NewDeliveryGate()})
	runs := delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{{OutputID: "output", DeviceID: "router", ArtifactID: "artifact"}}}})
	if len(deployments.commands) != 0 || len(runs[0].DeliveryFailures) != 1 || runs[0].DeliveryFailures[0].Code != "credential_unavailable" {
		t.Fatalf("commands = %#v run = %#v", deployments.commands, runs[0])
	}
}

func TestAutomaticDeliveryStopsWhenTheBindingChangedDuringRefresh(t *testing.T) {
	source := automaticDeviceSource{
		devices: map[string]Device{"old": {ID: "old", AutoDeliver: true}},
		secrets: map[string]string{"old": "secret"},
		outputs: map[string]Output{"output": {ID: "output"}},
	}
	deployments := &automaticDeploymentTarget{}
	delivery, _ := NewAutomaticDeliveryService(AutomaticDeliveryConfig{Devices: source, Deployments: deployments, Outputs: source, Gate: NewDeliveryGate()})
	runs := delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{{OutputID: "output", DeviceID: "old", ArtifactID: "artifact"}}}})
	if len(deployments.commands) != 0 || runs[0].Delivered != 0 || len(runs[0].DeliveryFailures) != 0 {
		t.Fatalf("stale binding was acted on: commands = %#v run = %#v", deployments.commands, runs[0])
	}
}

func TestAutomaticDeliveryCompositionIsRequired(t *testing.T) {
	if _, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{}); !errors.Is(err, ErrAutomaticDeliveryComposition) {
		t.Fatalf("err = %v", err)
	}
}

func TestAutomaticDeliveryHoldsAuthorizationThroughTheDeviceSideEffect(t *testing.T) {
	gate := NewDeliveryGate()
	source := automaticDeviceSource{
		devices: map[string]Device{"router": {ID: "router", Address: "http://192.168.1.1", Account: "admin", Interface: "Wireguard0", AutoDeliver: true}},
		secrets: map[string]string{"router": "secret"},
		outputs: map[string]Output{"output": {ID: "output", DeviceID: "router"}},
	}
	started := make(chan struct{})
	finish := make(chan struct{})
	deployments := &automaticDeploymentTarget{deploy: func(context.Context, DeployCommand) error {
		close(started)
		<-finish
		return nil
	}}
	delivery, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{Devices: source, Deployments: deployments, Outputs: source, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	delivered := make(chan []ScheduledRun, 1)
	go func() {
		delivered <- delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{{OutputID: "output", DeviceID: "router", ArtifactID: "artifact"}}}})
	}()
	<-started

	mutationCtx, cancelMutation := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelMutation()
	if release, err := gate.Acquire(mutationCtx); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			release()
		}
		t.Fatalf("an authorization mutation interleaved with deploy: %v", err)
	}
	if source.outputs["output"].DeviceID != "router" {
		t.Fatal("the binding changed while its authorized delivery was in progress")
	}
	detached := make(chan struct{})
	go func() {
		release, err := gate.Acquire(context.Background())
		if err == nil {
			source.outputs["output"] = Output{ID: "output"}
			release()
		}
		close(detached)
	}()
	select {
	case <-detached:
		t.Fatal("a queued detach interleaved before the deployment returned")
	case <-time.After(20 * time.Millisecond):
	}

	close(finish)
	runs := <-delivered
	if runs[0].Delivered != 1 {
		t.Fatalf("delivery = %#v", runs[0])
	}
	<-detached
	if source.outputs["output"].DeviceID != "" {
		t.Fatal("the detach did not take effect after delivery released authorization")
	}
}

func TestAutomaticDeliveryGivesEachSiblingAFreshBudget(t *testing.T) {
	const budget = 20 * time.Millisecond
	source := automaticDeviceSource{
		devices: map[string]Device{
			"first":  {ID: "first", AutoDeliver: true},
			"second": {ID: "second", AutoDeliver: true},
		},
		secrets: map[string]string{"first": "one", "second": "two"},
		outputs: map[string]Output{
			"first-output":  {ID: "first-output", DeviceID: "first"},
			"second-output": {ID: "second-output", DeviceID: "second"},
		},
	}
	deployments := &automaticDeploymentTarget{deploy: func(ctx context.Context, command DeployCommand) error {
		if command.ArtifactID == "first-artifact" {
			<-ctx.Done()
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < budget/2 {
			return errors.New("second attempt inherited an exhausted sibling budget")
		}
		return nil
	}}
	delivery, err := NewAutomaticDeliveryService(AutomaticDeliveryConfig{
		Devices: source, Deployments: deployments, Outputs: source,
		Gate: NewDeliveryGate(), AttemptBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	runs := delivery.Deliver(context.Background(), []ScheduledRun{{Publications: []ScheduledPublication{
		{OutputID: "first-output", DeviceID: "first", ArtifactID: "first-artifact"},
		{OutputID: "second-output", DeviceID: "second", ArtifactID: "second-artifact"},
	}}})
	if len(runs[0].DeliveryFailures) != 1 || runs[0].DeliveryFailures[0].Code != "delivery_timeout" || runs[0].Delivered != 1 {
		t.Fatalf("run = %#v", runs[0])
	}
}
