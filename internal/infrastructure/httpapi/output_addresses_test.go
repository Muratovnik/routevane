package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

// The setting is a boolean the client must state: an absent or misspelled
// field is refused, not read as "off", and a format that cannot hold it is
// refused with its own code.
func TestOutputKeepAddressesHTTPStatesTheSavedValueAndRefusesWhatItCannotHold(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	eligible, other := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, tc := range []struct {
		name   string
		id     string
		body   string
		status int
		keep   bool
	}{
		{"switched on", eligible, `{"keep_addresses":true}`, http.StatusOK, true},
		{"switched off", eligible, `{"keep_addresses":false}`, http.StatusOK, false},
		{"off is always allowed", other, `{"keep_addresses":false}`, http.StatusOK, false},
		{"a format without names and addresses", other, `{"keep_addresses":true}`, http.StatusBadRequest, false},
		{"absent value", eligible, `{}`, http.StatusBadRequest, false},
		{"misspelled field", eligible, `{"keep_address":true}`, http.StatusBadRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:8765/v1/outputs/"+tc.id+"/keep-addresses", strings.NewReader(tc.body))
			request.RemoteAddr = "127.0.0.1:1"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status != http.StatusOK {
				return
			}
			var got struct {
				Output application.Output `json:"output"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got.Output.KeepAddresses != tc.keep {
				t.Fatalf("saved=%s err=%v", response.Body.String(), err)
			}
		})
	}
}
