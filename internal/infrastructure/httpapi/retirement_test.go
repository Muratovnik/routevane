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

func retirementPost(t *testing.T, server *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

// Each retirement verb answers 200 with what it did, reaches the backend with
// the identity in its path, and states the moment as the API states every
// other moment.
func TestRetirementRoutesAnswerWithWhatTheyDid(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)

	deleted := retirementPost(t, server, "/v1/profiles/"+id+"/delete", `{}`)
	if deleted.Code != http.StatusOK || strings.TrimSpace(deleted.Body.String()) != `{"deleted_at":"2026-10-13T12:00:00Z","id":"`+id+`"}` {
		t.Fatalf("delete code=%d body=%s", deleted.Code, deleted.Body.String())
	}
	removed := retirementPost(t, server, "/v1/outputs/"+id+"/remove", `{}`)
	if removed.Code != http.StatusOK || strings.TrimSpace(removed.Body.String()) != `{"id":"`+id+`","removed_at":"2026-10-13T12:00:00Z"}` {
		t.Fatalf("remove code=%d body=%s", removed.Code, removed.Body.String())
	}
	reissued := retirementPost(t, server, "/v1/outputs/"+id+"/subscription/reissue", `{}`)
	var link struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	if reissued.Code != http.StatusOK || json.Unmarshal(reissued.Body.Bytes(), &link) != nil || link.SubscriptionURL != "http://127.0.0.1:8765/v1/subscriptions/"+backend.token {
		t.Fatalf("reissue code=%d body=%s", reissued.Code, reissued.Body.String())
	}
	want := []string{"delete " + id, "remove " + id, "reissue " + id}
	if fmt.Sprint(backend.retired) != fmt.Sprint(want) {
		t.Fatalf("backend saw %v, want %v", backend.retired, want)
	}

	// The verbs are mutations: an unguarded request and a read never reach
	// the backend, and a body that is not the empty object is refused.
	for _, path := range []string{"/v1/profiles/" + id + "/delete", "/v1/outputs/" + id + "/remove", "/v1/outputs/" + id + "/subscription/reissue"} {
		read := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		read.RemoteAddr = "127.0.0.1:54321"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, read)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s code=%d", path, response.Code)
		}
		unguarded := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, strings.NewReader(`{}`))
		unguarded.RemoteAddr = "127.0.0.1:54321"
		unguarded.Header.Set("Content-Type", "application/json")
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, unguarded)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unguarded %s code=%d", path, response.Code)
		}
		if body := retirementPost(t, server, path, `{"force":true}`); body.Code != http.StatusBadRequest {
			t.Fatalf("%s with a body code=%d", path, body.Code)
		}
	}
	if len(backend.retired) != len(want) {
		t.Fatalf("a refused request reached the backend: %v", backend.retired)
	}
}

// Every refusal a retirement can meet has its own status and stable code, so a
// client branches on the code rather than on the wording.
func TestRetirementRefusalsCarryStableCodes(t *testing.T) {
	id := strings.Repeat("a", 32)
	cases := []struct {
		name   string
		path   string
		err    error
		status int
		body   string
	}{
		{"delete on the shelf", "/v1/profiles/" + id + "/delete", application.ErrProfileNotArchived, http.StatusConflict, `{"code":"profile_not_archived","error":"profile is not archived"}`},
		{"delete unknown", "/v1/profiles/" + id + "/delete", application.ErrNotFound, http.StatusNotFound, `{"error":"not found"}`},
		{"remove from archived", "/v1/outputs/" + id + "/remove", application.ErrProfileArchived, http.StatusConflict, `{"code":"profile_archived","error":"profile is archived"}`},
		{"remove unknown", "/v1/outputs/" + id + "/remove", application.ErrNotFound, http.StatusNotFound, `{"error":"not found"}`},
		{"reissue archived", "/v1/outputs/" + id + "/subscription/reissue", application.ErrProfileArchived, http.StatusConflict, `{"code":"profile_archived","error":"profile is archived"}`},
		{"reissue never published", "/v1/outputs/" + id + "/subscription/reissue", application.ErrSubscriptionMissing, http.StatusConflict, `{"code":"subscription_not_issued","error":"subscription not issued"}`},
		{"reissue removed", "/v1/outputs/" + id + "/subscription/reissue", application.ErrNotFound, http.StatusNotFound, `{"error":"not found"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backend := testBackend()
			backend.retireErr = fmt.Errorf("wrapped: %w", testCase.err)
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			response := retirementPost(t, server, testCase.path, `{}`)
			if response.Code != testCase.status || strings.TrimSpace(response.Body.String()) != testCase.body {
				t.Fatalf("code=%d body=%s, want %d %s", response.Code, response.Body.String(), testCase.status, testCase.body)
			}
		})
	}
}

// A revoked subscription is gone, not unknown, and neither answer carries a
// file. The token never reaches the local log on either path, and a reissued
// one never reaches it on the way out.
func TestRevokedSubscriptionIsGoneAndNoTokenIsLogged(t *testing.T) {
	backend := testBackend()
	var logs bytes.Buffer
	server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	get := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/subscriptions/"+backend.token, nil)
		request.RemoteAddr = "127.0.0.1:1"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}
	backend.subscriptionErr = application.ErrSubscriptionRevoked
	gone := get()
	if gone.Code != http.StatusGone || strings.TrimSpace(gone.Body.String()) != `{"code":"subscription_revoked","error":"subscription revoked"}` || gone.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoked code=%d cache=%q body=%s", gone.Code, gone.Header().Get("Cache-Control"), gone.Body.String())
	}
	backend.subscriptionErr = application.ErrNotFound
	if unknown := get(); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown code=%d", unknown.Code)
	}
	backend.subscriptionErr = nil
	if reissued := retirementPost(t, server, "/v1/outputs/"+strings.Repeat("a", 32)+"/subscription/reissue", `{}`); reissued.Code != http.StatusOK {
		t.Fatalf("reissue code=%d", reissued.Code)
	}
	secret := strings.Split(backend.token, ".")[2]
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("a subscription secret reached the log: %s", logs.String())
	}
}
