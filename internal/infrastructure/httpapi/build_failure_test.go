package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func refusedBuild(t *testing.T, refusal error) *httptest.ResponseRecorder {
	t.Helper()
	backend := testBackend()
	backend.buildErr = refusal
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, "/v1/outputs/"+strings.Repeat("a", 32)+"/build", `{}`))
	return response
}

// A build refused for incomplete coverage answers with the lists it could not
// cover, each by identity and title, so the screen can send the operator to
// them. The code stays the one every client already branches on.
func TestBuildRefusedForCoverageNamesTheUncoveredLists(t *testing.T) {
	refusal := fmt.Errorf("build output: %w", &application.PartialCoverageError{Lists: []application.UncoveredList{
		{ID: "alpha", Title: "Альфа"},
		{ID: "custom-0102030405060708", Title: "Мой список"},
	}})
	response := refusedBuild(t, refusal)
	want := `{"code":"partial_coverage","error":"operation failed","maximum_rules":0,"projected_rules":0,` +
		`"uncovered_lists":[{"id":"alpha","title":"Альфа"},{"id":"custom-0102030405060708","title":"Мой список"}]}` + "\n"
	if response.Code != http.StatusUnprocessableEntity || response.Body.String() != want {
		t.Fatalf("code=%d body=%s want=%s", response.Code, response.Body.String(), want)
	}
}

// Every other refusal keeps the body it always had: the lists belong to the one
// failure that has them.
func TestOtherBuildRefusalsNameNoLists(t *testing.T) {
	for name, refusal := range map[string]error{
		"coverage without lists": application.ErrPartialCoverage,
		"rule limit":             application.ErrRuleLimit,
		"preflight":              application.ErrPreflight,
		"unclassified":           errors.New("disk on fire"),
	} {
		t.Run(name, func(t *testing.T) {
			response := refusedBuild(t, refusal)
			if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "uncovered_lists") {
				t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
