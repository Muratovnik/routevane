package keenetic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestCommandChecksDeviceStatusWithoutOutput(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want, kind error
	}{
		{"saved", `{"status":[{"status":"message","code":"saved"}]}`, nil, nil},
		{"save error", `{"status":[{"status":"error","code":"save-refused"}]}`, ErrDeviceRefused, application.ErrDeviceCommandRejected},
		{"batch error", `[{"status":[{"status":"error"}]}]`, ErrDeviceRefused, application.ErrDeviceCommandRejected},
		{"parse error", `[{"parse":{"status":[{"status":"error"}]}}]`, ErrDeviceRefused, application.ErrDeviceCommandRejected},
		{"bad JSON", `truncated`, ErrDeviceAnswer, application.ErrDeviceAnswerInvalid},
		{"null", `null`, ErrDeviceAnswer, application.ErrDeviceAnswerInvalid},
		{"wrong status shape", `{"status":"error"}`, ErrDeviceAnswer, application.ErrDeviceAnswerInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			s := session{client: server.Client(), baseURL: server.URL}
			for _, out := range []any{nil, &map[string]any{}} {
				err := s.command(context.Background(), "/rci/system/configuration/save", map[string]any{}, out)
				if !errors.Is(err, tc.want) || !errors.Is(err, tc.kind) {
					t.Fatalf("out=%T error=%v want=%v and %v", out, err, tc.want, tc.kind)
				}
			}
		})
	}
}

func TestRollbackRequiresAcceptedSaveAndMatchingReadback(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		noop, restoreError, saveError, readError bool
	}{
		{name: "restored"}, {name: "restore HTTP200 error", restoreError: true}, {name: "save HTTP200 error", saveError: true}, {name: "restore HTTP200 no-op", noop: true}, {name: "readback unavailable", readError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			double := newDeviceDouble("5.1.2")
			base := double.handler()
			restoring := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ci/startup-config" && r.Method == http.MethodPost {
					restoring = true
					if tc.restoreError {
						_, _ = w.Write([]byte(`{"status":[{"status":"error"}]}`))
						return
					}
					if tc.noop {
						w.WriteHeader(http.StatusOK)
						return
					}
				}
				if r.URL.Path == "/rci/system/configuration/save" && tc.saveError {
					_, _ = w.Write([]byte(`{"status":[{"status":"error"}]}`))
					return
				}
				if r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet && tc.readError && restoring {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				base.ServeHTTP(w, r)
			}))
			defer server.Close()
			d := New(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			connection := application.Connection{URL: "http://10.255.0.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}
			backup, err := d.Backup(context.Background(), application.DeviceInfo{}, connection)
			if err != nil {
				t.Fatal(err)
			}
			double.config = []byte("! changed configuration\n")
			err = d.Rollback(context.Background(), application.DeviceInfo{Interface: deviceInterface}, connection, backup)
			wantFailure := tc.noop || tc.restoreError || tc.saveError || tc.readError
			if (err != nil) != wantFailure {
				t.Fatalf("rollback=%v failure=%t", err, wantFailure)
			}
		})
	}
}
