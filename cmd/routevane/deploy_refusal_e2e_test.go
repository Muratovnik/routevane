package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/deployers/keenetic/keenetictest"
)

// A send page asks the device what would change before it changes anything. A
// device that refuses that question must be named for what it refused, so the
// operator fixes the password or the interface instead of reading "applying did
// not finish" about a step that applied nothing.
func TestDeviceRefusalOfAPreviewOrASendNamesTheCauseAndWritesNothing(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	target, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), target, 0o600); err != nil {
		t.Fatal(err)
	}
	device := newFQDNE2EDevice()
	// The KeeneticOS 5.2 login the live refusal came from (ADR 0058).
	device.base.ndw4 = &keenetictest.NDW4{Login: deviceUser, Password: devicePassword, Salt: []byte("0123456789abcdef"), Iterations: 2, MemoryKiB: 8}
	device.groups["manual"] = fqdnE2EGroup{Description: "Operator list", Entries: []string{"manual.example"}}
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()
	data := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()}}
	origin, stop, done, stderr := startServeServer(t, catalog, data, deps)
	defer func() {
		stop()
		if code := <-done; code != 0 {
			t.Errorf("serve=%d stderr=%s", code, stderr.String())
		}
	}()
	created := postJSON(t, origin+"/v1/lists", `{"title":"Claude","domains":["claude.ai"]}`)
	var list struct {
		List struct{ ID string } `json:"list"`
	}
	if err := json.Unmarshal(created, &list); err != nil || list.List.ID == "" {
		t.Fatalf("create list=%s err=%v", created, err)
	}
	profileID, outputID, _ := createProfileOutput(t, origin, "Home AI", "keenetic-dns", list.List.ID)
	artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID

	const refusedSecret = "not-the-device-secret"
	send := func(attempt, password, interfaceName string, confirm bool) (int, map[string]any) {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"attempt_id": attempt, "confirm": confirm, "device": "http://10.255.0.1",
			"username": deviceUser, "password": password, "interface": interfaceName,
		})
		if err != nil {
			t.Fatal(err)
		}
		response := postGuardedBody(t, origin+"/v1/artifacts/"+artifactID+"/deploy", string(body))
		var decoded map[string]any
		if err := json.Unmarshal(response.body, &decoded); err != nil {
			t.Fatalf("status=%d body=%s err=%v", response.status, response.body, err)
		}
		return response.status, decoded
	}
	untouched := func(stage string) {
		t.Helper()
		groups, routes, commands := device.snapshot()
		_, restores := device.base.snapshot()
		if commands != 0 || restores != 0 || len(groups) != 1 || len(routes) != 0 {
			t.Fatalf("%s changed the device: commands=%d restores=%d groups=%v routes=%v", stage, commands, restores, groups, routes)
		}
	}

	// The device refuses the password of a read-only preview.
	status, body := send("", refusedSecret, deviceInterfaceName, false)
	if status != http.StatusUnprocessableEntity || body["error"] != "device_auth_rejected" || body["step"] != "probe" || body["field"] != "credential" {
		t.Fatalf("preview with a refused password: status=%d body=%v", status, body)
	}
	untouched("a refused preview")

	// The same refusal of a confirmed send is a failed attempt that names it,
	// and the stored attempt answers the same afterwards.
	attemptID := strings.Repeat("7", 32)
	status, body = send(attemptID, refusedSecret, deviceInterfaceName, true)
	result, _ := body["result"].(map[string]any)
	if status != http.StatusUnprocessableEntity || body["status"] != "failed" || body["error"] != "device_auth_rejected" || body["step"] != "probe" || result["applied"] != false {
		t.Fatalf("send with a refused password: status=%d body=%v", status, body)
	}
	untouched("a refused send")
	replayed := httpGet(t, origin+"/v1/deployment-attempts/"+attemptID, nil)
	if replayed.status != http.StatusOK || !strings.Contains(string(replayed.body), `"error":"device_auth_rejected"`) || !strings.Contains(string(replayed.body), `"step":"probe"`) {
		t.Fatalf("stored attempt: status=%d body=%s", replayed.status, replayed.body)
	}

	// An interface the device does not have is named as that, not as a login.
	status, body = send("", devicePassword, "Wireguard9", false)
	if status != http.StatusUnprocessableEntity || body["error"] != "device_interface_missing" || body["step"] != "probe" || body["field"] != "interface" {
		t.Fatalf("preview to a missing interface: status=%d body=%v", status, body)
	}
	untouched("a preview to a missing interface")

	// The right password and interface preview normally, so the refusals above
	// were about what they name.
	if status, body = send("", devicePassword, deviceInterfaceName, false); status != http.StatusOK || body["confirmed"] != false {
		t.Fatalf("preview with the device password: status=%d body=%v", status, body)
	}
	untouched("an accepted preview")

	logs := stderr.String()
	for _, want := range []string{`"code":"device_auth_rejected"`, `"code":"device_interface_missing"`, `"step":"probe"`, `"route":"artifacts.deploy"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("the refusal log lacks %s: %s", want, logs)
		}
	}
	for _, secret := range []string{refusedSecret, devicePassword, "10.255.0.1", "authentication status"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log carries %q: %s", secret, logs)
		}
	}
}
