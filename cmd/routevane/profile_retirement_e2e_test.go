package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRetiringFormatsAndProfilesWithdrawsTheirLinksAndFreesTheLibrary is the
// end-to-end oracle of ADR 0051 through the running service: a reissued link
// replaces the old one, a removed format and a deleted profile stop answering
// everywhere, their subscriptions say they are gone, and a list that only a
// deleted profile named can finally leave the library.
func TestRetiringFormatsAndProfilesWithdrawsTheirLinksAndFreesTheLibrary(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID, outputID, firstURL := createProfileOutput(t, origin, "Дача", "keenetic", "youtube")
	served := httpGet(t, firstURL, nil)
	if served.status != http.StatusOK || !strings.Contains(string(served.body), "192.0.2.10") {
		t.Fatalf("first publication=%d %s", served.status, served.body)
	}
	var added struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"singbox"}`), &added); err != nil {
		t.Fatal(err)
	}
	var built struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(postJSON(t, origin+"/v1/outputs/"+added.Output.ID+"/build", `{}`), &built); err != nil || built.SubscriptionURL == "" {
		t.Fatalf("second format build err=%v", err)
	}
	formatURL := built.SubscriptionURL

	// A reissued link replaces the old one: the old link is gone, the new one
	// serves the same file, and a second request never sees the token again.
	var reissued struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(postJSON(t, origin+"/v1/outputs/"+outputID+"/subscription/reissue", `{}`), &reissued); err != nil {
		t.Fatal(err)
	}
	secondURL := reissued.SubscriptionURL
	if !strings.HasPrefix(secondURL, origin+"/v1/subscriptions/rv1.") || secondURL == firstURL {
		t.Fatalf("reissued link=%q", secondURL)
	}
	if old := httpGet(t, firstURL, nil); old.status != http.StatusGone || !strings.Contains(string(old.body), `"subscription_revoked"`) {
		t.Fatalf("the replaced link answered %d %s", old.status, old.body)
	}
	if fresh := httpGet(t, secondURL, nil); fresh.status != http.StatusOK || !bytes.Equal(fresh.body, served.body) || fresh.etag != served.etag {
		t.Fatalf("the reissued link answered %d etag %q/%q", fresh.status, fresh.etag, served.etag)
	}
	output := httpGet(t, origin+"/v1/outputs/"+outputID, nil)
	if strings.Contains(string(output.body), strings.TrimPrefix(secondURL, origin+"/v1/subscriptions/")) {
		t.Fatalf("an output read returned the reissued token: %s", output.body)
	}

	// Removing one format retires that format alone.
	removed := postGuardedBody(t, origin+"/v1/outputs/"+added.Output.ID+"/remove", `{}`)
	if removed.status != http.StatusOK || !strings.Contains(string(removed.body), `"removed_at":"2026-10-13T12:00:00Z"`) {
		t.Fatalf("remove=%d %s", removed.status, removed.body)
	}
	if gone := httpGet(t, formatURL, nil); gone.status != http.StatusGone {
		t.Fatalf("a removed format's link answered %d", gone.status)
	}
	for name, path := range map[string]string{
		"read":   "/v1/outputs/" + added.Output.ID,
		"remove": "/v1/outputs/" + added.Output.ID + "/remove",
		"build":  "/v1/outputs/" + added.Output.ID + "/build",
	} {
		var status int
		if name == "read" {
			status = httpGet(t, origin+path, nil).status
		} else {
			status = postGuardedBody(t, origin+path, `{}`).status
		}
		if status != http.StatusNotFound {
			t.Fatalf("%s of a removed format answered %d", name, status)
		}
	}
	page := httpGet(t, origin+"/v1/profiles/"+profileID, nil)
	if page.status != http.StatusOK || strings.Contains(string(page.body), added.Output.ID) || !strings.Contains(string(page.body), outputID) {
		t.Fatalf("profile page after removal=%d %s", page.status, page.body)
	}
	var screen struct {
		Outputs []struct {
			ID     string `json:"id"`
			Latest *struct {
				ID string `json:"id"`
			} `json:"latest"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(page.body, &screen); err != nil || len(screen.Outputs) != 1 || screen.Outputs[0].Latest == nil {
		t.Fatalf("profile page outputs=%s err=%v", page.body, err)
	}
	artifactID := screen.Outputs[0].Latest.ID
	if artifact := httpGet(t, origin+"/v1/artifacts/"+artifactID, nil); artifact.status != http.StatusOK {
		t.Fatalf("a live file is not addressable: %d", artifact.status)
	}
	if again := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"singbox"}`); again.status != http.StatusCreated {
		t.Fatalf("binding the removed format again answered %d %s", again.status, again.body)
	}

	// The library protects a list a live profile names, and deletion is the
	// step after archival, never instead of it.
	if inUse := postGuardedBody(t, origin+"/v1/lists/youtube/remove", `{}`); inUse.status != http.StatusConflict {
		t.Fatalf("removing a list in use answered %d", inUse.status)
	}
	if shelf := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/delete", `{}`); shelf.status != http.StatusConflict || !strings.Contains(string(shelf.body), `"profile_not_archived"`) {
		t.Fatalf("deleting a profile on the shelf answered %d %s", shelf.status, shelf.body)
	}
	postJSON(t, origin+"/v1/profiles/"+profileID+"/archive", `{}`)
	for name, path := range map[string]string{
		"reissue": "/v1/outputs/" + outputID + "/subscription/reissue",
		"remove":  "/v1/outputs/" + outputID + "/remove",
	} {
		if frozen := postGuardedBody(t, origin+path, `{}`); frozen.status != http.StatusConflict || !strings.Contains(string(frozen.body), `"profile_archived"`) {
			t.Fatalf("%s on an archived profile answered %d %s", name, frozen.status, frozen.body)
		}
	}
	if archived := httpGet(t, secondURL, nil); archived.status != http.StatusOK {
		t.Fatalf("archival stopped the link before deletion: %d", archived.status)
	}

	deleted := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/delete", `{}`)
	if deleted.status != http.StatusOK || !strings.Contains(string(deleted.body), `"id":"`+profileID+`"`) {
		t.Fatalf("delete=%d %s", deleted.status, deleted.body)
	}
	if gone := httpGet(t, secondURL, nil); gone.status != http.StatusGone {
		t.Fatalf("a deleted profile's link answered %d", gone.status)
	}
	if read := httpGet(t, origin+"/v1/profiles/"+profileID, nil); read.status != http.StatusNotFound {
		t.Fatalf("a deleted profile read answered %d", read.status)
	}
	listing := httpGet(t, origin+"/v1/profiles", nil)
	if listing.status != http.StatusOK || strings.Contains(string(listing.body), profileID) {
		t.Fatalf("the shelf still lists a deleted profile: %s", listing.body)
	}
	if artifact := httpGet(t, origin+"/v1/artifacts/"+artifactID, nil); artifact.status != http.StatusNotFound {
		t.Fatalf("a deleted profile's file is still addressable: %d", artifact.status)
	}
	// Nor can it be sent to a device: the refusal comes before any connection.
	if deploy := postGuardedBody(t, origin+"/v1/artifacts/"+artifactID+"/deploy", `{"device":"http://192.0.2.1","username":"admin","interface":"Wireguard0"}`); deploy.status != http.StatusNotFound {
		t.Fatalf("planning a deployment of a deleted profile's file answered %d %s", deploy.status, deploy.body)
	}
	if again := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/delete", `{}`); again.status != http.StatusNotFound {
		t.Fatalf("a second deletion answered %d", again.status)
	}
	if restore := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/restore", `{}`); restore.status != http.StatusNotFound {
		t.Fatalf("restoring a deleted profile answered %d", restore.status)
	}

	// Nothing the deleted profile named holds the library any longer.
	if freed := postGuardedBody(t, origin+"/v1/lists/youtube/remove", `{}`); freed.status != http.StatusNoContent {
		t.Fatalf("removing a list only a deleted profile named answered %d %s", freed.status, freed.body)
	}
}
