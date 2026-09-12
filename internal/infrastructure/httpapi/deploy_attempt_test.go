package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestConfirmedDeploymentRequiresAClientAttemptID(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := mutationRequest(t, "/v1/artifacts/"+strings.Repeat("a", 32)+"/deploy",
		`{"device":"http://192.168.1.1","confirm":true}`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error":"attempt_id_required"`) {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	if len(backend.deployCommands) != 0 {
		t.Fatalf("request without identity reached deployment: %#v", backend.deployCommands)
	}
}

func TestDeploymentAttemptLookupDistinguishesUnknownAndFinalOutcomes(t *testing.T) {
	id := strings.Repeat("e", 32)
	artifactID := strings.Repeat("a", 32)
	backend := testBackend()
	backend.deployAttempts = map[string]application.DeploymentAttempt{
		id: {ID: id, ArtifactID: artifactID, Status: application.DeploymentAttemptPending},
	}
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func() *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/deployment-attempts/"+id, nil)
		request.RemoteAddr = "127.0.0.1:54321"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}
	unknown := lookup()
	if unknown.Code != http.StatusOK || !strings.Contains(unknown.Body.String(), `"status":"outcome_unknown"`) {
		t.Fatalf("unknown code=%d body=%s", unknown.Code, unknown.Body.String())
	}

	backend.deployAttempts[id] = application.DeploymentAttempt{
		ID: id, ArtifactID: artifactID, Status: application.DeploymentAttemptSucceeded,
		Result: application.DeployResult{ArtifactID: artifactID, Applied: true},
	}
	final := lookup()
	if final.Code != http.StatusOK || !strings.Contains(final.Body.String(), `"status":"succeeded"`) || !strings.Contains(final.Body.String(), `"applied":true`) {
		t.Fatalf("final code=%d body=%s", final.Code, final.Body.String())
	}
}
