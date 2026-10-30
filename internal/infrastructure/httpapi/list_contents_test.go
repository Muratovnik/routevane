package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The list card decides which verdict a switch sends from these bytes: a
// definition row is switched off by exclude, a row without the flag that
// stands on an include verdict is withdrawn by auto.
func TestListContentsNamesTheRowsOfTheDefinition(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists/example/contents", nil)
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, body)
	}
	if !strings.Contains(body, `{"value":"example.com","kind":"domain","origin":"catalog","enabled":true,"definition":true}`) {
		t.Fatalf("definition row lacks its flag: %s", body)
	}
	if !strings.Contains(body, `{"value":"198.51.100.7","kind":"ip","origin":"manual","enabled":true}`) {
		t.Fatalf("an added row claims the definition: %s", body)
	}
}

// The freshness contract the library and the list card parse: instants are
// RFC 3339 UTC or null, a failure carries its stable code, and the refresh
// answer says what that refresh brought next to its cycle counts.
func TestListFreshnessWireContract(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists/example/contents", nil)
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, want := range []string{
		`{"id":"dns","type":"dns","custom":false,"enabled":true,"state":"failed","last_success_at":"2026-10-30T09:15:00Z","last_failure_at":"2026-10-30T10:00:00Z","last_error":"dns_observation_failed"}`,
		`{"id":"vendor","type":"http","custom":false,"enabled":true,"state":"unread","last_success_at":null,"last_failure_at":null}`,
		`"observed":false,"freshness":{"state":"failed","last_read_at":"2026-10-30T09:15:00Z","added":3,"removed":1}}`,
	} {
		if response.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("contents lacks %s: %d %s", want, response.Code, body)
		}
	}

	refresh := mutationRequest(t, "/v1/lists/example/refresh", `{}`)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, refresh)
	if want := `{"refresh":{"list":"example","source_runs":1,"successful_runs":1,"failed_runs":0,"sightings":0,"relations":0,"added":3,"removed":1}}` + "\n"; response.Code != http.StatusOK || response.Body.String() != want {
		t.Fatalf("refresh = %d %s, want %s", response.Code, response.Body.String(), want)
	}
}
