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

// TestKeepAddressesBesideDomainsReachesTheRouterFile is the visible outcome of
// ADR 0057: a domain-capable format leaves out the address its list's name
// resolves to until the operator asks that format to keep it; the setting
// survives a restart, applies on the next build, and a format that cannot carry
// names refuses it.
func TestKeepAddressesBesideDomainsReachesTheRouterFile(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	dnsTarget, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), dnsTarget, 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	// alphaBetaResolver answers alpha.example with this address.
	const resolved = "192.0.2.10"

	serve := func(body func(origin string)) {
		t.Helper()
		origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
		defer func() {
			cancel()
			if code := <-done; code != 0 {
				t.Errorf("server code=%d stderr=%s", code, stderr.String())
			}
		}()
		body(origin)
	}
	// The subscription path outlives the server; its origin does not.
	var subscriptionPath string
	published := func(origin string) string {
		t.Helper()
		file := httpGet(t, origin+subscriptionPath, nil)
		if file.status != http.StatusOK || !strings.Contains(string(file.body), "alpha.example") {
			t.Fatalf("published file=%d %q", file.status, file.body)
		}
		return string(file.body)
	}

	var profileID, outputID string
	serve(func(origin string) {
		var subscriptionURL string
		profileID, outputID, subscriptionURL = createProfileOutput(t, origin, "Адреса", "keenetic-dns", "alpha")
		subscriptionPath = strings.TrimPrefix(subscriptionURL, origin)
		if file := published(origin); strings.Contains(file, resolved) {
			t.Fatalf("default build kept the address its name covers: %q", file)
		}
		saved := putGuardedBody(t, origin+"/v1/outputs/"+outputID+"/keep-addresses", `{"keep_addresses":true}`)
		if saved.status != http.StatusOK || !strings.Contains(string(saved.body), `"keep_addresses":true`) {
			t.Fatalf("switch on status=%d body=%s", saved.status, saved.body)
		}
		// Saving alone publishes nothing: the file changes on the next build.
		if file := published(origin); strings.Contains(file, resolved) {
			t.Fatalf("saving the setting changed the published file: %q", file)
		}

		// A format that carries no names has nothing to keep the address beside.
		var bat struct {
			Output struct {
				ID string `json:"id"`
			} `json:"output"`
		}
		if err := json.Unmarshal(postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"keenetic"}`), &bat); err != nil {
			t.Fatal(err)
		}
		refused := putGuardedBody(t, origin+"/v1/outputs/"+bat.Output.ID+"/keep-addresses", `{"keep_addresses":true}`)
		if refused.status != http.StatusBadRequest || !strings.Contains(string(refused.body), "invalid_keep_addresses") {
			t.Fatalf("BAT format status=%d body=%s", refused.status, refused.body)
		}
	})

	serve(func(origin string) {
		var detail struct {
			Outputs []struct {
				ID                     string `json:"id"`
				TargetID               string `json:"target_id"`
				KeepAddresses          bool   `json:"keep_addresses"`
				KeepAddressesAvailable bool   `json:"keep_addresses_available"`
			} `json:"outputs"`
		}
		page := httpGet(t, origin+"/v1/profiles/"+profileID, nil)
		if err := json.Unmarshal(page.body, &detail); err != nil {
			t.Fatalf("profile page=%s err=%v", page.body, err)
		}
		for _, card := range detail.Outputs {
			want := card.ID == outputID
			if card.KeepAddresses != want || card.KeepAddressesAvailable != want {
				t.Fatalf("after restart card %s (%s) keep=%v available=%v", card.ID, card.TargetID, card.KeepAddresses, card.KeepAddressesAvailable)
			}
		}
		postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		if file := published(origin); !strings.Contains(file, resolved) {
			t.Fatalf("build with the setting on left out the address: %q", file)
		}

		putGuardedBody(t, origin+"/v1/outputs/"+outputID+"/keep-addresses", `{"keep_addresses":false}`)
		postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		if file := published(origin); strings.Contains(file, resolved) {
			t.Fatalf("switching off did not drop the address again: %q", file)
		}
	})
}

func putGuardedBody(t *testing.T, endpoint, body string) httpResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, endpoint, strings.NewReader(body))
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
	received, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResponse{status: response.StatusCode, body: received}
}
