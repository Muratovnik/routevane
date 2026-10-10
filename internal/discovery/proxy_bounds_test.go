package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type refusalResolver struct{}

func (refusalResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestProxyRefusalDiagnosticsRemainBounded(t *testing.T) {
	const attempts = 64
	for _, kind := range []string{"connect", "plaintext", "invalid", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			proxy := newGuardedProxy(refusalResolver{}, nil, 3, 1, 1024)
			for i := range attempts {
				host := fmt.Sprintf("h%d.example.com", i)
				request := httptest.NewRequest(http.MethodConnect, "https://"+host, nil)
				request.Host = host + ":443"
				wantStatus := http.StatusForbidden
				switch kind {
				case "plaintext":
					request = httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
				case "invalid":
					request.Host = "invalid-" + host
					wantStatus = http.StatusBadRequest
				case "oversized":
					request.Host = strings.Repeat("x", MaxTargetLength) + host + ":443"
				}
				response := httptest.NewRecorder()
				proxy.ServeHTTP(response, request)
				if response.Code != wantStatus {
					t.Fatalf("request %d status=%d, want %d", i, response.Code, wantStatus)
				}
			}
			hosts, blocked := proxy.observed()
			retained := 3
			if kind == "oversized" {
				retained = 0
			}
			evidence := proxy.evidence(Target{})
			if len(hosts) != 0 || len(blocked) != retained || len(evidence.Hosts) != retained {
				t.Fatalf("retained contacted=%d, blocked=%d, evidence=%d, want %d refusals", len(hosts), len(blocked), len(evidence.Hosts), retained)
			}
			if evidence.OmittedRefusals != int64(attempts-retained) {
				t.Fatalf("omitted refusals=%d, want %d", evidence.OmittedRefusals, attempts-retained)
			}
			payload, err := json.Marshal(evidence)
			if err != nil || len(payload) > 1024 {
				t.Fatalf("refusal report is not bounded: bytes=%d, error=%v", len(payload), err)
			}
		})
	}
}

func TestProxyHostAdmissionRefusalsShareTheDiagnosticBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	dialer := controlledDialer{calls: make(chan pendingDial)}
	proxy := newGuardedProxy(fixtureResolver{"busy.example.com": {netip.MustParseAddr("203.0.113.1")}}, dialer, 4, 1, 1024)
	done := startConnect(ctx, proxy, "busy.example.com:443")
	call := awaitDial(t, ctx, dialer, done)
	for i := range 64 {
		host := fmt.Sprintf("h%d.example.com", i)
		request := httptest.NewRequest(http.MethodConnect, "https://"+host, nil)
		request.Host = host + ":443"
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("host-limit request status=%d", response.Code)
		}
	}
	_, blocked := proxy.observed()
	if len(blocked) != 4 || blocked["h0.example.com"] != RefusedHostLimit || proxy.evidence(Target{}).OmittedRefusals != 60 {
		t.Fatalf("host-limit diagnostics escaped their bound: retained=%d, first=%q, omitted=%d", len(blocked), blocked["h0.example.com"], proxy.evidence(Target{}).OmittedRefusals)
	}
	call.result <- errors.New("finish the pending connection")
	awaitConnect(t, ctx, done)
}
