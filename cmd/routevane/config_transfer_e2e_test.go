package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfigTransferLegacyPrioritySurvivesApplyAndReopen(t *testing.T) {
	for _, version := range []string{"config-transfer-v1.2", "config-transfer-v1.3", "config-transfer-v1.4"} {
		t.Run(version, func(t *testing.T) {
			catalog := writeAlphaBetaCatalog(t)
			data := filepath.Join(t.TempDir(), "data")
			deps := runtimeDeps{Now: func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }}
			func() {
				origin, cancel, done, stderr := startServeServer(t, catalog, data, deps)
				defer func() {
					cancel()
					if code := <-done; code != 0 {
						t.Fatalf("first server code=%d stderr=%s", code, stderr.String())
					}
				}()
				exported := httpGet(t, origin+"/v1/config-transfer/export", nil)
				if exported.status != http.StatusOK {
					t.Fatalf("export status=%d body=%s", exported.status, exported.body)
				}
				var document map[string]any
				if err := json.Unmarshal(exported.body, &document); err != nil {
					t.Fatal(err)
				}
				settings, ok := document["settings"].(map[string]any)
				if !ok {
					t.Fatalf("exported settings=%#v", document["settings"])
				}
				settings["default_priority"] = []string{"beta", "alpha"}
				document["version"] = version
				if version != "config-transfer-v1.4" {
					document["custom_services"] = document["custom_lists"]
					document["routes"] = document["profiles"]
					delete(document, "custom_lists")
					delete(document, "profiles")
				}
				payload, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				previewBody := postJSON(t, origin+"/v1/config-transfer/preview", string(payload))
				var preview struct {
					Digest string `json:"digest"`
				}
				if err := json.Unmarshal(previewBody, &preview); err != nil || preview.Digest == "" {
					t.Fatalf("preview=%s err=%v", previewBody, err)
				}
				postConfigTransfer(t, origin+"/v1/config-transfer/apply", string(payload), preview.Digest)
			}()

			origin, cancel, done, stderr := startServeServer(t, catalog, data, deps)
			defer func() {
				cancel()
				if code := <-done; code != 0 {
					t.Fatalf("reopened server code=%d stderr=%s", code, stderr.String())
				}
			}()
			want := []string{"beta", "alpha"}
			if version == "config-transfer-v1.2" {
				// The priority field was introduced in v1.3. Older documents
				// continue to use the canonical catalog fallback.
				want = []string{"alpha", "beta"}
			}
			response := httpGet(t, origin+"/v1/lists", nil)
			var library struct {
				Priority []string `json:"default_priority"`
			}
			if err := json.Unmarshal(response.body, &library); err != nil || response.status != http.StatusOK || !reflect.DeepEqual(library.Priority, want) {
				t.Fatalf("reopened library status=%d priority=%v want=%v err=%v", response.status, library.Priority, want, err)
			}
			created := postJSON(t, origin+"/v1/profiles", `{"name":"After import","lists":["alpha","beta"]}`)
			var profile struct {
				Profile struct {
					Priority []string `json:"priority"`
				} `json:"profile"`
			}
			if err := json.Unmarshal(created, &profile); err != nil || !reflect.DeepEqual(profile.Profile.Priority, want) {
				t.Fatalf("new profile priority=%v want=%v body=%s err=%v", profile.Profile.Priority, want, created, err)
			}
		})
	}
}

// TestConfigTransferRoundTripsThroughServe starts the command boundary and
// proves its empty-state export can be previewed and applied to a freshly
// initialized persistent store. The HTTP/application/SQLite tests cover the
// richer fixture; this is the user-visible command lifecycle oracle.
func TestConfigTransferRoundTripsThroughServe(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	origin, cancel, done, stderr := startServeServer(t, catalog, data, runtimeDeps{Now: func() time.Time {
		return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	}})
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	exported := httpGet(t, origin+"/v1/config-transfer/export", nil)
	if exported.status != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.status, exported.body)
	}
	previewBody := postJSON(t, origin+"/v1/config-transfer/preview", string(exported.body))
	var preview struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(previewBody, &preview); err != nil || preview.Digest == "" {
		t.Fatalf("preview=%s err=%v", previewBody, err)
	}
	applyBody := postConfigTransfer(t, origin+"/v1/config-transfer/apply", string(exported.body), preview.Digest)
	var applied struct {
		Applied bool `json:"applied"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil || !applied.Applied {
		t.Fatalf("apply=%s err=%v", applyBody, err)
	}
}

func postConfigTransfer(t *testing.T, endpoint, body, digest string) []byte {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	request.Header.Set("X-Routevane-Transfer-Digest", digest)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("POST %s status=%d body=%s", endpoint, response.StatusCode, payload)
	}
	return payload
}
