package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/httpapi"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

// recordingSavedDeviceDeployer is a credential-taking deployer that refuses an
// empty password, the way a real authenticated router does, and records the
// password each validated connection carried.
type recordingSavedDeviceDeployer struct {
	savedDeviceDeployer
	seen []string
}

func (d *recordingSavedDeviceDeployer) ValidateConnection(connection application.Connection) error {
	if connection.Password == "" {
		return application.ErrConnectionInvalid
	}
	d.seen = append(d.seen, connection.Password)
	return nil
}

// ADR 0050 end to end through the HTTP API, the serve backend and a real
// SQLite registry, with a fake operating-system store: a password is saved
// explicitly, serves a manual delivery with automatic delivery off, is replaced
// for one attempt by a password in the request without being stored, and is
// gone after removal, which also ends automatic delivery that needed it.
func TestASavedPasswordServesManualDeliveryWithoutAutomaticDelivery(t *testing.T) {
	artifacts := newDeliveryGateArtifacts()
	deployer := &recordingSavedDeviceDeployer{savedDeviceDeployer: savedDeviceDeployer{&controlledDeliveryDeployer{entered: make(chan string, 1)}}}
	clock := application.ClockFunc(func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) })
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
		Clock:                    clock, Entropy: bytes.NewReader(bytes.Repeat([]byte{9}, 128)),
	})
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := application.NewDeploymentAttemptService(application.DeploymentAttemptConfig{Deployments: deployments, Journal: store, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	backend := serveBackend{deployments: deployments, deploymentAttempts: attempts, devices: devices, deliveryGate: application.NewDeliveryGate()}
	server, err := httpapi.New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		request := httptest.NewRequest(method, "http://127.0.0.1:8765"+path, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:54321"
		if method == http.MethodPost {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		var decoded map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &decoded)
		return response.Code, decoded
	}
	deviceFlags := func(reply map[string]any) (saved, auto bool) {
		t.Helper()
		device, _ := reply["device"].(map[string]any)
		saved, _ = device["credential_stored"].(bool)
		auto, _ = device["auto_deliver"].(bool)
		return saved, auto
	}

	status, reply := call(http.MethodPost, "/v1/devices", `{"target_id":"`+artifacts.target.ID+`","name":"Saved router","address":"http://192.0.2.1","account":"admin","interface":"Wireguard0"}`)
	device, _ := reply["device"].(map[string]any)
	id, _ := device["id"].(string)
	if status != http.StatusCreated || id == "" {
		t.Fatalf("register status=%d reply=%v", status, reply)
	}
	plan := func(password string) (int, map[string]any) {
		t.Helper()
		body := map[string]any{
			"device_id":           id,
			"expected_connection": map[string]string{"device": "http://192.0.2.1", "username": "admin", "interface": "Wireguard0"},
			"confirm":             false,
		}
		if password != "" {
			body["password"] = password
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return call(http.MethodPost, "/v1/artifacts/"+deliveryGateArtifactID+"/deploy", string(encoded))
	}

	// Nothing is saved: a manual delivery without a password is refused.
	if status, reply := plan(""); status != http.StatusBadRequest || reply["error"] != "connection_invalid" || secrets.reads != 0 {
		t.Fatalf("plan without a password status=%d reply=%v reads=%d", status, reply, secrets.reads)
	}
	// Enabling automatic delivery without a saved password has its own code.
	if status, reply := call(http.MethodPost, "/v1/devices/"+id+"/auto-delivery", `{"enabled":true}`); status != http.StatusConflict || reply["error"] != "credential_required" {
		t.Fatalf("enable without a password status=%d reply=%v", status, reply)
	}

	// Saving is explicit and independent of automatic delivery.
	status, reply = call(http.MethodPost, "/v1/devices/"+id+"/credential", `{"credential":"saved-secret"}`)
	if saved, auto := deviceFlags(reply); status != http.StatusOK || !saved || auto || secrets.value != "saved-secret" {
		t.Fatalf("save status=%d reply=%v", status, reply)
	}
	status, reply = call(http.MethodGet, "/v1/devices", "")
	listed, _ := reply["devices"].([]any)
	first, _ := listed[0].(map[string]any)
	if status != http.StatusOK || first["credential_stored"] != true || first["auto_deliver"] != false {
		t.Fatalf("list status=%d reply=%v", status, reply)
	}

	// The saved password serves a manual delivery; the request's own wins for
	// that attempt and is never stored.
	if status, reply := plan(""); status != http.StatusOK || reply["error"] != nil {
		t.Fatalf("plan with the saved password status=%d reply=%v", status, reply)
	}
	if status, reply := plan("one-use-secret"); status != http.StatusOK || reply["error"] != nil {
		t.Fatalf("plan with a request password status=%d reply=%v", status, reply)
	}
	// Each plan validates the resolved connection twice: once when the saved
	// access is resolved and once when the plan is built.
	if got := strings.Join(deployer.seen, ","); got != "saved-secret,saved-secret,one-use-secret,one-use-secret" || secrets.value != "saved-secret" {
		t.Fatalf("passwords used = %q, stored = %q", got, secrets.value)
	}

	// Turning automatic delivery on and off leaves the saved password.
	if status, reply := call(http.MethodPost, "/v1/devices/"+id+"/auto-delivery", `{"enabled":true}`); status != http.StatusOK {
		t.Fatalf("enable status=%d reply=%v", status, reply)
	}
	status, reply = call(http.MethodPost, "/v1/devices/"+id+"/auto-delivery", `{"enabled":false}`)
	if saved, auto := deviceFlags(reply); status != http.StatusOK || !saved || auto || secrets.value != "saved-secret" {
		t.Fatalf("disable status=%d reply=%v stored=%q", status, reply, secrets.value)
	}

	// Removal deletes it, and a manual delivery without a password is refused
	// again.
	call(http.MethodPost, "/v1/devices/"+id+"/auto-delivery", `{"enabled":true}`)
	status, reply = call(http.MethodPost, "/v1/devices/"+id+"/credential/remove", `{}`)
	if saved, auto := deviceFlags(reply); status != http.StatusOK || saved || auto || secrets.value != "" {
		t.Fatalf("remove status=%d reply=%v stored=%q", status, reply, secrets.value)
	}
	if status, reply := plan(""); status != http.StatusBadRequest || reply["error"] != "connection_invalid" {
		t.Fatalf("plan after removal status=%d reply=%v", status, reply)
	}
}
