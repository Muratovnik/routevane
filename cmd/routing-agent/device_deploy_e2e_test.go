package main

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

const (
	deviceUser          = "admin"
	devicePassword      = "device-secret-value"
	deviceRealm         = "Keenetic"
	deviceChallenge     = "fedcba9876543210"
	deviceInterfaceName = "Wireguard0"
)

// fakeKeeneticDevice answers the RCI surface the deployer uses and records the
// state a deployment leaves behind.
type fakeKeeneticDevice struct {
	mu            sync.Mutex
	authenticated bool
	routes        map[string]string
	config        []byte
	restores      int
	drift         bool
	bodies        []string
}

func newFakeKeeneticDevice() *fakeKeeneticDevice {
	return &fakeKeeneticDevice{routes: map[string]string{}, config: []byte("! startup-config\nsystem hostname router\n")}
}

func (d *fakeKeeneticDevice) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := ""
		if r.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			body = string(raw)
		}
		d.mu.Lock()
		d.bodies = append(d.bodies, body)
		authenticated := d.authenticated
		d.mu.Unlock()

		switch {
		case r.URL.Path == "/auth" && r.Method == http.MethodGet:
			if authenticated {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("X-NDM-Realm", deviceRealm)
			w.Header().Set("X-NDM-Challenge", deviceChallenge)
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/auth" && r.Method == http.MethodPost:
			inner := md5.Sum([]byte(deviceUser + ":" + deviceRealm + ":" + devicePassword)) // #nosec G401 -- reproduces the device's documented challenge scheme in a test.
			outer := sha256.Sum256([]byte(deviceChallenge + hex.EncodeToString(inner[:])))
			if !strings.Contains(body, hex.EncodeToString(outer[:])) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			d.mu.Lock()
			d.authenticated = true
			d.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "ndm_session", Value: "opaque"})
			w.WriteHeader(http.StatusOK)
		case !authenticated:
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/rci/show/version":
			writeDeviceJSON(w, map[string]any{"release": "5.1.2", "model": "Giga", "manufacturer": "Keenetic"})
		case r.URL.Path == "/rci/show/interface":
			writeDeviceJSON(w, map[string]any{deviceInterfaceName: map[string]any{"state": "up"}})
		case r.URL.Path == "/rci/show/sc/ip/route":
			d.mu.Lock()
			routes := make([]map[string]any, 0, len(d.routes))
			for key, attached := range d.routes {
				parts := strings.SplitN(key, "/", 2)
				routes = append(routes, map[string]any{"network": parts[0], "mask": parts[1], "interface": attached})
			}
			d.mu.Unlock()
			writeDeviceJSON(w, map[string]any{"route": routes})
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet:
			d.mu.Lock()
			config := append([]byte(nil), d.config...)
			d.mu.Unlock()
			_, _ = w.Write(config)
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodPost:
			d.mu.Lock()
			d.config = []byte(body)
			d.restores++
			d.routes = map[string]string{}
			d.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/rci/system/configuration/save":
			writeDeviceJSON(w, map[string]any{"status": []map[string]any{{"status": "message", "code": "saved"}}})
		case r.URL.Path == "/rci/":
			d.apply(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (d *fakeKeeneticDevice) apply(w http.ResponseWriter, body string) {
	var commands []map[string]any
	if err := json.Unmarshal([]byte(body), &commands); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	answers := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		ip, _ := command["ip"].(map[string]any)
		route, _ := ip["route"].(map[string]any)
		network, _ := route["network"].(string)
		mask, _ := route["mask"].(string)
		attached, _ := route["interface"].(string)
		_, remove := route["no"]
		if network == "" || mask == "" || attached == "" {
			answers = append(answers, map[string]any{"status": []map[string]any{{"status": "error", "code": "unknown"}}})
			continue
		}
		if remove {
			delete(d.routes, network+"/"+mask)
		} else {
			d.routes[network+"/"+mask] = attached
		}
		answers = append(answers, map[string]any{"status": []map[string]any{{"status": "message", "code": "route.applied"}}})
	}
	if d.drift {
		for key := range d.routes {
			delete(d.routes, key)
			break
		}
	}
	writeDeviceJSON(w, answers)
}

func (d *fakeKeeneticDevice) snapshot() (routes int, restores int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.routes), d.restores
}

func (d *fakeKeeneticDevice) sentBodies() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.bodies...)
}

func writeDeviceJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	payload, _ := json.Marshal(value)
	_, _ = w.Write(payload)
}

// redirectDialer satisfies every Dialer interface in this package (they all
// declare the same single DialContext method): it connects to a fixed target
// address regardless of what address the caller asked for.
type redirectDialer struct{ target string }

func (d redirectDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, d.target)
}

func TestAppliesAPublishedArtifactToADeviceAndRollsBackAFailedVerification(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})

	device := newFakeKeeneticDevice()
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()

	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	listID, outputID, _ := createListOutput(t, origin, "Видео и общение", "keenetic", "youtube", "discord")
	build := refreshAndBuild(t, origin, listID, outputID)
	artifactID := build.Artifact.ID
	if artifactID == "" {
		t.Fatalf("build = %#v", build)
	}

	deps := runtimeDeps{
		Resolver:     resolver,
		DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()},
		Now:          func() time.Time { return now },
		Context:      context.Background(),
	}
	baseArgs := []string{"deploy", "--artifact", artifactID, "--target", "keenetic", "--device", "http://192.168.1.1", "--user", deviceUser, "--interface", deviceInterfaceName, "--catalog-dir", catalog, "--data-dir", data}

	// The unconfirmed call changes nothing on the device.
	preview := deployViaCLI(t, deps, baseArgs, "")
	if preview.Confirmed || preview.Hint == "" {
		t.Fatalf("preview = %#v", preview)
	}
	if routes, _ := device.snapshot(); routes != 0 {
		t.Fatalf("an unconfirmed deployment reached the device: %d routes", routes)
	}

	// A confirmed deployment without the credential variable is refused.
	stdout, deployStderr := &syncBuffer{}, &syncBuffer{}
	t.Setenv(DevicePasswordVariable, "")
	if code := runWithDeps(stdout, deployStderr, append(append([]string(nil), baseArgs...), "--confirm"), deps); code == 0 {
		t.Fatalf("a deployment without a credential was accepted: %s", stdout.String())
	}

	applied := deployViaCLI(t, deps, append(append([]string(nil), baseArgs...), "--confirm"), devicePassword)
	if !applied.Applied || applied.RolledBack {
		t.Fatalf("result = %#v", applied)
	}
	if applied.Device.FirmwareVersion != "5.1.2" || applied.Device.ProfileKey == "" {
		t.Fatalf("device = %#v", applied.Device)
	}
	if applied.Backup.Hash == "" || applied.Backup.SizeBytes == 0 {
		t.Fatalf("backup = %#v", applied.Backup)
	}
	steps := make([]string, 0, len(applied.Events))
	for _, event := range applied.Events {
		steps = append(steps, event.Step+":"+event.Outcome)
	}
	if strings.Join(steps, ",") != "probe:success,backup:success,deploy:success,verify:success" {
		t.Fatalf("audit = %v", steps)
	}
	if routes, _ := device.snapshot(); routes != 2 {
		t.Fatalf("the device holds %d routes", routes)
	}
	// The backup is on disk where the audit record says it is.
	backupPath := filepath.Join(data, filepath.FromSlash(applied.Backup.Path))
	if info, err := os.Stat(backupPath); err != nil || info.Size() != applied.Backup.SizeBytes {
		t.Fatalf("backup file = %v %v", info, err)
	}

	// Applying the same artifact again is idempotent.
	again := deployViaCLI(t, deps, append(append([]string(nil), baseArgs...), "--confirm"), devicePassword)
	if !again.Applied || again.RolledBack {
		t.Fatalf("result = %#v", again)
	}
	if routes, restores := device.snapshot(); routes != 2 || restores != 0 {
		t.Fatalf("a repeated deployment changed the device: routes=%d restores=%d", routes, restores)
	}

	// A device that silently loses a route fails verification and is rolled back.
	// One route is dropped first so the deployment has work to do; the drift then
	// makes the device answer with less than it was told to install, which is
	// what a partial write looks like from outside.
	device.mu.Lock()
	for key := range device.routes {
		delete(device.routes, key)
		break
	}
	device.drift = true
	device.mu.Unlock()
	stdout, deployStderr = &syncBuffer{}, &syncBuffer{}
	t.Setenv(DevicePasswordVariable, devicePassword)
	code := runWithDeps(stdout, deployStderr, append(append([]string(nil), baseArgs...), "--confirm"), deps)
	if code == 0 {
		t.Fatalf("a failed verification was reported as success: %s", stdout.String())
	}
	var rolled deployReport
	if err := json.Unmarshal([]byte(stdout.String()), &rolled); err != nil {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
	if rolled.Applied || !rolled.RolledBack {
		t.Fatalf("result = %#v", rolled)
	}
	if _, restores := device.snapshot(); restores != 1 {
		t.Fatalf("a failed verification did not restore the backup: restores=%d", restores)
	}

	// No credential appears in the audit record, the log, or anything the device
	// was told beyond the challenge proof.
	for _, text := range []string{stdout.String(), deployStderr.String()} {
		if strings.Contains(text, devicePassword) {
			t.Fatalf("the credential leaked: %s", text)
		}
	}
	for _, body := range device.sentBodies() {
		if strings.Contains(body, devicePassword) {
			t.Fatalf("the credential was sent to the device in cleartext: %s", body)
		}
	}
}

func deployViaCLI(t *testing.T, deps runtimeDeps, args []string, password string) deployReport {
	t.Helper()
	t.Setenv(DevicePasswordVariable, password)
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("deploy %v failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
	var report deployReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
	return report
}

// TestKeepsTheRendererAndTheDeployerInSeparatePackages is a structural
// assertion: the deployment path installs bytes the publication path already
// validated, and it never renders.
func TestKeepsTheRendererAndTheDeployerInSeparatePackages(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "deployers", "keenetic", "keenetic.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"keenetic.Render(", "keenetic.ProjectedRuleCount(", "planner.", "domain.RoutingPlan"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the deployer referenced %q; rendering and planning belong to their own packages", forbidden)
		}
	}
	// It does use the renderer's validator, which is what stops a foreign or
	// corrupted file from reaching a device.
	if !strings.Contains(text, "keenetic.Parse(") {
		t.Fatal("the deployer must validate the artifact with the renderer's own parser")
	}
	var _ application.Deployer = (application.Deployer)(nil)
}
