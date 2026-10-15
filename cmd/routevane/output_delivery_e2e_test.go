package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// The profile page states when each format's file last reached the saved
// connection the format is bound to. Only a succeeded send to that registered
// device is that: a failed send, a send to an address typed by hand and a send
// to another connection leave the card without one, and the card keeps its
// build attempt as a separate fact.
func TestOutputCardReportsOnlyASuccessfulSendToTheBoundConnection(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	configPath := filepath.Join(t.TempDir(), "config.json")
	ruleSetPath := filepath.Join(filepath.Dir(configPath), "routevane.json")
	writeLocalSingBoxConfig(t, configPath, ruleSetPath)
	typedConfig := filepath.Join(t.TempDir(), "config.json")
	typedRuleSet := filepath.Join(filepath.Dir(typedConfig), "routevane.json")
	writeLocalSingBoxConfig(t, typedConfig, typedRuleSet)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC).UnixNano())
	nowFunc := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	now := nowFunc()
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, nowFunc)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()
	profileID := createScheduledProfile(t, origin, []string{"youtube"})
	outputID := addScheduledOutput(t, origin, profileID, "singbox")
	build := refreshAndBuild(t, origin, profileID, outputID)
	var registered struct {
		Device application.Device `json:"device"`
	}
	body := postJSON(t, origin+"/v1/devices", `{"target_id":"singbox","name":"Local sing-box","address":`+mustQuote(t, localFileURL(configPath))+`,"account":"","interface":""}`)
	if err := json.Unmarshal(body, &registered); err != nil || registered.Device.ID == "" {
		t.Fatalf("device=%s err=%v", body, err)
	}
	postJSON(t, origin+"/v1/outputs/"+outputID+"/device", `{"device_id":`+mustQuote(t, registered.Device.ID)+`}`)
	endpoint := origin + "/v1/artifacts/" + build.Artifact.ID + "/deploy"
	send := func(attemptID string, request map[string]any) httpResponse {
		t.Helper()
		request["confirm"], request["attempt_id"] = true, attemptID
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return postGuardedBody(t, endpoint, string(encoded))
	}
	savedConnection := map[string]any{
		"device_id": registered.Device.ID,
		"expected_connection": application.DeviceConnectionSnapshot{
			Address: registered.Device.Address, Account: registered.Device.Account, Interface: registered.Device.Interface,
		},
	}

	// card reads the output the way both screens do: the profile page and the
	// profile shelf. The two must state the same delivery.
	card := func() map[string]any {
		t.Helper()
		var page struct {
			Outputs []map[string]any `json:"outputs"`
		}
		view := httpGet(t, origin+"/v1/profiles/"+profileID, nil)
		if view.status != 200 || json.Unmarshal(view.body, &page) != nil || len(page.Outputs) != 1 {
			t.Fatalf("profile view status=%d body=%s", view.status, view.body)
		}
		var shelf struct {
			Profiles []struct {
				Outputs []map[string]any `json:"outputs"`
			} `json:"profiles"`
		}
		list := httpGet(t, origin+"/v1/profiles", nil)
		if list.status != 200 || json.Unmarshal(list.body, &shelf) != nil || len(shelf.Profiles) != 1 || len(shelf.Profiles[0].Outputs) != 1 {
			t.Fatalf("profile shelf status=%d body=%s", list.status, list.body)
		}
		if viewed, listed := page.Outputs[0]["last_delivery"], shelf.Profiles[0].Outputs[0]["last_delivery"]; !jsonEqual(t, viewed, listed) {
			t.Fatalf("the profile view says %v and the shelf says %v", viewed, listed)
		}
		return page.Outputs[0]
	}
	requireNoDelivery := func(label string) {
		t.Helper()
		output := card()
		if delivery, present := output["last_delivery"]; present {
			t.Fatalf("%s: the card reports a delivery %v", label, delivery)
		}
		if output["last_attempt"] == nil {
			t.Fatalf("%s: the build attempt disappeared from the card: %v", label, output)
		}
	}

	requireNoDelivery("before any send")

	// A send to an address typed by hand succeeds and names no connection.
	typed := send(strings.Repeat("1", 32), map[string]any{"device": localFileURL(typedConfig)})
	if typed.status != 200 || !strings.Contains(string(typed.body), `"status":"succeeded"`) {
		t.Fatalf("typed address send status=%d body=%s", typed.status, typed.body)
	}
	if written, err := os.ReadFile(typedRuleSet); err != nil || len(written) == 0 {
		t.Fatalf("the typed send did not install the file: %v", err)
	}
	requireNoDelivery("after a send to a typed address")

	// A send to the saved connection fails once its configuration is gone: the
	// attempt is recorded as failed and delivered nothing.
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	failed := send(strings.Repeat("2", 32), cloneSend(savedConnection))
	if failed.status < 400 || !strings.Contains(string(failed.body), `"status":"failed"`) {
		t.Fatalf("a send to a missing configuration status=%d body=%s", failed.status, failed.body)
	}
	requireNoDelivery("after a failed send to the saved connection")

	// The same send succeeds once the configuration is back, and the card then
	// names the connection, the file and the moment it completed.
	writeLocalSingBoxConfig(t, configPath, ruleSetPath)
	sent := send(strings.Repeat("3", 32), cloneSend(savedConnection))
	if sent.status != 200 || !strings.Contains(string(sent.body), `"status":"succeeded"`) {
		t.Fatalf("send to the saved connection status=%d body=%s", sent.status, sent.body)
	}
	want := map[string]any{
		"device_id":    registered.Device.ID,
		"artifact_id":  build.Artifact.ID,
		"completed_at": now.Format(time.RFC3339Nano),
	}
	if got := card()["last_delivery"]; !jsonEqual(t, got, want) {
		t.Fatalf("last_delivery = %v, want %v", got, want)
	}

	// A later failure does not take the delivery back.
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if later := send(strings.Repeat("4", 32), cloneSend(savedConnection)); later.status < 400 {
		t.Fatalf("a send to a missing configuration status=%d body=%s", later.status, later.body)
	}
	if got := card()["last_delivery"]; !jsonEqual(t, got, want) {
		t.Fatalf("a failed send changed last_delivery to %v", got)
	}

	// A newer one-off send to another saved connection is not the bound one's
	// delivery: the card keeps naming the connection the format is bound to, and
	// follows the binding when it moves.
	otherConfig := filepath.Join(t.TempDir(), "config.json")
	otherRuleSet := filepath.Join(filepath.Dir(otherConfig), "routevane.json")
	writeLocalSingBoxConfig(t, otherConfig, otherRuleSet)
	writeLocalSingBoxConfig(t, configPath, ruleSetPath)
	var second struct {
		Device application.Device `json:"device"`
	}
	body = postJSON(t, origin+"/v1/devices", `{"target_id":"singbox","name":"Other sing-box","address":`+mustQuote(t, localFileURL(otherConfig))+`,"account":"","interface":""}`)
	if err := json.Unmarshal(body, &second); err != nil || second.Device.ID == "" {
		t.Fatalf("second device=%s err=%v", body, err)
	}
	clock.Add(int64(time.Hour))
	now = nowFunc()
	oneOff := send(strings.Repeat("5", 32), map[string]any{
		"device_id": second.Device.ID,
		"expected_connection": application.DeviceConnectionSnapshot{
			Address: second.Device.Address, Account: second.Device.Account, Interface: second.Device.Interface,
		},
	})
	if oneOff.status != 200 || !strings.Contains(string(oneOff.body), `"status":"succeeded"`) {
		t.Fatalf("send to the other connection status=%d body=%s", oneOff.status, oneOff.body)
	}
	if got := card()["last_delivery"]; !jsonEqual(t, got, want) {
		t.Fatalf("a newer send to another connection changed last_delivery to %v", got)
	}
	postJSON(t, origin+"/v1/outputs/"+outputID+"/device", `{"device_id":`+mustQuote(t, second.Device.ID)+`}`)
	wantSecond := map[string]any{
		"device_id":    second.Device.ID,
		"artifact_id":  build.Artifact.ID,
		"completed_at": now.Format(time.RFC3339Nano),
	}
	if got := card()["last_delivery"]; !jsonEqual(t, got, wantSecond) {
		t.Fatalf("after binding to the other connection last_delivery = %v, want %v", got, wantSecond)
	}
	postJSON(t, origin+"/v1/outputs/"+outputID+"/device", `{"device_id":`+mustQuote(t, registered.Device.ID)+`}`)
	if got := card()["last_delivery"]; !jsonEqual(t, got, want) {
		t.Fatalf("after binding back last_delivery = %v, want %v", got, want)
	}
}

func cloneSend(request map[string]any) map[string]any {
	clone := make(map[string]any, len(request)+2)
	for key, value := range request {
		clone[key] = value
	}
	return clone
}

// jsonEqual compares two decoded JSON values by their canonical encoding.
func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(left) == string(right)
}
