package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServeLibraryDeletionsReachThePublishedFileEndToEnd is the visible
// outcome of ADR 0029: the operator owns the library, including what the
// catalog shipped. Deleting a list reaches the file a profile publishes when the
// profile named the category that held it, and is refused with the profile named
// when the profile named the list itself. The shipped catalog file is never
// edited, and the deletion survives a restart.
func TestServeLibraryDeletionsReachThePublishedFileEndToEnd(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	start := func() (string, func()) {
		t.Helper()
		origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			cancel()
			if code := <-done; code != 0 {
				t.Errorf("server code=%d stderr=%s", code, stderr.String())
			}
		}
		t.Cleanup(stop)
		return origin, stop
	}
	origin, stopFirst := start()

	// A category of the operator's own, holding both shipped lists, and a
	// profile that names the category rather than what is inside it.
	var categoryResponse struct {
		Category struct {
			ID    string   `json:"id"`
			Lists []string `json:"lists"`
		} `json:"category"`
	}
	created := postJSON(t, origin+"/v1/categories", `{"title":"Мои списки","lists":["alpha","beta"]}`)
	if err := json.Unmarshal(created, &categoryResponse); err != nil {
		t.Fatalf("create category=%s: %v", created, err)
	}
	categoryID := categoryResponse.Category.ID

	var profileResponse struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	body := postJSON(t, origin+"/v1/profiles", `{"name":"Дом","categories":["`+categoryID+`"]}`)
	if err := json.Unmarshal(body, &profileResponse); err != nil {
		t.Fatalf("create route=%s: %v", body, err)
	}
	profileID := profileResponse.Profile.ID
	var outputResponse struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	body = postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"keenetic"}`)
	if err := json.Unmarshal(body, &outputResponse); err != nil {
		t.Fatalf("bind output=%s: %v", body, err)
	}
	outputID := outputResponse.Output.ID
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	var buildResponse struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	body = postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	if err := json.Unmarshal(body, &buildResponse); err != nil || buildResponse.SubscriptionURL == "" {
		t.Fatalf("build=%s err=%v", body, err)
	}
	published := httpGet(t, buildResponse.SubscriptionURL, nil)
	if published.status != 200 || !strings.Contains(string(published.body), "192.0.2.10") || !strings.Contains(string(published.body), "198.51.100.20") {
		t.Fatalf("published file=%d %q", published.status, published.body)
	}

	// A second profile names one list directly. That reference refuses the
	// deletion and says which profile holds it.
	body = postJSON(t, origin+"/v1/profiles", `{"name":"Офис","lists":["alpha"]}`)
	if err := json.Unmarshal(body, &profileResponse); err != nil {
		t.Fatalf("create direct route=%s: %v", body, err)
	}
	directID := profileResponse.Profile.ID
	refused := postGuardedBody(t, origin+"/v1/lists/alpha/remove", `{}`)
	if refused.status != 409 || !strings.Contains(string(refused.body), `"error":"list in use"`) || !strings.Contains(string(refused.body), `"id":"`+directID+`","title":"Офис"`) {
		t.Fatalf("direct refusal=%d %q", refused.status, refused.body)
	}

	// beta is reached only through the category, so deleting it is allowed and
	// changes what the profile that named the category publishes next.
	if removed := postGuardedBody(t, origin+"/v1/lists/beta/remove", `{}`); removed.status != 204 || len(removed.body) != 0 {
		t.Fatalf("deletion=%d %q", removed.status, removed.body)
	}
	assertLibraryHoldsOnlyAlpha(t, origin, categoryID)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	republished := httpGet(t, buildResponse.SubscriptionURL, nil)
	if republished.status != 200 || strings.Contains(string(republished.body), "198.51.100.20") || !strings.Contains(string(republished.body), "192.0.2.10") {
		t.Fatalf("republished file=%d %q", republished.status, republished.body)
	}

	// Deleting the category is refused while a profile names it, whichever
	// disposition the request states, and an unstated disposition is refused
	// before the library is read at all.
	for _, disposition := range []string{`{"lists":"detach"}`, `{"lists":"delete"}`} {
		inUse := postGuardedBody(t, origin+"/v1/categories/"+categoryID+"/remove", disposition)
		if inUse.status != 409 || !strings.Contains(string(inUse.body), `"error":"category in use"`) || !strings.Contains(string(inUse.body), `"id":"`+profileID+`","title":"Дом"`) {
			t.Fatalf("category refusal %s=%d %q", disposition, inUse.status, inUse.body)
		}
	}
	if unstated := postGuardedBody(t, origin+"/v1/categories/"+categoryID+"/remove", `{}`); unstated.status != 422 {
		t.Fatalf("unstated disposition=%d %q", unstated.status, unstated.body)
	}

	// The shipped catalog file is untouched: the deletion is a record beside
	// the catalog, which is why the next catalog load cannot resurrect it.
	shipped, err := os.ReadFile(filepath.Join(catalog, "builtin", "beta.yaml"))
	if err != nil || !strings.Contains(string(shipped), "beta.example") {
		t.Fatalf("the shipped catalog file was edited: %q err=%v", shipped, err)
	}

	// And the deletion is stored, not remembered: a restarted process reads
	// the same catalog and still answers without the deleted list.
	stopFirst()
	restarted, _ := start()
	assertLibraryHoldsOnlyAlpha(t, restarted, categoryID)
}

func assertLibraryHoldsOnlyAlpha(t *testing.T, origin, categoryID string) {
	t.Helper()
	listing := httpGet(t, origin+"/v1/lists", nil)
	var decoded struct {
		Lists   []string `json:"lists"`
		Details []struct {
			ID string `json:"id"`
		} `json:"list_details"`
		Categories []struct {
			ID    string   `json:"id"`
			Lists []string `json:"lists"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(listing.body, &decoded); err != nil {
		t.Fatalf("catalog listing=%s: %v", listing.body, err)
	}
	if len(decoded.Lists) != 1 || decoded.Lists[0] != "alpha" {
		t.Fatalf("lists = %#v", decoded.Lists)
	}
	if len(decoded.Details) != 1 || decoded.Details[0].ID != "alpha" {
		t.Fatalf("list details = %#v", decoded.Details)
	}
	found := false
	for _, category := range decoded.Categories {
		if category.ID != categoryID {
			continue
		}
		found = true
		if len(category.Lists) != 1 || category.Lists[0] != "alpha" {
			t.Fatalf("category membership = %#v", category.Lists)
		}
	}
	if !found {
		t.Fatalf("the operator's category left the listing: %s", listing.body)
	}
}
