package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/plugin"
)

func TestMain(m *testing.M) {
	if handled, exitCode := plugin.RunProcessRunner(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	os.Exit(m.Run())
}

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"version"})

	if code != 0 {
		t.Fatalf("run() code = %d, want 0", code)
	}
	if got := stdout.String(); got != "routevane dev\n" {
		t.Fatalf("stdout = %q, want %q", got, "routevane dev\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"unknown"})

	if code != 2 {
		t.Fatalf("run() code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "usage:") {
		t.Fatalf("stderr = %q, want usage", got)
	}
}

func TestParseServeExposesOnlyValidatedPort(t *testing.T) {
	if options, ok := parseServe(nil); !ok || options.Port != 8765 {
		t.Fatalf("default serve=%#v ok=%v", options, ok)
	}
	for _, args := range [][]string{{"--port", "0"}, {"--port", "65536"}, {"--port", "-1"}, {"--host", "0.0.0.0"}, {"--listen", "192.0.2.1:8765"}} {
		if _, ok := parseServe(args); ok {
			t.Fatalf("unsafe serve args accepted: %v", args)
		}
	}
}

func TestStructuredLogHasOnlyStableFields(t *testing.T) {
	var output bytes.Buffer
	logResult(newLogger(&output), "refresh", "example", "dns-main", "failed", 2, time.Now(), "source_failed")
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"operation": true, "service": true, "source": true, "status": true, "count": true, "duration": true, "error_code": true}
	if len(record) != len(want) {
		t.Fatalf("log fields=%v", record)
	}
	for key := range record {
		if !want[key] {
			t.Fatalf("unexpected log field %q in %v", key, record)
		}
	}
}

type commandFakeResolver struct{ calls int }

func (r *commandFakeResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	r.calls++
	if r.calls%2 == 0 {
		return []string{"192.0.2.1", "::ffff:192.0.2.2", "192.0.2.1"}, nil
	}
	return []string{"::ffff:192.0.2.2", "192.0.2.1", "192.0.2.1"}, nil
}

func (*commandFakeResolver) LookupCNAME(_ context.Context, _ string) (string, error) {
	return "", nil
}

func TestRunPreviewWithFakeDependenciesIsByteStable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	deps := runtimeDeps{Resolver: &commandFakeResolver{}, Now: func() time.Time { return now }}
	var firstOut, firstErr bytes.Buffer
	if code := runWithDeps(&firstOut, &firstErr, []string{"preview", "--service", "example", "--target", "raw-json"}, deps); code != 0 {
		t.Fatalf("first run code = %d, stderr = %q", code, firstErr.String())
	}
	var secondOut, secondErr bytes.Buffer
	if code := runWithDeps(&secondOut, &secondErr, []string{"preview", "--service", "example", "--target", "raw-json"}, deps); code != 0 {
		t.Fatalf("second run code = %d, stderr = %q", code, secondErr.String())
	}
	if firstErr.Len() != 0 || secondErr.Len() != 0 {
		t.Fatalf("stderr = %q / %q", firstErr.String(), secondErr.String())
	}
	if !bytes.Equal(firstOut.Bytes(), secondOut.Bytes()) {
		t.Fatalf("preview output changed:\n%s\n%s", firstOut.String(), secondOut.String())
	}
	if !strings.HasSuffix(firstOut.String(), "\n") || !strings.Contains(firstOut.String(), `"interface_version":"m0-spike-v1"`) || !strings.Contains(firstOut.String(), `"rules":[`) || !strings.Contains(firstOut.String(), `"value":"example.com"`) || !strings.Contains(firstOut.String(), `"observation_count":1`) {
		t.Fatalf("unexpected preview JSON: %q", firstOut.String())
	}
}
