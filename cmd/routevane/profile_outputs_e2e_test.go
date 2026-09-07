package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProfileEditsReachEveryOutputThroughTheSameSubscription is the end-to-end
// oracle of ADR 0013: one list feeds several formats, editing its composition
// changes what those formats publish, and the subscription URL issued once at
// output creation keeps resolving to the newest file.
func TestProfileEditsReachEveryOutputThroughTheSameSubscription(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{
		"youtube.expiry.test": {"192.0.2.10"},
		"discord.expiry.test": {"198.51.100.20"},
	})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID, routerOutput, subscriptionURL := createProfileOutput(t, origin, "Видео", "keenetic", "youtube")
	singboxOutput := addOutput(t, origin, profileID, "singbox")

	first := refreshAndBuild(t, origin, profileID, routerOutput)
	firstFile := httpGet(t, subscriptionURL, nil)
	if firstFile.status != http.StatusOK || !strings.Contains(string(firstFile.body), "192.0.2.10") {
		t.Fatalf("first publication=%d %s", firstFile.status, firstFile.body)
	}
	if strings.Contains(string(firstFile.body), "198.51.100.20") {
		t.Fatalf("a service outside the list reached the file: %s", firstFile.body)
	}

	// Editing the list is what a rebuild then publishes: the output row, its
	// subscription and its identity are untouched by the edit.
	updated := postJSON(t, origin+"/v1/profiles/"+profileID+"/update", `{"name":"Видео и общение","lists":["youtube","discord"]}`)
	var updateResponse struct {
		Profile struct {
			ID    string   `json:"id"`
			Name  string   `json:"name"`
			Lists []string `json:"lists"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(updated, &updateResponse); err != nil {
		t.Fatal(err)
	}
	if updateResponse.Profile.ID != profileID || updateResponse.Profile.Name != "Видео и общение" || len(updateResponse.Profile.Lists) != 2 {
		t.Fatalf("update response=%s", updated)
	}

	second := refreshAndBuild(t, origin, profileID, routerOutput)
	if second.Artifact.ID == first.Artifact.ID {
		t.Fatal("an edited list republished the same artifact")
	}
	secondFile := httpGet(t, subscriptionURL, nil)
	if secondFile.status != http.StatusOK {
		t.Fatalf("second publication status=%d", secondFile.status)
	}
	for _, address := range []string{"192.0.2.10", "198.51.100.20"} {
		if !strings.Contains(string(secondFile.body), address) {
			t.Fatalf("the edited composition is missing %s: %s", address, secondFile.body)
		}
	}

	// The second output serves the same list in its own format. It is
	// domain-capable, so it carries the names rather than the addresses the
	// router dialect had to fall back to.
	singboxBuild := refreshAndBuild(t, origin, profileID, singboxOutput)
	singboxFile := httpGet(t, origin+"/v1/artifacts/"+singboxBuild.Artifact.ID, nil)
	if singboxFile.status != http.StatusOK {
		t.Fatalf("second format status=%d body=%s", singboxFile.status, singboxFile.body)
	}
	for _, domain := range []string{"youtube.com", "discord.com"} {
		if !strings.Contains(string(singboxFile.body), domain) {
			t.Fatalf("the second format is missing %s: %s", domain, singboxFile.body)
		}
	}
	if string(singboxFile.body) == string(secondFile.body) {
		t.Fatal("two formats produced identical bytes")
	}

	// One output per format: asking again returns the existing one instead of
	// issuing a second subscription for the same file.
	duplicate := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"keenetic"}`)
	if duplicate.status != http.StatusConflict {
		t.Fatalf("duplicate output status=%d body=%s", duplicate.status, duplicate.body)
	}

	// The library states the list, its composition and both of its outputs.
	library := httpGet(t, origin+"/v1/profiles", nil)
	var listing struct {
		Profiles []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Lists   []string `json:"lists"`
			Outputs []struct {
				ID       string `json:"id"`
				TargetID string `json:"target_id"`
				Latest   *struct {
					ID string `json:"id"`
				} `json:"latest"`
			} `json:"outputs"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(library.body, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Profiles) != 1 || listing.Profiles[0].Name != "Видео и общение" || len(listing.Profiles[0].Lists) != 2 || len(listing.Profiles[0].Outputs) != 2 {
		t.Fatalf("library=%s", library.body)
	}
	for _, output := range listing.Profiles[0].Outputs {
		if output.Latest == nil {
			t.Fatalf("output %s (%s) reports no published file", output.ID, output.TargetID)
		}
	}
	if strings.Contains(string(library.body), "rv1.") {
		t.Fatalf("the library repeats a subscription secret: %s", library.body)
	}
}
