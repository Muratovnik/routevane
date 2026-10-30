package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestFailedRecoveryOutranksTheOriginalVerificationFailure(t *testing.T) {
	joined := errors.Join(application.ErrVerifyFailed, application.ErrRollbackFailed)
	if got := deployFailure(joined); got != "rollback_failed" {
		t.Fatalf("failure=%s", got)
	}
	if got := deployFailure(application.ErrVerifyFailed); got != "verify_failed" {
		t.Fatalf("recovered verification=%s", got)
	}
}

// deviceAnswerText stands for whatever a device put in its refusal. It may
// reach the error chain, and must reach neither the reply nor the log.
const deviceAnswerText = "device-said-something-private"

func deviceRefusal(reason error) error {
	return fmt.Errorf("%w: %w", application.ErrDeployFailed, fmt.Errorf("vendor: %w: %s", reason, deviceAnswerText))
}

// A preview and a send refused by the device answer with what the device did,
// the step it did it at, and the field to fix when there is one; the log line
// says the same and nothing else.
func TestADeviceRefusalNamesItsCauseStepAndFieldWithoutRepeatingTheDevice(t *testing.T) {
	// audited takes "step:outcome" pairs.
	audited := func(events ...string) application.DeployResult {
		result := application.DeployResult{}
		for _, event := range events {
			step, outcome, _ := strings.Cut(event, ":")
			result.Events = append(result.Events, application.DeployEvent{Step: step, Outcome: outcome})
		}
		return result
	}
	for _, test := range []struct {
		name      string
		confirm   bool
		err       error
		result    application.DeployResult
		status    int
		code      string
		step      string
		field     string
		wantField bool
	}{
		{name: "preview with a rejected password", err: &application.StepError{Step: application.StepProbe, Err: deviceRefusal(application.ErrDeviceAuthRejected)},
			status: http.StatusUnprocessableEntity, code: "device_auth_rejected", step: "probe", field: "credential", wantField: true},
		{name: "preview to a missing interface", err: &application.StepError{Step: application.StepProbe, Err: deviceRefusal(errors.Join(application.ErrDeviceAnswerInvalid, application.ErrDeviceInterfaceMissing))},
			status: http.StatusUnprocessableEntity, code: "device_interface_missing", step: "probe", field: "interface", wantField: true},
		{name: "preview of an unreachable device", err: &application.StepError{Step: application.StepProbe, Err: deviceRefusal(application.ErrDeviceUnreachable)},
			status: http.StatusConflict, code: "device_unreachable", step: "probe"},
		{name: "preview of an unusable observation", err: &application.StepError{Step: application.StepPreview, Err: deviceRefusal(application.ErrDeviceAnswerInvalid)},
			status: http.StatusConflict, code: "device_answer_invalid", step: "preview"},
		{name: "unclassified preview", err: &application.StepError{Step: application.StepProbe, Err: fmt.Errorf("%w: %s", application.ErrDeployFailed, deviceAnswerText)},
			status: http.StatusConflict, code: "deploy_failed", step: "probe"},
		{name: "send with a rejected password", confirm: true, err: deviceRefusal(application.ErrDeviceAuthRejected), result: audited("probe:failed"),
			status: http.StatusUnprocessableEntity, code: "device_auth_rejected", step: "probe", field: "credential", wantField: true},
		// The rollback after the rejected command succeeded, so the failed
		// step is the deploy.
		{name: "send whose command was rejected", confirm: true, err: deviceRefusal(application.ErrDeviceCommandRejected),
			result: audited("probe:success", "backup:success", "deploy:failed", "rollback:success"),
			status: http.StatusConflict, code: "device_command_rejected", step: "deploy"},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := testBackend()
			backend.deployErr = test.err
			backend.deployResult = test.result
			var logs bytes.Buffer
			server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			const secret = "SuperSecret123"
			attemptID := strings.Repeat("e", 32)
			body := fmt.Sprintf(`{"attempt_id":%q,"device":"http://192.0.2.1","username":"admin","password":%q,"interface":"Wireguard0","confirm":%t}`, attemptID, secret, test.confirm)
			if !test.confirm {
				body = fmt.Sprintf(`{"device":"http://192.0.2.1","username":"admin","password":%q,"interface":"Wireguard0"}`, secret)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, mutationRequest(t, "/v1/artifacts/"+strings.Repeat("c", 32)+"/deploy", body))
			assertRefusal := func(origin string, status int, raw []byte) {
				t.Helper()
				var decoded map[string]any
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatalf("%s: %s err=%v", origin, raw, err)
				}
				field, hasField := decoded["field"]
				if status != test.status || decoded["error"] != test.code || decoded["step"] != test.step || hasField != test.wantField || test.wantField && field != test.field {
					t.Fatalf("%s: status=%d body=%s", origin, status, raw)
				}
				for _, leaked := range []string{secret, deviceAnswerText, "192.0.2.1"} {
					if strings.Contains(string(raw), leaked) {
						t.Fatalf("%s repeats %q: %s", origin, leaked, raw)
					}
				}
			}
			assertRefusal("reply", response.Code, response.Body.Bytes())
			if test.confirm {
				stored := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/deployment-attempts/"+attemptID, nil)
				stored.RemoteAddr = "127.0.0.1:54321"
				replay := httptest.NewRecorder()
				server.Handler().ServeHTTP(replay, stored)
				// The stored attempt is read, not refused again: its status is 200.
				test.status = http.StatusOK
				assertRefusal("stored attempt", replay.Code, replay.Body.Bytes())
			}

			var refusals []map[string]any
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("log line %q: %v", line, err)
				}
				if record["msg"] == "deployment refused" {
					refusals = append(refusals, record)
				}
			}
			if len(refusals) != 1 {
				t.Fatalf("refusal log lines = %d: %s", len(refusals), logs.String())
			}
			refusal := refusals[0]
			if refusal["route"] != "artifacts.deploy" || refusal["code"] != test.code || refusal["step"] != test.step || refusal["confirmed"] != test.confirm {
				t.Fatalf("refusal log = %v", refusal)
			}
			for _, leaked := range []string{secret, deviceAnswerText, "192.0.2.1", "vendor"} {
				if strings.Contains(logs.String(), leaked) {
					t.Fatalf("the log carries %q: %s", leaked, logs.String())
				}
			}
		})
	}
}
