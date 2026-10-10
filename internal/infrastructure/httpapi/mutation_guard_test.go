package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

type guardedPrefixBackend struct {
	*fakeBackend
	writes int
}

func (b *guardedPrefixBackend) SetOutputFQDNPrefix(ctx context.Context, id, prefix string) (application.Output, error) {
	b.writes++
	return b.fakeBackend.SetOutputFQDNPrefix(ctx, id, prefix)
}

func TestEveryOutputPrefixMutationRequiresTheRequestGuard(t *testing.T) {
	const origin = "http://127.0.0.1:8765"
	cases := []struct {
		name   string
		change func(http.Header)
		allow  bool
	}{
		{name: "local API", allow: true},
		{name: "same-origin browser", change: func(h http.Header) { h.Set("Origin", origin) }, allow: true},
		{name: "missing marker", change: func(h http.Header) { h.Del("X-Routevane-Request") }},
		{name: "wrong marker", change: func(h http.Header) { h.Set("X-Routevane-Request", "0") }},
		{name: "duplicate marker", change: func(h http.Header) { h.Add("X-Routevane-Request", "1") }},
		{name: "missing content type", change: func(h http.Header) { h.Del("Content-Type") }},
		{name: "text content type", change: func(h http.Header) { h.Set("Content-Type", "text/plain") }},
		{name: "duplicate content type", change: func(h http.Header) { h.Add("Content-Type", "application/json") }},
		{name: "foreign origin", change: func(h http.Header) { h.Set("Origin", "https://outside.example") }},
		{name: "null origin", change: func(h http.Header) { h.Set("Origin", "null") }},
		{name: "duplicate origin", change: func(h http.Header) { h.Add("Origin", origin); h.Add("Origin", origin) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &guardedPrefixBackend{fakeBackend: testBackend()}
			server, err := New(origin, backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPut, origin+"/v1/outputs/"+strings.Repeat("a", 32)+"/fqdn-prefix", strings.NewReader(`{"fqdn_group_prefix":"home"}`))
			request.RemoteAddr = "127.0.0.1:1"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
			if tc.change != nil {
				tc.change(request.Header)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			wantStatus, wantWrites := http.StatusForbidden, 0
			if tc.allow {
				wantStatus, wantWrites = http.StatusOK, 1
			}
			if response.Code != wantStatus || backend.writes != wantWrites {
				t.Fatalf("status=%d writes=%d; want status=%d writes=%d: %s", response.Code, backend.writes, wantStatus, wantWrites, response.Body.String())
			}
		})
	}
}
