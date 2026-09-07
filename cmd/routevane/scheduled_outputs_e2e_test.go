package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

const scheduledSingboxTargetYAML = `id: singbox
profile_key: singbox-source-json-v1
title: sing-box
kind: app
renderer: singbox-ruleset-json
constraints:
  supports_domain_exact: true
  supports_domain_suffix: true
  supports_dynamic_dns_set: false
  supports_ipv4: true
  supports_ipv6: true
  supports_prefixes: true
  max_rules: 8192
  max_artifact_size: 1048576
renderer_options: []
manual_installation_hint: Save the file as a local source rule-set.
`

// One scheduled output exceeding its format must not prevent a sibling format
// from publishing the same refreshed route. This walks the real HTTP, catalog,
// SQLite, scheduler, planner, renderer, artifact and subscription boundaries.
func TestScheduledOutputsContinueAfterOneFormatFails(t *testing.T) {
	catalog, bulkLists := writeScheduledOutputCatalog(t)
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time, 1)
	deps := runtimeDeps{
		Now: func() time.Time { return now },
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

	profileID := createScheduledProfile(t, origin, []string{"small"})
	keeneticID := addScheduledOutput(t, origin, profileID, "keenetic")
	singboxID := addScheduledOutput(t, origin, profileID, "singbox")
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	keeneticSubscription := buildScheduledOutput(t, origin, keeneticID)
	singboxSubscription := buildScheduledOutput(t, origin, singboxID)
	initial := scheduledProfileDetail(t, origin, profileID)
	initialKeenetic := initial.output("keenetic")
	initialSingbox := initial.output("singbox")
	if initialKeenetic.Latest == nil || initialSingbox.Latest == nil {
		t.Fatalf("initial outputs were not published: %#v", initial.Outputs)
	}

	listsJSON, err := json.Marshal(bulkLists)
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, origin+"/v1/profiles/"+profileID+"/update",
		`{"name":"Scheduled formats","lists":`+string(listsJSON)+`}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/schedule", `{"refresh_interval":"daily"}`)
	ticks <- now

	var current scheduledDetail
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current = scheduledProfileDetail(t, origin, profileID)
		keenetic := current.output("keenetic")
		singbox := current.output("singbox")
		if current.Schedule.LastRefreshFailed &&
			keenetic.LastAttempt.Code == application.BuildFailureRuleLimit &&
			singbox.LastAttempt.Status == "success" && singbox.Latest != nil &&
			singbox.Latest.ID != initialSingbox.Latest.ID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	keenetic := current.output("keenetic")
	singbox := current.output("singbox")
	if keenetic.Latest == nil || keenetic.Latest.ID != initialKeenetic.Latest.ID ||
		keenetic.LastAttempt.Status != "failed" || keenetic.LastAttempt.Code != application.BuildFailureRuleLimit {
		t.Fatalf("failed output did not keep its previous publication: %#v", keenetic)
	}
	if singbox.Latest == nil || singbox.Latest.ID == initialSingbox.Latest.ID || singbox.LastAttempt.Status != "success" {
		t.Fatalf("successful sibling was not republished: %#v", singbox)
	}

	oldRouterFile := httpGet(t, keeneticSubscription, nil)
	newAppFile := httpGet(t, singboxSubscription, nil)
	if oldRouterFile.status != 200 || strings.Contains(string(oldRouterFile.body), "8.9.127.1") {
		t.Fatalf("failed output no longer serves its previous file: %d %q", oldRouterFile.status, oldRouterFile.body)
	}
	if newAppFile.status != 200 || !strings.Contains(string(newAppFile.body), "8.9.127.1") {
		t.Fatalf("successful sibling does not serve the refreshed file: %d %q", newAppFile.status, newAppFile.body)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if schedulerPartialLog(stderr.String(), profileID, keeneticID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scheduler did not report the affected output and aggregate status: %s", stderr.String())
}

type scheduledDetail struct {
	Outputs  []scheduledOutputDetail `json:"outputs"`
	Schedule struct {
		LastRefreshFailed bool `json:"last_refresh_failed"`
	} `json:"schedule"`
}

type scheduledOutputDetail struct {
	ID       string `json:"id"`
	TargetID string `json:"target_id"`
	Latest   *struct {
		ID string `json:"id"`
	} `json:"latest"`
	LastAttempt struct {
		Status string `json:"status"`
		Code   string `json:"code"`
	} `json:"last_attempt"`
}

func (d scheduledDetail) output(targetID string) scheduledOutputDetail {
	for _, output := range d.Outputs {
		if output.TargetID == targetID {
			return output
		}
	}
	return scheduledOutputDetail{}
}

func scheduledProfileDetail(t *testing.T, origin, profileID string) scheduledDetail {
	t.Helper()
	response := httpGet(t, origin+"/v1/profiles/"+profileID, nil)
	if response.status != 200 {
		t.Fatalf("list detail status=%d body=%s", response.status, response.body)
	}
	var detail scheduledDetail
	if err := json.Unmarshal(response.body, &detail); err != nil {
		t.Fatalf("list detail=%s: %v", response.body, err)
	}
	return detail
}

func createScheduledProfile(t *testing.T, origin string, lists []string) string {
	t.Helper()
	encoded, err := json.Marshal(lists)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	body := postJSON(t, origin+"/v1/profiles", `{"name":"Scheduled formats","lists":`+string(encoded)+`}`)
	if err := json.Unmarshal(body, &response); err != nil || len(response.Profile.ID) != 32 {
		t.Fatalf("create list=%s: %v", body, err)
	}
	return response.Profile.ID
}

func addScheduledOutput(t *testing.T, origin, profileID, targetID string) string {
	t.Helper()
	var response struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	body := postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":`+strconv.Quote(targetID)+`}`)
	if err := json.Unmarshal(body, &response); err != nil || len(response.Output.ID) != 32 {
		t.Fatalf("create output=%s: %v", body, err)
	}
	return response.Output.ID
}

func buildScheduledOutput(t *testing.T, origin, outputID string) string {
	t.Helper()
	var response struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	body := postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	if err := json.Unmarshal(body, &response); err != nil || !strings.HasPrefix(response.SubscriptionURL, origin+"/v1/subscriptions/") {
		t.Fatalf("build output=%s: %v", body, err)
	}
	return response.SubscriptionURL
}

func writeScheduledOutputCatalog(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"builtin", "targets"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "targets", "keenetic.yaml"), keeneticTargetYAML)
	write(filepath.Join(root, "targets", "singbox.yaml"), scheduledSingboxTargetYAML)
	write(filepath.Join(root, "builtin", "small.yaml"), "id: small\ntitle: Small\ncomponents:\n  web:\n    required: false\nseeds:\n  - kind: ipv4\n    value: 8.0.0.1\n    component: web\n    source: manual\n")

	lists := make([]string, 0, 9)
	for listIndex := 1; listIndex <= 9; listIndex++ {
		listID := fmt.Sprintf("bulk%d", listIndex)
		lists = append(lists, listID)
		var yaml strings.Builder
		fmt.Fprintf(&yaml, "id: %s\ntitle: Bulk %d\ncomponents:\n  web:\n    required: false\nseeds:\n", listID, listIndex)
		for addressIndex := 0; addressIndex < 128; addressIndex++ {
			fmt.Fprintf(&yaml, "  - kind: ipv4\n    value: 8.%d.%d.1\n    component: web\n    source: manual\n", listIndex, addressIndex)
		}
		write(filepath.Join(root, "builtin", listID+".yaml"), yaml.String())
	}
	return root, lists
}

func schedulerPartialLog(logs, profileID, failedOutputID string) bool {
	failure, aggregate := false, false
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil || record["operation"] != "schedule" || record["list"] != profileID {
			continue
		}
		if record["output"] == failedOutputID && record["target"] == "keenetic" && record["status"] == "failed" && record["error_code"] == application.BuildFailureRuleLimit {
			failure = true
		}
		if record["status"] == "partial" && record["count"] == float64(1) && record["failures"] == float64(1) {
			aggregate = true
		}
	}
	return failure && aggregate
}
