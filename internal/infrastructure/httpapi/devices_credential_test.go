package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

type deviceReply struct {
	Device map[string]any `json:"device"`
	Error  string         `json:"error"`
}

func serveDevice(t *testing.T, backend *fakeBackend, logs *bytes.Buffer) *Server {
	t.Helper()
	var logger *slog.Logger
	if logs != nil {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	server, err := New("http://127.0.0.1:8765", backend, logger)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func postDevice(t *testing.T, server *Server, path, body string) (int, deviceReply, string) {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, path, body))
	var reply deviceReply
	_ = json.Unmarshal(response.Body.Bytes(), &reply)
	return response.Code, reply, response.Body.String()
}

// ADR 0050: a password is saved and removed through its own routes. The reply is
// the device and never the secret, and neither the reply nor the request log
// may carry it.
func TestSavingAndRemovingADeviceCredentialAnswersWithTheDeviceOnly(t *testing.T) {
	backend := testBackend()
	var logs bytes.Buffer
	server := serveDevice(t, backend, &logs)
	secret := rand.Text()
	base := "/v1/devices/" + strings.Repeat("d", 32)

	status, reply, raw := postDevice(t, server, base+"/credential", fmt.Sprintf(`{"credential":%q}`, secret))
	if status != http.StatusOK || reply.Device["credential_stored"] != true || reply.Device["auto_deliver"] != false {
		t.Fatalf("save status=%d body=%s", status, raw)
	}
	if backend.storedCredential != secret {
		t.Fatal("the credential did not reach the backend intact")
	}
	if strings.Contains(raw, secret) || strings.Contains(logs.String(), secret) {
		t.Fatal("a response or log disclosed the credential")
	}

	status, reply, raw = postDevice(t, server, base+"/credential/remove", `{}`)
	if status != http.StatusOK || reply.Device["credential_stored"] != false || backend.removedCredentials != 1 || backend.storedCredential != "" {
		t.Fatalf("remove status=%d body=%s removed=%d", status, raw, backend.removedCredentials)
	}
}

func TestTheCredentialRoutesKeepTheMutationContract(t *testing.T) {
	base := "/v1/devices/" + strings.Repeat("d", 32)
	for _, test := range []struct {
		name, path, body string
		want             int
	}{
		{"save without a field", base + "/credential", `{}`, http.StatusOK},
		{"save with an unknown field", base + "/credential", `{"credential":"x","enabled":true}`, http.StatusBadRequest},
		{"save with trailing JSON", base + "/credential", `{"credential":"x"} {}`, http.StatusBadRequest},
		{"remove with a body", base + "/credential/remove", `{"credential":"x"}`, http.StatusBadRequest},
		{"remove with an empty body", base + "/credential/remove", ``, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := testBackend()
			server := serveDevice(t, backend, nil)
			status, _, raw := postDevice(t, server, test.path, test.body)
			if status != test.want {
				t.Fatalf("status=%d body=%s, want %d", status, raw, test.want)
			}
			if test.want == http.StatusBadRequest && (backend.storedCredential != "" || backend.removedCredentials != 0) {
				t.Fatal("a malformed request reached the backend")
			}
		})
	}

	for _, path := range []string{base + "/credential", base + "/credential/remove"} {
		server := serveDevice(t, testBackend(), nil)
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		request.RemoteAddr = "127.0.0.1:54321"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("GET %s status=%d allow=%q", path, response.Code, response.Header().Get("Allow"))
		}
		// A write without the request marker is refused before the backend.
		unmarked := mutationRequest(t, path, `{}`)
		unmarked.Header.Del("X-Routevane-Request")
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, unmarked)
		if response.Code < 400 {
			t.Fatalf("unmarked POST %s status=%d", path, response.Code)
		}
	}
}

// Enabling automatic delivery for a device that has no saved password answers
// with a stable code a screen can act on, not the generic failure.
func TestEnablingAutomaticDeliveryWithoutASavedCredentialHasAStableCode(t *testing.T) {
	backend := testBackend()
	backend.deviceErr = application.ErrCredentialRequired
	server := serveDevice(t, backend, nil)
	base := "/v1/devices/" + strings.Repeat("d", 32)
	status, reply, raw := postDevice(t, server, base+"/auto-delivery", `{"enabled":true}`)
	if status != http.StatusConflict || reply.Error != "credential_required" {
		t.Fatalf("status=%d body=%s", status, raw)
	}
}

func TestAutomaticDeliveryKeepsItsCompatibleRequestShape(t *testing.T) {
	backend := testBackend()
	server := serveDevice(t, backend, nil)
	secret := rand.Text()
	base := "/v1/devices/" + strings.Repeat("d", 32)

	status, reply, raw := postDevice(t, server, base+"/auto-delivery", fmt.Sprintf(`{"enabled":true,"credential":%q}`, secret))
	if status != http.StatusOK || reply.Device["auto_deliver"] != true || reply.Device["credential_stored"] != true || strings.Contains(raw, secret) {
		t.Fatalf("enable status=%d body=%s", status, raw)
	}
	status, reply, raw = postDevice(t, server, base+"/auto-delivery", `{"enabled":false}`)
	if status != http.StatusOK || reply.Device["auto_deliver"] != false || reply.Device["credential_stored"] != true {
		t.Fatalf("disable status=%d body=%s", status, raw)
	}
	if backend.storedCredential != secret {
		t.Fatal("turning automatic delivery off removed the saved credential")
	}
}

func TestEveryDeviceCarriesTheSavedCredentialFact(t *testing.T) {
	server := serveDevice(t, testBackend(), nil)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/devices", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var body struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Devices) != 1 {
		t.Fatalf("status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
	if value, present := body.Devices[0]["credential_stored"]; !present || value != false {
		t.Fatalf("credential_stored = %v present = %v", value, present)
	}
}

// ADR 0059: a saved password's reply says what the read-only login found, and
// the log names the route, the result and the code only. Clients that read
// only `device` keep working: the check is a sibling of it.
func TestSavingACredentialAnswersWhatItsCheckFound(t *testing.T) {
	base := "/v1/devices/" + strings.Repeat("d", 32)
	for _, check := range []application.CredentialCheck{
		{Result: application.CredentialVerified},
		{Result: application.CredentialInterfaceMissing, Code: "device_interface_missing"},
		{Result: application.CredentialUnverified, Code: "device_unreachable"},
		{Result: application.CredentialCheckNotApplicable},
	} {
		t.Run(check.Result, func(t *testing.T) {
			backend := testBackend()
			backend.credentialCheck = check
			var logs bytes.Buffer
			server := serveDevice(t, backend, &logs)
			secret := rand.Text()
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, mutationRequest(t, base+"/credential", fmt.Sprintf(`{"credential":%q}`, secret)))
			var reply struct {
				Device       map[string]any `json:"device"`
				Verification map[string]any `json:"verification"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || response.Code != http.StatusOK || reply.Device["credential_stored"] != true {
				t.Fatalf("status=%d body=%s err=%v", response.Code, response.Body.String(), err)
			}
			code, coded := reply.Verification["code"]
			if reply.Verification["result"] != check.Result || coded != (check.Code != "") || coded && code != check.Code {
				t.Fatalf("verification = %v, want %#v", reply.Verification, check)
			}
			line := `"route":"devices.credential","result":"` + check.Result + `","code":"` + check.Code + `"`
			if !strings.Contains(logs.String(), line) {
				t.Fatalf("the log lacks %s: %s", line, logs.String())
			}
			if strings.Contains(response.Body.String(), secret) || strings.Contains(logs.String(), secret) {
				t.Fatal("a response or log disclosed the credential")
			}
		})
	}
}

// A password the device refused is a field refusal, on the credential route
// and on the enabling request that carries one.
func TestAPasswordTheDeviceRefusedIsAFieldRefusal(t *testing.T) {
	base := "/v1/devices/" + strings.Repeat("d", 32)
	for _, request := range []struct{ path, body string }{
		{base + "/credential", `{"credential":"wrong"}`},
		{base + "/auto-delivery", `{"enabled":true,"credential":"wrong"}`},
	} {
		backend := testBackend()
		backend.deviceErr = fmt.Errorf("check the credential: %w", application.ErrDeviceAuthRejected)
		var logs bytes.Buffer
		server := serveDevice(t, backend, &logs)
		status, reply, raw := postDevice(t, server, request.path, request.body)
		var body map[string]any
		_ = json.Unmarshal([]byte(raw), &body)
		if status != http.StatusUnprocessableEntity || reply.Error != "device_auth_rejected" || body["field"] != "credential" || reply.Device != nil {
			t.Fatalf("%s: status=%d body=%s", request.path, status, raw)
		}
		if strings.Contains(logs.String(), "wrong") {
			t.Fatalf("the log carries the refused password: %s", logs.String())
		}
	}
}
