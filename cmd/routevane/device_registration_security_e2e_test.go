package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Direct API callers cross the same deployer-owned persistence boundary as the
// browser form. Neither a secret-bearing URL nor a destination outside the
// supported local transport may reach SQLite.
func TestDeviceRegistrationRefusesUnsafePersistedDestinationsEndToEnd(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{Now: func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }}
	origin, cancel, done, stderr := startServeServer(t, catalog, data, deps)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	userInfoSecret := "embedded-password"
	userInfoURL := (&url.URL{Scheme: "http", Host: "192.168.1.1", User: url.UserPassword("admin", userInfoSecret)}).String()
	cases := []struct {
		name, target, address, account, interfaceName, secret string
	}{
		{"keenetic userinfo", "keenetic", userInfoURL, "admin", "Wireguard0", userInfoSecret},
		{"keenetic query", "keenetic", "http://192.168.1.1?password=query-password", "admin", "Wireguard0", "query-password"},
		{"keenetic fragment", "keenetic", "http://192.168.1.1/#fragment-password", "admin", "Wireguard0", "fragment-password"},
		{"keenetic public address", "keenetic", "https://203.0.113.10", "admin", "Wireguard0", "203.0.113.10"},
		{"sing-box authority", "singbox", "file://server/share/config.json", "", "", "server"},
		{"sing-box query", "singbox", "file:///C:/sing-box/config.json?token=file-password", "", "", "file-password"},
		{"sing-box fragment", "singbox", "file:///C:/sing-box/config.json#file-fragment", "", "", "file-fragment"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := `{"target_id":` + mustQuote(t, test.target) + `,"name":"Unsafe","address":` + mustQuote(t, test.address) + `,"account":` + mustQuote(t, test.account) + `,"interface":` + mustQuote(t, test.interfaceName) + `}`
			response := postGuardedBody(t, origin+"/v1/devices", body)
			if response.status != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", response.status, response.body)
			}
			if strings.Contains(string(response.body), test.secret) {
				t.Fatalf("response leaked secret-bearing destination: %s", response.body)
			}
		})
	}

	listing := httpGet(t, origin+"/v1/devices", nil)
	var decoded struct {
		Devices []struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"devices"`
	}
	if listing.status != http.StatusOK || json.Unmarshal(listing.body, &decoded) != nil || len(decoded.Devices) != 0 {
		t.Fatalf("unsafe devices reached the registry: status=%d body=%s", listing.status, listing.body)
	}

	createdBody := postJSON(t, origin+"/v1/devices", `{"target_id":"keenetic","name":"Router","address":"http://192.168.1.1","account":"admin","interface":"Wireguard0"}`)
	var created struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	if err := json.Unmarshal(createdBody, &created); err != nil || len(created.Device.ID) != 32 {
		t.Fatalf("valid device registration=%s err=%v", createdBody, err)
	}
	const updateSecret = "update-password"
	unsafeUpdate := postGuardedBody(t, origin+"/v1/devices/"+created.Device.ID+"/update", `{"name":"Router","address":"http://192.168.1.1?password=`+updateSecret+`","account":"admin","interface":"Wireguard0"}`)
	if unsafeUpdate.status != http.StatusUnprocessableEntity || strings.Contains(string(unsafeUpdate.body), updateSecret) {
		t.Fatalf("unsafe update status=%d body=%s", unsafeUpdate.status, unsafeUpdate.body)
	}
	listing = httpGet(t, origin+"/v1/devices", nil)
	decoded.Devices = nil
	if listing.status != http.StatusOK || json.Unmarshal(listing.body, &decoded) != nil || len(decoded.Devices) != 1 || decoded.Devices[0].Address != "http://192.168.1.1" || strings.Contains(string(listing.body), updateSecret) {
		t.Fatalf("unsafe update changed persisted metadata: status=%d body=%s", listing.status, listing.body)
	}
}
