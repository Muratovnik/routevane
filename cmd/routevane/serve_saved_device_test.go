package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

func TestManualDeliveryUsesARegisteredConnectionAndRefusesAChangedDestination(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	configPath := filepath.Join(t.TempDir(), "config.json")
	ruleSetPath := filepath.Join(filepath.Dir(configPath), "routevane.json")
	writeLocalSingBoxConfig(t, configPath, ruleSetPath)
	otherConfig := filepath.Join(t.TempDir(), "config.json")
	otherRuleSet := filepath.Join(filepath.Dir(otherConfig), "routevane.json")
	writeLocalSingBoxConfig(t, otherConfig, otherRuleSet)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()
	profileID := createScheduledProfile(t, origin, []string{"youtube"})
	outputID := addScheduledOutput(t, origin, profileID, "singbox")
	build := refreshAndBuild(t, origin, profileID, outputID)
	var created struct {
		Device application.Device `json:"device"`
	}
	registered := postJSON(t, origin+"/v1/devices", `{"target_id":"singbox","name":"Local sing-box","address":`+mustQuote(t, localFileURL(configPath))+`,"account":"","interface":""}`)
	if err := json.Unmarshal(registered, &created); err != nil || created.Device.ID == "" {
		t.Fatalf("device=%s err=%v", registered, err)
	}
	selected := map[string]any{
		"device_id": created.Device.ID,
		"expected_connection": application.DeviceConnectionSnapshot{
			Address: created.Device.Address, Account: created.Device.Account, Interface: created.Device.Interface,
		},
		"confirm": false,
	}
	encode := func() string {
		t.Helper()
		body, err := json.Marshal(selected)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	endpoint := origin + "/v1/artifacts/" + build.Artifact.ID + "/deploy"
	postJSON(t, endpoint, encode())
	if _, err := os.Stat(ruleSetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote the destination: %v", err)
	}
	selected["confirm"] = true
	selected["attempt_id"] = strings.Repeat("e", 32)
	applied := postJSON(t, endpoint, encode())
	var result struct {
		Result application.DeployResult `json:"result"`
	}
	if err := json.Unmarshal(applied, &result); err != nil || !result.Result.Applied {
		t.Fatalf("apply=%s err=%v", applied, err)
	}
	written, err := os.ReadFile(ruleSetPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact := httpGet(t, origin+"/v1/artifacts/"+build.Artifact.ID, nil)
	if artifact.status != http.StatusOK || !bytes.Equal(written, artifact.body) {
		t.Fatal("registered delivery did not install the immutable artifact")
	}
	output := httpGet(t, origin+"/v1/outputs/"+outputID, nil)
	var binding struct {
		DeviceID string `json:"device_id"`
	}
	if output.status != http.StatusOK || json.Unmarshal(output.body, &binding) != nil || binding.DeviceID != "" || created.Device.AutoDeliver {
		t.Fatal("manual delivery changed the binding or enabled automatic delivery")
	}
	registry := httpGet(t, origin+"/v1/devices", nil)
	var connections struct {
		Devices []application.DeviceCard `json:"devices"`
	}
	if registry.status != http.StatusOK || json.Unmarshal(registry.body, &connections) != nil || len(connections.Devices) != 1 || connections.Devices[0].AutoDeliver {
		t.Fatal("manual delivery enabled automatic delivery on the saved connection")
	}
	operatorEdit := []byte("operator's later edit\n")
	if err := os.WriteFile(ruleSetPath, operatorEdit, 0o600); err != nil {
		t.Fatal(err)
	}
	postJSON(t, endpoint, encode())
	if retained, err := os.ReadFile(ruleSetPath); err != nil || !bytes.Equal(retained, operatorEdit) {
		t.Fatalf("replayed attempt overwrote an operator edit: %v", err)
	}
	postJSON(t, origin+"/v1/devices/"+created.Device.ID+"/update", `{"name":"Moved sing-box","address":`+mustQuote(t, localFileURL(otherConfig))+`,"account":"","interface":""}`)
	selected["attempt_id"] = strings.Repeat("f", 32)
	refused := postGuardedBody(t, endpoint, encode())
	var refusal struct {
		Error          string `json:"error"`
		AttemptStarted *bool  `json:"attempt_started"`
	}
	if err := json.Unmarshal(refused.body, &refusal); err != nil || refused.status != http.StatusBadRequest || refusal.Error != "connection_invalid" || refusal.AttemptStarted == nil || *refusal.AttemptStarted {
		t.Fatalf("stale destination status=%d body=%s err=%v", refused.status, refused.body, err)
	}
	if _, err := os.Stat(otherRuleSet); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale snapshot reached the new destination: %v", err)
	}
	if retained, err := os.ReadFile(ruleSetPath); err != nil || !bytes.Equal(retained, operatorEdit) {
		t.Fatalf("stale snapshot changed the old destination: %v", err)
	}
	lookup := httpGet(t, origin+"/v1/deployment-attempts/"+strings.Repeat("f", 32), nil)
	if lookup.status != http.StatusNotFound {
		t.Fatalf("stale selection created an attempt: status=%d body=%s", lookup.status, lookup.body)
	}
}

type savedDeviceDeployer struct{ *controlledDeliveryDeployer }

func (savedDeviceDeployer) Requirements() application.ConnectionRequirements {
	return application.ConnectionRequirements{NeedsCredential: true, NeedsInterface: true}
}

type savedDeviceSecrets struct {
	value string
	reads int
}

func (*savedDeviceSecrets) Available() bool { return true }
func (s *savedDeviceSecrets) PutSecret(_ context.Context, _, _, value string) error {
	s.value = value
	return nil
}
func (s *savedDeviceSecrets) Secret(context.Context, string) (string, error) {
	s.reads++
	if s.value == "" {
		return "", application.ErrNotFound
	}
	return s.value, nil
}
func (s *savedDeviceSecrets) DeleteSecret(context.Context, string) error {
	s.value = ""
	return nil
}

// ADR 0050: the saved credential, not automatic delivery, is what a registered
// manual delivery reads, and removing it while a request waits for the gate
// must be seen by that request.
func TestRegisteredDeploymentReadsTheSavedCredentialOnlyAfterAcquiringTheDeliveryGate(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		name := "preview"
		if confirm {
			name = "apply"
		}
		t.Run(name, func(t *testing.T) {
			artifacts := newDeliveryGateArtifacts()
			deployer := savedDeviceDeployer{&controlledDeliveryDeployer{entered: make(chan string, 1)}}
			clock := application.ClockFunc(time.Now)
			deployments, err := application.NewDeploymentService(application.DeploymentConfig{
				Artifacts: artifacts, Deployers: application.DeployerRegistry{deployer.ID(): deployer}, Backups: deliveryGateBackupStore{}, Clock: clock,
			})
			if err != nil {
				t.Fatal(err)
			}
			root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
			if err != nil {
				t.Fatal(err)
			}
			store, err := sqlite.Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			secrets := &savedDeviceSecrets{}
			devices, err := application.NewDeviceService(application.DeviceConfig{
				Store: store, Secrets: secrets, Targets: artifacts.Targets,
				Deployable: func(string) bool { return true }, NeedsCredential: func(string) bool { return true }, NeedsInterface: func(string) bool { return true },
				ValidateStoredConnection: deployments.ValidateStoredConnection,
				RetireManagedRoutes:      func(context.Context, string, application.Connection) error { return nil },
				Clock:                    clock, Entropy: bytes.NewReader(bytes.Repeat([]byte{7}, 128)),
			})
			if err != nil {
				t.Fatal(err)
			}
			device, err := devices.RegisterDevice(context.Background(), artifacts.target.ID, "Saved router", "http://10.255.0.1", "admin", "Wireguard0")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := devices.EnableAutoDelivery(context.Background(), device.ID, "saved-secret"); err != nil {
				t.Fatal(err)
			}
			attempts, err := application.NewDeploymentAttemptService(application.DeploymentAttemptConfig{Deployments: deployments, Journal: store, Clock: clock})
			if err != nil {
				t.Fatal(err)
			}
			gate := application.NewDeliveryGate()
			backend := serveBackend{deployments: deployments, deploymentAttempts: attempts, devices: devices, deliveryGate: gate}
			release, err := gate.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			waiting := &observedDoneContext{Context: ctx, observed: make(chan struct{})}
			command := application.DeployCommand{
				ArtifactID: deliveryGateArtifactID, DeviceID: device.ID, Confirm: confirm,
				ExpectedConnection: &application.DeviceConnectionSnapshot{Address: device.Address, Account: device.Account, Interface: device.Interface},
			}
			done := make(chan error, 1)
			go func() {
				if confirm {
					_, err := backend.DeployAttempt(waiting, strings.Repeat("e", 32), command)
					done <- err
					return
				}
				_, err := backend.DeployPlan(waiting, command)
				done <- err
			}()
			select {
			case <-waiting.observed:
			case <-ctx.Done():
				t.Fatal("registered selection did not wait for the delivery gate")
			}
			if _, err := devices.RemoveCredential(context.Background(), device.ID); err != nil {
				t.Fatal(err)
			}
			release()
			if err := <-done; !errors.Is(err, application.ErrConnectionInvalid) || confirm && !errors.Is(err, application.ErrDeploymentNotStarted) {
				t.Fatalf("queued delivery did not refuse the removed credential: %v", err)
			}
			if secrets.reads != 0 || len(deployer.entered) != 0 {
				t.Fatalf("a removed credential reached the store or device: reads=%d calls=%d", secrets.reads, len(deployer.entered))
			}
			if _, err := attempts.Attempt(context.Background(), strings.Repeat("e", 32)); !errors.Is(err, application.ErrNotFound) {
				t.Fatalf("refused selection created a durable attempt: %v", err)
			}
		})
	}
}
