package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
