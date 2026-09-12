package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

type previewBackend struct {
	*fakeBackend
	plan application.DeployPlan
}

func (b previewBackend) DeployPlan(context.Context, application.DeployCommand) (application.DeployPlan, error) {
	return b.plan, b.deployErr
}

func TestDNSPreviewPreservesAnEmptyChangeSetAndDeviceRefusal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		want   string
	}{
		{"unchanged", nil, 200, `"fqdn_changes":[]`},
		{"refused probe", application.ErrDeployFailed, 409, `"error":"deploy_failed"`},
		{"unsupported firmware", application.ErrDeviceIncompatible, 409, `"error":"device_incompatible"`},
		{"invalid connection", application.ErrConnectionInvalid, 400, `"error":"connection_invalid"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := previewBackend{fakeBackend: testBackend(), plan: application.DeployPlan{FQDNChanges: []string{}}}
			backend.deployErr = tc.err
			server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", strings.NewReader(`{"confirm":false}`))
			request.RemoteAddr = "127.0.0.1:1"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestOutputPrefixHTTPRejectsInvalidAndReturnsSavedValue(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prefix string
		status int
	}{{"", 200}, {"home-2", 200}, {"2home", 400}, {"bad prefix", 400}, {strings.Repeat("x", 25), 400}} {
		body, _ := json.Marshal(map[string]string{"fqdn_group_prefix": tc.prefix})
		request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:8765/v1/outputs/"+strings.Repeat("a", 32)+"/fqdn-prefix", strings.NewReader(string(body)))
		request.RemoteAddr = "127.0.0.1:1"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Routevane-Request", "1")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("prefix=%q status=%d body=%s", tc.prefix, response.Code, response.Body.String())
		}
		if tc.status == 200 {
			var got struct {
				Output application.Output `json:"output"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got.Output.FQDNGroupPrefix != tc.prefix {
				t.Fatalf("saved=%s err=%v", response.Body.String(), err)
			}
		} else if !strings.Contains(response.Body.String(), "invalid_fqdn_prefix") {
			t.Fatalf("failure=%s", response.Body.String())
		}
	}
}

func TestDeploymentHTTPReportsUnrecoveredVerificationWithItsAuditTrail(t *testing.T) {
	backend := testBackend()
	backend.deployErr = errors.Join(application.ErrVerifyFailed, application.ErrRollbackFailed)
	backend.deployResult = application.DeployResult{Events: []application.DeployEvent{{Step: application.StepVerify, Outcome: "failed"}, {Step: application.StepRollback, Outcome: "failed"}}}
	server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"attempt_id": strings.Repeat("e", 32), "device": "http://192.168.1.1", "interface": "Wireguard0", "confirm": true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy", strings.NewReader(string(body)))
	request.RemoteAddr = "127.0.0.1:1"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var got struct {
		Error  string                   `json:"error"`
		Result application.DeployResult `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || got.Error != "rollback_failed" || got.Result.RolledBack || len(got.Result.Events) != 2 {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}
