package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServeCustomListOwnsItsDomainsEndToEnd is the visible outcome of the
// operator-defined catalog entry: a list created over HTTP joins the
// catalog, enters a list, and its domains reach the published file — and an
// edit reaches subscribers on the next refresh-and-rebuild without changing
// the subscription URL.
func TestServeCustomListOwnsItsDomainsEndToEnd(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	// The BAT target cannot carry a name; the FQDN-group target is the format
	// an operator-defined domain list is actually for.
	dnsTarget, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), dnsTarget, 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	created := postJSON(t, origin+"/v1/lists", `{"title":"Мои сайты","domains":["MY.example.","corp.example"]}`)
	var listResponse struct {
		List struct {
			ID      string   `json:"id"`
			Title   string   `json:"title"`
			Domains []string `json:"domains"`
		} `json:"list"`
	}
	if err := json.Unmarshal(created, &listResponse); err != nil {
		t.Fatal(err)
	}
	listID := listResponse.List.ID
	if !strings.HasPrefix(listID, "custom-") || listResponse.List.Title != "Мои сайты" {
		t.Fatalf("created = %s", created)
	}
	if want := []string{"corp.example", "my.example"}; len(listResponse.List.Domains) != 2 || listResponse.List.Domains[0] != want[0] || listResponse.List.Domains[1] != want[1] {
		t.Fatalf("domains = %#v", listResponse.List.Domains)
	}

	listing := httpGet(t, origin+"/v1/lists", nil)
	var catalogResponse struct {
		Details []struct {
			ID     string `json:"id"`
			Custom bool   `json:"custom"`
		} `json:"list_details"`
	}
	if err := json.Unmarshal(listing.body, &catalogResponse); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, detail := range catalogResponse.Details {
		if detail.ID == listID {
			found = detail.Custom
		} else if detail.Custom {
			t.Fatalf("shipped list %q reported as custom", detail.ID)
		}
	}
	if !found {
		t.Fatalf("catalog listing lacks the custom list: %s", listing.body)
	}

	// A mixed list is the regression oracle: the planner accepts exactly one
	// catalog revision per plan, so a custom list must compose with shipped
	// lists rather than only with itself.
	profileID, outputID, subscriptionURL := createProfileOutput(t, origin, "Свои сайты", "keenetic-dns", listID, "alpha")
	first := httpGet(t, subscriptionURL, nil)
	if first.status != 200 || !strings.Contains(string(first.body), "my.example") || !strings.Contains(string(first.body), "corp.example") || !strings.Contains(string(first.body), "alpha.example") {
		t.Fatalf("published file=%d %q", first.status, first.body)
	}

	postJSON(t, origin+"/v1/lists/"+listID+"/update", `{"title":"Мои сайты","domains":["renamed.example"]}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	second := httpGet(t, subscriptionURL, nil)
	if second.status != 200 || !strings.Contains(string(second.body), "renamed.example") || strings.Contains(string(second.body), "my.example") {
		t.Fatalf("republished file=%d %q", second.status, second.body)
	}

	// The destination verdict switches one specific row off wherever it came
	// from: the static suffix leaves the file while the observed address stays.
	postJSON(t, origin+"/v1/lists/alpha/domains", `{"values":["alpha.example"],"verdict":"exclude"}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	third := httpGet(t, subscriptionURL, nil)
	if third.status != 200 || strings.Contains(string(third.body), "alpha.example") || !strings.Contains(string(third.body), "192.0.2.10") || !strings.Contains(string(third.body), "renamed.example") {
		t.Fatalf("tuned file=%d %q", third.status, third.body)
	}

	// Switching the catalog source off removes its observed material from the
	// next build without deleting anything: the address leaves, the restored
	// static suffix stays.
	postJSON(t, origin+"/v1/lists/alpha/domains", `{"values":["alpha.example"],"verdict":"auto"}`)
	postJSON(t, origin+"/v1/lists/alpha/sources/dns-main/update", `{"enabled":false}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	switchedOff := httpGet(t, subscriptionURL, nil)
	if switchedOff.status != 200 || strings.Contains(string(switchedOff.body), "192.0.2.10") || !strings.Contains(string(switchedOff.body), "alpha.example") {
		t.Fatalf("source-off file=%d %q", switchedOff.status, switchedOff.body)
	}
	postJSON(t, origin+"/v1/lists/alpha/domains", `{"values":["alpha.example"],"verdict":"exclude"}`)

	// The contents table tells the same story the build reads: the switched
	// source is off, the excluded domain is off, and re-enabling both brings
	// the material back on the next rebuild.
	contents := httpGet(t, origin+"/v1/lists/alpha/contents", nil)
	var contentsResponse struct {
		Rows []struct {
			Value   string `json:"value"`
			Kind    string `json:"kind"`
			Origin  string `json:"origin"`
			Enabled bool   `json:"enabled"`
		} `json:"rows"`
		Sources []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"sources"`
		Observed bool `json:"observed"`
	}
	if err := json.Unmarshal(contents.body, &contentsResponse); err != nil {
		t.Fatal(err)
	}
	if !contentsResponse.Observed || len(contentsResponse.Sources) != 1 || contentsResponse.Sources[0].ID != "dns-main" || contentsResponse.Sources[0].Enabled {
		t.Fatalf("contents sources = %s", contents.body)
	}
	foundExcluded := false
	for _, row := range contentsResponse.Rows {
		if row.Value == "alpha.example" {
			foundExcluded = !row.Enabled && row.Kind == "domain"
		}
	}
	if !foundExcluded {
		t.Fatalf("contents rows = %s", contents.body)
	}

	postJSON(t, origin+"/v1/lists/alpha/sources/dns-main/update", `{"enabled":true}`)
	postJSON(t, origin+"/v1/lists/alpha/domains", `{"values":["alpha.example"],"verdict":"auto"}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	fourth := httpGet(t, subscriptionURL, nil)
	// The domain-capable target resolves the restored suffix itself, so the
	// observed address is legitimately not repeated beside it (ADR 0017).
	if fourth.status != 200 || !strings.Contains(string(fourth.body), "alpha.example") || !strings.Contains(string(fourth.body), "renamed.example") {
		t.Fatalf("restored file=%d %q", fourth.status, fourth.body)
	}

	// A destination is not only a name. The operator states an address, the
	// contents table calls it one, and the address-only target carries it into
	// the published file beside the observed material.
	postJSON(t, origin+"/v1/lists/alpha/domains", `{"values":["198.51.100.7"],"verdict":"include"}`)
	addressContents := httpGet(t, origin+"/v1/lists/alpha/contents", nil)
	if err := json.Unmarshal(addressContents.body, &contentsResponse); err != nil {
		t.Fatal(err)
	}
	statedAddress := false
	for _, row := range contentsResponse.Rows {
		if row.Value == "198.51.100.7" {
			statedAddress = row.Kind == "ip" && row.Origin == "manual" && row.Enabled
		}
	}
	if !statedAddress {
		t.Fatalf("contents rows = %s", addressContents.body)
	}
	_, _, addressSubscription := createProfileOutput(t, origin, "Адреса", "keenetic", "alpha")
	addressFile := httpGet(t, addressSubscription, nil)
	if addressFile.status != 200 || !strings.Contains(string(addressFile.body), "route ADD 198.51.100.7") {
		t.Fatalf("address file=%d %q", addressFile.status, addressFile.body)
	}
}
