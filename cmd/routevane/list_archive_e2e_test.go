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

// TestAnArchivedListStopsChangingAndKeepsServing is the end-to-end oracle of
// archival: the list leaves the shelf and stops accepting change, while the
// file it already published and the subscription that carries it go on working
// exactly as before. Nothing is deleted (ADR 0004), so restoring puts the list
// back with its outputs, its history and the same subscription URL.
func TestAnArchivedListStopsChangingAndKeepsServing(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	listID, outputID, subscriptionURL := createListOutput(t, origin, "Дача", "keenetic", "youtube")
	published := refreshAndBuild(t, origin, listID, outputID)
	served := httpGet(t, subscriptionURL, nil)
	if served.status != http.StatusOK || !strings.Contains(string(served.body), "192.0.2.10") {
		t.Fatalf("first publication=%d %s", served.status, served.body)
	}

	archived := postJSON(t, origin+"/v1/profiles/"+listID+"/archive", `{}`)
	var archivedResponse struct {
		List struct {
			ID         string `json:"id"`
			ArchivedAt string `json:"archived_at"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(archived, &archivedResponse); err != nil {
		t.Fatal(err)
	}
	if archivedResponse.List.ID != listID || archivedResponse.List.ArchivedAt == "" {
		t.Fatalf("archive response=%s", archived)
	}

	// What stops is change. Every route that would rewrite the list or its
	// files refuses with a conflict, naming the state rather than the body.
	refusals := map[string]struct{ path, body string }{
		"edit":       {"/v1/profiles/" + listID + "/update", `{"name":"Дача и офис","lists":["youtube","discord"]}`},
		"refresh":    {"/v1/profiles/" + listID + "/refresh", `{}`},
		"build":      {"/v1/outputs/" + outputID + "/build", `{}`},
		"add output": {"/v1/profiles/" + listID + "/outputs", `{"target_id":"singbox"}`},
		"schedule":   {"/v1/profiles/" + listID + "/schedule", `{"refresh_interval":"daily"}`},
	}
	for name, request := range refusals {
		t.Run(name, func(t *testing.T) {
			refused := postGuardedBody(t, origin+request.path, request.body)
			if refused.status != http.StatusConflict {
				t.Fatalf("%s on an archived list status=%d body=%s", name, refused.status, refused.body)
			}
		})
	}

	// The upstream moved while the list was archived. Because nothing can
	// rebuild it, the file its subscribers receive is byte for byte the one it
	// was archived with, down to the validator etag.
	resolver.set(map[string][]string{"youtube.expiry.test": {"198.51.100.77"}})
	stillServed := httpGet(t, subscriptionURL, nil)
	if stillServed.status != http.StatusOK {
		t.Fatalf("an archived subscription stopped serving: %d", stillServed.status)
	}
	if !bytes.Equal(stillServed.body, served.body) || stillServed.etag != served.etag {
		t.Fatalf("an archived list changed what it serves: etag %q/%q body %s", stillServed.etag, served.etag, stillServed.body)
	}
	if direct := httpGet(t, origin+"/v1/artifacts/"+published.Artifact.ID, nil); direct.status != http.StatusOK {
		t.Fatalf("the published artifact stopped downloading: %d", direct.status)
	}

	// The library still carries the list, marked. A row that vanished would
	// leave the operator with a working subscription they could not find.
	shelved := httpGet(t, origin+"/v1/profiles", nil)
	var listing struct {
		Lists []struct {
			ID         string `json:"id"`
			ArchivedAt string `json:"archived_at"`
			Outputs    []struct {
				ID     string `json:"id"`
				Latest *struct {
					ID string `json:"id"`
				} `json:"latest"`
			} `json:"outputs"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(shelved.body, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Lists) != 1 || listing.Lists[0].ID != listID || listing.Lists[0].ArchivedAt == "" {
		t.Fatalf("library=%s", shelved.body)
	}
	if len(listing.Lists[0].Outputs) != 1 || listing.Lists[0].Outputs[0].Latest == nil {
		t.Fatalf("an archived list lost its published output: %s", shelved.body)
	}

	// Restoring returns the list to every write it refused, and the
	// subscription issued before it was archived is still the one that resolves
	// to the new file. Tokens are never rotated (ADR 0004).
	restored := postJSON(t, origin+"/v1/profiles/"+listID+"/restore", `{}`)
	if strings.Contains(string(restored), `"archived_at"`) {
		t.Fatalf("restore response still carries an archival date: %s", restored)
	}
	rebuilt := refreshAndBuild(t, origin, listID, outputID)
	if rebuilt.Artifact.ID == published.Artifact.ID {
		t.Fatal("a restored list republished the artifact it was archived with")
	}
	after := httpGet(t, subscriptionURL, nil)
	if after.status != http.StatusOK || !strings.Contains(string(after.body), "198.51.100.77") {
		t.Fatalf("restored publication=%d %s", after.status, after.body)
	}
}
