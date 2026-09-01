package application

import (
	"reflect"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestDegradedSourcesSelectsOnlyFailingSourcesInsideTheGraceWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	health := []SourceRunState{
		// Failing now, last success well inside the window.
		{SourceID: "inside", SourceRevision: "r1", LastSuccessAt: now.Add(-2 * time.Hour), LastFailureAt: now.Add(-time.Minute)},
		// Failing, but the window closed before the cutoff.
		{SourceID: "expired", SourceRevision: "r1", LastSuccessAt: now.Add(-SourceGracePeriod - time.Minute), LastFailureAt: now.Add(-time.Minute)},
		// Healthy: the latest cycle succeeded.
		{SourceID: "healthy", SourceRevision: "r1", LastSuccessAt: now.Add(-time.Minute), LastFailureAt: now.Add(-time.Hour)},
		// Never succeeded, so nothing is being kept alive.
		{SourceID: "never", SourceRevision: "r1", LastFailureAt: now.Add(-time.Minute)},
		// Failing inside the window but under a superseded revision.
		{SourceID: "stalerevision", SourceRevision: "old", LastSuccessAt: now.Add(-time.Hour), LastFailureAt: now.Add(-time.Minute)},
	}
	active := map[string]string{"inside": "r1", "expired": "r1", "healthy": "r1", "never": "r1", "stalerevision": "r1"}
	degraded := DegradedSources(health, active, now)
	if len(degraded) != 1 || degraded[0].SourceID != "inside" {
		t.Fatalf("degraded = %#v", degraded)
	}
	if !degraded[0].GraceUntil.Equal(now.Add(-2 * time.Hour).Add(SourceGracePeriod)) {
		t.Fatalf("grace window = %v", degraded[0].GraceUntil)
	}
}

func TestDegradedSourcesIsSortedAndKeepsTheLongestWindowPerSource(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	health := []SourceRunState{
		{SourceID: "beta", SourceRevision: "r1", LastSuccessAt: now.Add(-3 * time.Hour), LastFailureAt: now},
		{SourceID: "alpha", SourceRevision: "r1", LastSuccessAt: now.Add(-4 * time.Hour), LastFailureAt: now},
		{SourceID: "alpha", SourceRevision: "r2", LastSuccessAt: now.Add(-time.Hour), LastFailureAt: now},
	}
	degraded := DegradedSources(health, nil, now)
	ids := []string{degraded[0].SourceID, degraded[1].SourceID}
	if !reflect.DeepEqual(ids, []string{"alpha", "beta"}) {
		t.Fatalf("ids = %v", ids)
	}
	if !degraded[0].GraceUntil.Equal(now.Add(-time.Hour).Add(SourceGracePeriod)) {
		t.Fatalf("a revision change must not shorten an active window: %v", degraded[0].GraceUntil)
	}
}

func TestSourceRunStateLastRunFailed(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		state SourceRunState
		want  bool
	}{
		{"never ran", SourceRunState{}, false},
		{"only succeeded", SourceRunState{LastSuccessAt: now}, false},
		{"only failed", SourceRunState{LastFailureAt: now}, true},
		{"failure is newer", SourceRunState{LastSuccessAt: now.Add(-time.Hour), LastFailureAt: now}, true},
		{"success is newer", SourceRunState{LastSuccessAt: now, LastFailureAt: now.Add(-time.Hour)}, false},
		{"same instant favors success", SourceRunState{LastSuccessAt: now, LastFailureAt: now}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.state.LastRunFailed(); got != testCase.want {
				t.Fatalf("LastRunFailed = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestSourceErrorCodeIsStablePerSourceType(t *testing.T) {
	cases := []struct {
		sourceType domain.SourceType
		want       string
	}{
		{domain.SourceDNS, "dns_observation_failed"},
		{domain.SourceHTTP, "http_feed_failed"},
		{domain.SourceType("example-plugin"), "external_source_failed"},
	}
	for _, testCase := range cases {
		if got := SourceErrorCode(testCase.sourceType); got != testCase.want {
			t.Fatalf("SourceErrorCode(%q) = %q, want %q", testCase.sourceType, got, testCase.want)
		}
	}
}
