package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pocketTargetYAML is a second device on the same renderer whose rule budget is
// one. It exists so the end-to-end proof covers the case the endpoint was built
// for — a composition the device cannot hold — without needing a catalog large
// enough to overflow a real Keenetic. It also carries the English pair, which
// the shipped keenetic fixture deliberately does not, so one listing shows both
// a translated and an untranslated target.
const pocketTargetYAML = `id: pocket
profile_key: keenetic-bat-ipv4-v1
title: Карманный роутер
title_en: Pocket router
kind: router
renderer: keenetic-route-bat
constraints:
  supports_domain_exact: false
  supports_domain_suffix: false
  supports_dynamic_dns_set: false
  supports_ipv4: true
  supports_ipv6: false
  supports_prefixes: true
  max_rules: 1
  max_artifact_size: 131072
renderer_options: []
manual_installation_hint: Одно правило и ни одним больше.
manual_installation_hint_en: One rule and not a single one more.
`

// A composition is forecast over real HTTP, against a real store and a real
// catalog, before the profile that would carry it exists. The user-visible result
// is the pair of numbers a screen refuses a device on: what the device holds
// and what the composition would need. The device that cannot hold it says so
// here instead of at the first build.
func TestServeForecastsACompositionBeforeItIsCreated(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	if err := os.WriteFile(filepath.Join(catalog, "targets", "pocket.yaml"), []byte(pocketTargetYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	// The catalog listing carries the English name and instruction where the
	// catalog has them, and omits them where it does not, so a browser falls
	// back to the catalog's own language instead of rendering an empty name.
	targets := httpGet(t, origin+"/v1/targets", nil)
	if targets.status != http.StatusOK {
		t.Fatalf("targets status=%d body=%s", targets.status, targets.body)
	}
	var listing struct {
		Targets []struct {
			ID                       string `json:"id"`
			Title                    string `json:"title"`
			TitleEN                  string `json:"title_en"`
			ManualInstallationHint   string `json:"manual_installation_hint"`
			ManualInstallationHintEN string `json:"manual_installation_hint_en"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(targets.body, &listing); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, target := range listing.Targets {
		switch target.ID {
		case "pocket":
			seen++
			if target.Title != "Карманный роутер" || target.TitleEN != "Pocket router" || target.ManualInstallationHintEN != "One rule and not a single one more." {
				t.Fatalf("translated target = %#v", target)
			}
		case "keenetic":
			seen++
			// This fixture names no title at all, so the id is the honest
			// fallback and there is no English name to invent from it.
			if target.Title != "keenetic" || target.TitleEN != "" || target.ManualInstallationHintEN != "" {
				t.Fatalf("untranslated target = %#v", target)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("targets listing = %s", targets.body)
	}

	// One profile exists and has been observed, so the forecast has real
	// observations to plan from. The composition it is asked about is a
	// different one, and it is never created.
	created := postJSON(t, origin+"/v1/profiles", `{"name":"Наблюдение","lists":["alpha","beta"]}`)
	var profileResponse struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(created, &profileResponse); err != nil {
		t.Fatal(err)
	}
	postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/refresh", `{}`)

	forecast := postJSON(t, origin+"/v1/profiles/preview", `{"lists":["alpha","beta"],"targets":["pocket","keenetic"]}`)
	const want = `{"targets":[` +
		`{"target_id":"keenetic","maximum_rules":1024,"projected_rules":2,"fits":true,"per_list":[{"list_id":"alpha","rules":1},{"list_id":"beta","rules":1}],"overlaps":{"items":[],"truncated":false,"summary":[{"list_id":"alpha","overlaps":[]},{"list_id":"beta","overlaps":[]}]}}` +
		`,{"target_id":"pocket","maximum_rules":1,"projected_rules":2,"fits":false,"per_list":[{"list_id":"alpha","rules":1},{"list_id":"beta","rules":1}],"overlaps":{"items":[],"truncated":false,"summary":[{"list_id":"alpha","overlaps":[]},{"list_id":"beta","overlaps":[]}]}}` +
		`]}` + "\n"
	if string(forecast) != want {
		t.Fatalf("forecast = %s\nwant     = %s", forecast, want)
	}

	// Nothing was created by asking. The store still holds the one observed
	// profile, with no output and no attempt behind it.
	profiles := httpGet(t, origin+"/v1/profiles", nil)
	var library struct {
		Profiles []struct {
			ID      string            `json:"id"`
			Name    string            `json:"name"`
			Outputs []json.RawMessage `json:"outputs"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(profiles.body, &library); err != nil {
		t.Fatal(err)
	}
	if len(library.Profiles) != 1 || library.Profiles[0].ID != profileResponse.Profile.ID || len(library.Profiles[0].Outputs) != 0 {
		t.Fatalf("the forecast left state behind: %s", profiles.body)
	}

	// A device the catalog does not carry is refused rather than dropped from
	// the answer: a caller that asked about it and got silence would read the
	// silence as a fit.
	status, body := postForecast(t, origin, `{"lists":["alpha"],"targets":["absent-device"]}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown target status=%d body=%s", status, body)
	}
	// A composition that resolves to nothing is refused for the same reason:
	// there is no such profile to forecast.
	status, body = postForecast(t, origin, `{}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("empty composition status=%d body=%s", status, body)
	}
}

// postForecast posts to the preview route and returns the outcome instead of
// failing on it, because the refusals are part of what this route promises.
func postForecast(t *testing.T, origin, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, origin+"/v1/profiles/preview", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(payload)
}
