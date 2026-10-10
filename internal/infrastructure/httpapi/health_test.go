package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsBuildVersion(t *testing.T) {
	for _, version := range []string{"dev", "v0.3.1", "v0.4.0-rc.1"} {
		t.Run(version, func(t *testing.T) {
			server, err := New("http://127.0.0.1:8765", testBackend(), nil)
			if err != nil {
				t.Fatal(err)
			}
			server.SetVersion(version)
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/health", nil)
			request.RemoteAddr = "127.0.0.1:12345"
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			var health struct {
				Status  string `json:"status"`
				Version string `json:"version"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || health.Status != "ok" || health.Version != version {
				t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
