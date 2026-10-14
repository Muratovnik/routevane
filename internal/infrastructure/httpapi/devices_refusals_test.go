package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

// A refusal of a device route carries a stable code, and the field it is about
// when it is about one, so a screen can say it beside that field (ADR 0050).
// Neither the address nor the password is ever repeated in the reply.
func TestDeviceRoutesRefuseWithStableCodes(t *testing.T) {
	base := "/v1/devices/" + strings.Repeat("d", 32)
	address := "http://router.invalid"
	secret := rand.Text()
	register := fmt.Sprintf(`{"target_id":"keenetic","name":"Роутер","address":%q,"account":"admin","interface":"Wireguard0"}`, address)
	update := fmt.Sprintf(`{"name":"Роутер","address":%q,"account":"admin","interface":"Wireguard0"}`, address)
	credential := fmt.Sprintf(`{"credential":%q}`, secret)
	enable := fmt.Sprintf(`{"enabled":true,"credential":%q}`, secret)
	policy := fmt.Errorf("%w: %w", application.ErrConnectionInvalid, application.ErrAddressRejected)

	tests := []struct {
		name, path, body string
		err              error
		status           int
		code, field      string
	}{
		{"registering with an address the policy refuses", "/v1/devices", register, policy, http.StatusUnprocessableEntity, "address_rejected", "address"},
		{"updating to an address the policy refuses", base + "/update", update, policy, http.StatusUnprocessableEntity, "address_rejected", "address"},
		{"an address the service itself cannot read", "/v1/devices", register, fmt.Errorf("%w: invalid device address", application.ErrAddressRejected), http.StatusUnprocessableEntity, "address_rejected", "address"},
		{"a connection part other than the address", "/v1/devices", register, fmt.Errorf("%w: a device account is required", application.ErrConnectionInvalid), http.StatusUnprocessableEntity, "connection_invalid", ""},
		{"a password that cannot be kept", base + "/credential", credential, application.ErrCredentialInvalid, http.StatusUnprocessableEntity, "credential_invalid", "credential"},
		{"saving without a credential store", base + "/credential", credential, application.ErrSecretStoreUnavailable, http.StatusServiceUnavailable, "secret_store_unavailable", ""},
		{"enabling with a password and no credential store", base + "/auto-delivery", enable, application.ErrSecretStoreUnavailable, http.StatusServiceUnavailable, "secret_store_unavailable", ""},
		{"a removal that meets the missing credential store", base + "/credential/remove", `{}`, fmt.Errorf("remove the stored credential: %w", application.ErrSecretStoreUnavailable), http.StatusServiceUnavailable, "secret_store_unavailable", ""},
		{"enabling without a saved password", base + "/auto-delivery", `{"enabled":true}`, application.ErrCredentialRequired, http.StatusConflict, "credential_required", ""},
		{"a failure no code names", "/v1/devices", register, errors.New("the store is unavailable at /private/path"), http.StatusUnprocessableEntity, "operation failed", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := testBackend()
			backend.deviceErr = test.err
			var logs bytes.Buffer
			server := serveDevice(t, backend, &logs)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, mutationRequest(t, test.path, test.body))
			var reply map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
				t.Fatalf("body=%s: %v", response.Body.String(), err)
			}
			want := map[string]any{"error": test.code}
			if test.field != "" {
				want["field"] = test.field
			}
			if response.Code != test.status || len(reply) != len(want) || reply["error"] != want["error"] || reply["field"] != want["field"] {
				t.Fatalf("status=%d body=%s, want %d %v", response.Code, response.Body.String(), test.status, want)
			}
			if strings.Contains(response.Body.String(), address) || strings.Contains(response.Body.String(), secret) {
				t.Fatal("a refusal repeated the address or the password")
			}
		})
	}
}

// A send names the refused address the same way, in the reply of the review and
// of a send that stopped before any attempt, with the status the device routes
// give it (422) and the attempt journal's broad code kept for it. Another
// connection fault stays 400 on the deploy routes.
func TestADeployRefusedForItsAddressNamesTheField(t *testing.T) {
	policy := fmt.Errorf("%w: %w", application.ErrConnectionInvalid, application.ErrAddressRejected)
	for _, test := range []struct {
		name    string
		confirm bool
		err     error
		status  int
		field   string
	}{
		{"review", false, policy, http.StatusUnprocessableEntity, "address"},
		{"send that did not start", true, policy, http.StatusUnprocessableEntity, "address"},
		{"review of another connection fault", false, application.ErrConnectionInvalid, http.StatusBadRequest, ""},
		{"send of another connection fault that did not start", true, application.ErrConnectionInvalid, http.StatusBadRequest, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := unresolvedDeviceBackend{fakeBackend: testBackend(), refusedBeforeAttempt: true}
			backend.deployErr = test.err
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"device":"http://router.invalid","username":"admin","password":"x","interface":"Wireguard0","confirm":%t,"attempt_id":%q}`, test.confirm, strings.Repeat("e", 32))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, mutationRequest(t, "/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", body))
			var reply map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
				t.Fatalf("body=%s: %v", response.Body.String(), err)
			}
			code, field := "address_rejected", test.field
			if test.field == "" {
				code = "connection_invalid"
			}
			if response.Code != test.status || reply["error"] != code || reply["field"] != nilIfEmpty(field) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "router.invalid") {
				t.Fatal("a refusal repeated the address")
			}
		})
	}
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
