package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func cleanupObjects() []application.DeviceObjects {
	return []application.DeviceObjects{{
		OutputID: strings.Repeat("a", 32), TargetID: "keenetic-dns", Endpoint: "http://192.0.2.1", Interface: "Wireguard0",
		Connection: &application.DeviceObjectsConnection{ID: strings.Repeat("d", 32), Name: "Home router", CredentialStored: true},
		Cleanable:  true, FQDNGroups: []string{"routevane-claude-a1b2c3-s1"},
	}}
}

// The ledger reads answer the objects by surface and whether a cleanup can
// reach every one; an empty answer is an empty list that can be cleaned.
func TestDeviceObjectReadsAnswerTheirContract(t *testing.T) {
	id := strings.Repeat("a", 32)
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		request.RemoteAddr = "127.0.0.1:1"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}
	empty := get("/v1/outputs/" + id + "/device-objects")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"cleanable":true,"device_objects":[]}` {
		t.Fatalf("empty code=%d body=%s", empty.Code, empty.Body.String())
	}
	backend.deviceObjects = cleanupObjects()
	backend.deviceObjects = append(backend.deviceObjects, application.DeviceObjects{
		OutputID: id, TargetID: "keenetic", Endpoint: "http://192.0.2.1", Interface: "Wireguard0",
		BlockedBy: application.CleanupBlockedConnectionMissing, Routes: &application.DeviceRouteCounts{Claimed: 3, Removable: 1, Shared: 1, Preexisting: 1},
	})
	listed := get("/v1/profiles/" + id + "/device-objects")
	want := `{"cleanable":false,"device_objects":[` +
		`{"output_id":"` + id + `","target_id":"keenetic-dns","endpoint":"http://192.0.2.1","interface":"Wireguard0","connection":{"id":"` + strings.Repeat("d", 32) + `","name":"Home router","credential_stored":true},"cleanable":true,"fqdn_groups":["routevane-claude-a1b2c3-s1"]},` +
		`{"output_id":"` + id + `","target_id":"keenetic","endpoint":"http://192.0.2.1","interface":"Wireguard0","cleanable":false,"blocked_by":"connection_missing","routes":{"claimed":3,"removable":1,"shared":1,"preexisting":1}}]}`
	if listed.Code != http.StatusOK || strings.TrimSpace(listed.Body.String()) != want {
		t.Fatalf("listed code=%d body=%s\nwant %s", listed.Code, listed.Body.String(), want)
	}
	backend.retireErr = application.ErrNotFound
	if gone := get("/v1/outputs/" + id + "/device-objects"); gone.Code != http.StatusNotFound {
		t.Fatalf("unknown code=%d", gone.Code)
	}
	if fmt.Sprint(backend.retired) != fmt.Sprint([]string{"output-objects " + id, "profile-objects " + id, "output-objects " + id}) {
		t.Fatalf("backend saw %v", backend.retired)
	}
	if read := retirementPost(t, server, "/v1/outputs/"+id+"/device-objects", `{}`); read.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to a read code=%d", read.Code)
	}
}

// The opt-in takes the device form of each retirement verb and answers with
// the removals it made; without it, or with false, the verbs are unchanged.
func TestRetirementWithTheOptInAnswersItsRemovals(t *testing.T) {
	id := strings.Repeat("a", 32)
	backend := testBackend()
	backend.cleanups = []application.DeviceCleanupAttempt{{
		ID: strings.Repeat("c", 32), OutputID: id, DeviceID: strings.Repeat("d", 32), Endpoint: "http://192.0.2.1",
		Status: application.DeploymentAttemptSucceeded, StartedAt: retiredAt, CompletedAt: retiredAt,
		Result: application.DeployResult{Applied: true, Events: []application.DeployEvent{{Step: application.StepRemove, Outcome: "success"}}},
	}}
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	removed := retirementPost(t, server, "/v1/outputs/"+id+"/remove", `{"remove_from_devices":true}`)
	var reply struct {
		ID        string                             `json:"id"`
		RemovedAt string                             `json:"removed_at"`
		Cleanups  []application.DeviceCleanupAttempt `json:"device_cleanup"`
	}
	if removed.Code != http.StatusOK || json.Unmarshal(removed.Body.Bytes(), &reply) != nil || reply.ID != id || reply.RemovedAt != "2026-10-13T12:00:00Z" ||
		len(reply.Cleanups) != 1 || reply.Cleanups[0].ID != strings.Repeat("c", 32) || !strings.Contains(removed.Body.String(), `"attempt_id":"`+strings.Repeat("c", 32)+`"`) {
		t.Fatalf("remove code=%d body=%s", removed.Code, removed.Body.String())
	}
	backend.cleanups = nil
	deleted := retirementPost(t, server, "/v1/profiles/"+id+"/delete", `{"remove_from_devices":true}`)
	if deleted.Code != http.StatusOK || strings.TrimSpace(deleted.Body.String()) != `{"deleted_at":"2026-10-13T12:00:00Z","device_cleanup":[],"id":"`+id+`"}` {
		t.Fatalf("delete code=%d body=%s", deleted.Code, deleted.Body.String())
	}
	plain := retirementPost(t, server, "/v1/outputs/"+id+"/remove", `{"remove_from_devices":false}`)
	if plain.Code != http.StatusOK || strings.TrimSpace(plain.Body.String()) != `{"id":"`+id+`","removed_at":"2026-10-13T12:00:00Z"}` {
		t.Fatalf("plain code=%d body=%s", plain.Code, plain.Body.String())
	}
	if null := retirementPost(t, server, "/v1/outputs/"+id+"/remove", `null`); null.Code != http.StatusBadRequest {
		t.Fatalf("null body code=%d", null.Code)
	}
	want := []string{"remove-cleaning " + id, "delete-cleaning " + id, "remove " + id}
	if fmt.Sprint(backend.retired) != fmt.Sprint(want) {
		t.Fatalf("backend saw %v, want %v", backend.retired, want)
	}
}

// A refused cleanup answers its stable code with the step and the surface it
// stopped at, the attempts it made and an explicit statement that nothing was
// retired. The log keeps the route, the code and the step only.
func TestARefusedCleanupAnswersItsCodeAndRetiresNothing(t *testing.T) {
	id := strings.Repeat("a", 32)
	failed := application.DeviceCleanupAttempt{
		ID: strings.Repeat("c", 32), OutputID: id, DeviceID: strings.Repeat("d", 32), Endpoint: "http://192.0.2.1",
		Status: application.DeploymentAttemptFailed, Error: "verify_failed", StartedAt: retiredAt, CompletedAt: retiredAt,
		Result: application.DeployResult{RolledBack: true},
	}
	object := cleanupObjects()[0]
	blocked := object
	blocked.Cleanable, blocked.BlockedBy = false, application.CleanupBlockedLoginMissing
	cases := []struct {
		name    string
		path    string
		err     error
		status  int
		want    map[string]any
		cleaned int
	}{
		{"blocked", "/v1/profiles/" + id + "/delete",
			&application.DeviceCleanupRefusal{Blocked: []application.DeviceObjects{blocked}, Err: application.ErrDeviceCleanupUnavailable},
			http.StatusConflict, map[string]any{"error": "device_cleanup_unavailable", "code": "device_cleanup_unavailable", "retired": false}, 0},
		{"refused password", "/v1/outputs/" + id + "/remove",
			&application.DeviceCleanupRefusal{Objects: &object, Step: application.StepProbe, Err: fmt.Errorf("%w: %w", application.ErrDeployFailed, application.ErrDeviceAuthRejected)},
			http.StatusUnprocessableEntity, map[string]any{"error": "device_auth_rejected", "code": "device_auth_rejected", "retired": false, "step": "probe", "field": "credential"}, 0},
		{"unreachable", "/v1/outputs/" + id + "/remove",
			&application.DeviceCleanupRefusal{Objects: &object, Step: application.StepProbe, Err: fmt.Errorf("%w: %w", application.ErrDeployFailed, application.ErrDeviceUnreachable)},
			http.StatusConflict, map[string]any{"error": "device_unreachable", "code": "device_unreachable", "retired": false, "step": "probe"}, 0},
		{"failed verification", "/v1/profiles/" + id + "/delete",
			&application.DeviceCleanupRefusal{Objects: &object, Step: application.StepVerify, Cleanups: []application.DeviceCleanupAttempt{failed}, Err: fmt.Errorf("%w: x", application.ErrVerifyFailed)},
			http.StatusConflict, map[string]any{"error": "verify_failed", "code": "verify_failed", "retired": false, "step": "verify"}, 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backend := testBackend()
			backend.cleanupErr = testCase.err
			var logs bytes.Buffer
			server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			response := retirementPost(t, server, testCase.path, `{"remove_from_devices":true}`)
			var body map[string]any
			if response.Code != testCase.status || json.Unmarshal(response.Body.Bytes(), &body) != nil {
				t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
			}
			for key, value := range testCase.want {
				if body[key] != value {
					t.Fatalf("%s = %v, want %v in %s", key, body[key], value, response.Body.String())
				}
			}
			if cleanups, _ := body["device_cleanup"].([]any); len(cleanups) != testCase.cleaned {
				t.Fatalf("device_cleanup = %v", body["device_cleanup"])
			}
			_, hasBlocked := body["device_objects"]
			_, hasObject := body["device_object"]
			if hasBlocked != (testCase.name == "blocked") || hasObject == (testCase.name == "blocked") {
				t.Fatalf("surfaces in %s", response.Body.String())
			}
			if strings.Contains(logs.String(), "192.0.2.1") || !strings.Contains(logs.String(), `"code":"`+testCase.want["code"].(string)+`"`) {
				t.Fatalf("log = %s", logs.String())
			}
			if len(backend.retired) != 0 {
				t.Fatalf("backend retired %v", backend.retired)
			}
		})
	}
	// A refusal of the retirement itself keeps the retirement's own answer.
	backend := testBackend()
	backend.cleanupErr = application.ErrProfileArchived
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	if archived := retirementPost(t, server, "/v1/outputs/"+id+"/remove", `{"remove_from_devices":true}`); archived.Code != http.StatusConflict ||
		strings.TrimSpace(archived.Body.String()) != `{"code":"profile_archived","error":"profile is archived"}` {
		t.Fatalf("archived code=%d body=%s", archived.Code, archived.Body.String())
	}
}
