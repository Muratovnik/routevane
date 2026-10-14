package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func savedDeploymentBody(confirm bool) map[string]any {
	return map[string]any{
		"device_id": strings.Repeat("d", 32),
		"expected_connection": map[string]any{
			"device": "http://192.0.2.1", "username": "admin", "interface": "Wireguard0",
		},
		"password":   rand.Text(),
		"confirm":    confirm,
		"attempt_id": strings.Repeat("e", 32),
	}
}

func TestDeploymentAcceptsARegisteredDeviceWithoutReturningItsCredential(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		name := "preview"
		if confirm {
			name = "apply"
		}
		t.Run(name, func(t *testing.T) {
			backend := testBackend()
			var logs bytes.Buffer
			server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			requestBody := savedDeploymentBody(confirm)
			credential := requestBody["password"].(string)
			body, err := json.Marshal(requestBody)
			if err != nil {
				t.Fatal(err)
			}
			request := mutationRequest(t, "/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", string(body))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK || len(backend.deployCommands) != 1 {
				t.Fatalf("status=%d body=%s commands=%d", response.Code, response.Body.String(), len(backend.deployCommands))
			}
			command := backend.deployCommands[0]
			wantSnapshot := application.DeviceConnectionSnapshot{Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0"}
			if command.DeviceID != strings.Repeat("d", 32) || command.ExpectedConnection == nil || *command.ExpectedConnection != wantSnapshot || command.Connection.Password != credential || command.Confirm != confirm {
				t.Fatal("selected access did not reach the backend intact")
			}
			if command.Connection.URL != "" || command.Connection.Username != "" || command.Connection.Interface != "" {
				t.Fatal("a registered selection became mixed raw connection metadata")
			}
			if strings.Contains(response.Body.String(), credential) || strings.Contains(logs.String(), credential) {
				t.Fatal("a response or request log disclosed the credential")
			}
		})
	}
}

func TestRegisteredDeploymentRejectsMixedOrMissingSnapshotsBeforeTheBackend(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"raw address", func(body map[string]any) { body["device"] = "http://192.0.2.2" }},
		{"raw account", func(body map[string]any) { body["username"] = "admin" }},
		{"raw interface", func(body map[string]any) { body["interface"] = "Wireguard0" }},
		{"missing snapshot", func(body map[string]any) { delete(body, "expected_connection") }},
		{"snapshot without a device", func(body map[string]any) { delete(body, "device_id") }},
		{"password inside snapshot", func(body map[string]any) {
			body["expected_connection"] = map[string]any{"device": "http://192.0.2.1", "username": "admin", "interface": "Wireguard0", "password": rand.Text()}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := testBackend()
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			body := savedDeploymentBody(true)
			test.mutate(body)
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			request := mutationRequest(t, "/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", string(encoded))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || len(backend.deployCommands) != 0 {
				t.Fatalf("status=%d body=%s commands=%d", response.Code, response.Body.String(), len(backend.deployCommands))
			}
		})
	}
}

type unresolvedDeviceBackend struct {
	*fakeBackend
	refusedBeforeAttempt bool
}

func (b unresolvedDeviceBackend) DeployAttempt(context.Context, string, application.DeployCommand) (application.DeploymentAttempt, error) {
	err := b.deployErr
	if b.refusedBeforeAttempt {
		err = errors.Join(application.ErrDeploymentNotStarted, err)
	}
	return application.DeploymentAttempt{}, err
}

func TestRegisteredDeploymentResolutionFailuresKeepTheirStableCodes(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		refused bool
		status  int
		code    string
	}{
		{"stale or unavailable credential", application.ErrConnectionInvalid, true, http.StatusBadRequest, "connection_invalid"},
		{"incompatible target", application.ErrDeviceIncompatible, true, http.StatusConflict, "device_incompatible"},
		{"deleted registration", application.ErrNotFound, true, http.StatusNotFound, "not found"},
		{"journal read failure", application.ErrNotFound, false, http.StatusNotFound, "not found"},
		{"journal acknowledgement failure", errors.New("journal acknowledgement unavailable"), false, http.StatusUnprocessableEntity, "operation failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := unresolvedDeviceBackend{fakeBackend: testBackend(), refusedBeforeAttempt: test.refused}
			backend.deployErr = test.err
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(savedDeploymentBody(true))
			if err != nil {
				t.Fatal(err)
			}
			request := mutationRequest(t, "/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", string(body))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			var code string
			if err := json.Unmarshal(payload["error"], &code); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.status || code != test.code || payload["attempt_id"] != nil {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			marker, present := payload["attempt_started"]
			if present != test.refused || present && string(marker) != "false" {
				t.Fatalf("pre-attempt guarantee=%s, present=%v; want proven=%v", marker, present, test.refused)
			}
		})
	}
}
