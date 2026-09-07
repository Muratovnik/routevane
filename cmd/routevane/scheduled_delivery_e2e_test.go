package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// A scheduled refresh publishes a new artifact and carries that exact artifact
// to the one explicitly bound, opted-in device through the normal
// probe/backup/deploy/verify lifecycle. This crosses HTTP, SQLite, scheduler,
// publication, registry, deployment, filesystem backup and local deployer.
func TestScheduledRefreshAutomaticallyDeliversAnExplicitOutputBinding(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	singBoxDir := t.TempDir()
	configPath := filepath.Join(singBoxDir, "config.json")
	ruleSetPath := filepath.Join(singBoxDir, "routevane.json")
	writeLocalSingBoxConfig(t, configPath, "routevane.json")

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time, 1)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	deps := runtimeDeps{
		Now: func() time.Time { return now }, Resolver: resolver,
		NewDelayTicker: func(time.Duration) delayTicker {
			return &testDelayTicker{ch: ticks}
		},
	}
	origin, cancel, done, stderr := startServeServer(t, catalog, data, deps)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID := createScheduledProfile(t, origin, []string{"youtube"})
	outputID := addScheduledOutput(t, origin, profileID, "singbox")
	var created struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	body := postJSON(t, origin+"/v1/devices", `{"target_id":"singbox","name":"Local sing-box","address":`+mustQuote(t, localFileURL(configPath))+`,"account":"","interface":""}`)
	if err := json.Unmarshal(body, &created); err != nil || len(created.Device.ID) != 32 {
		t.Fatalf("create device=%s err=%v", body, err)
	}
	postJSON(t, origin+"/v1/outputs/"+outputID+"/device", `{"device_id":`+mustQuote(t, created.Device.ID)+`}`)
	postJSON(t, origin+"/v1/devices/"+created.Device.ID+"/auto-delivery", `{"enabled":true,"credential":""}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/schedule", `{"refresh_interval":"daily"}`)

	ticks <- now
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if payload, err := os.ReadFile(ruleSetPath); err == nil {
			if singbox.Validate(payload) == nil && strings.Contains(string(payload), "youtube.com") && scheduledDeliverySucceeded(stderr.String(), profileID, outputID, created.Device.ID) {
				current := scheduledProfileDetail(t, origin, profileID).output("singbox")
				if current.Latest == nil {
					t.Fatal("scheduled delivery wrote a file without publishing an artifact")
				}
				artifact := httpGet(t, origin+"/v1/artifacts/"+current.Latest.ID, nil)
				if artifact.status != 200 || string(artifact.body) != string(payload) {
					t.Fatalf("delivered file is not the scheduled artifact: status=%d\n%s\n%s", artifact.status, artifact.body, payload)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scheduled delivery did not complete: %s", stderr.String())
}

func mustQuote(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func scheduledDeliverySucceeded(logs, profileID, outputID, deviceID string) bool {
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record["operation"] == "schedule" && record["list"] == profileID && record["status"] == "succeeded" &&
			record["count"] == float64(1) && record["delivered"] == float64(1) && record["delivery_failures"] == float64(0) {
			return true
		}
		if record["output"] == outputID && record["device"] == deviceID && record["status"] == "failed" {
			return false
		}
	}
	return false
}
