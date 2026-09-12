package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
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
	mu                  sync.Mutex
	authenticated       bool
	routes              map[string]string
	comments            map[string]string
	config              []byte
	restores            int
	applies             int
	bodies              []string
	backupRoutes        map[string]string
	backupComments      map[string]string
	partialState        map[string]string
	failAfterAdd        bool
	omitRouteComments   bool
	cancelApply         context.CancelFunc
	waitForCallerCancel bool
	callerCancelled     chan struct{}
	rollbackStarted     chan struct{}
	releaseRollback     <-chan struct{}
}

func newFakeKeeneticDevice() *fakeKeeneticDevice {
	return &fakeKeeneticDevice{routes: map[string]string{}, comments: map[string]string{}, config: []byte("! startup-config\nsystem hostname router\n")}
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
				routes = append(routes, map[string]any{"network": parts[0], "mask": parts[1], "interface": attached, "comment": d.comments[key]})
			}
			d.mu.Unlock()
			writeDeviceJSON(w, map[string]any{"route": routes})
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet:
			d.mu.Lock()
			// Give each captured state distinct configuration bytes. Restore below
			// accepts those exact bytes, never the union of before and partial apply.
			var snapshot strings.Builder
			snapshot.WriteString("! startup-config\nsystem hostname router\n")
			for _, key := range slices.Sorted(maps.Keys(d.routes)) {
				parts := strings.SplitN(key, "/", 2)
				snapshot.WriteString("ip route " + parts[0] + " " + parts[1] + " " + d.routes[key] + " " + d.comments[key] + "\n")
			}
			d.config = []byte(snapshot.String())
			d.backupRoutes = maps.Clone(d.routes)
			d.backupComments = maps.Clone(d.comments)
			config := slices.Clone(d.config)
			d.mu.Unlock()
			_, _ = w.Write(config)
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodPost:
			d.mu.Lock()
			if body != string(d.config) {
				d.mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			d.restores++
			d.routes = maps.Clone(d.backupRoutes)
			d.comments = maps.Clone(d.backupComments)
			d.mu.Unlock()
			if d.rollbackStarted != nil {
				close(d.rollbackStarted)
			}
			if d.releaseRollback != nil {
				<-d.releaseRollback
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/rci/system/configuration/save":
			writeDeviceJSON(w, map[string]any{"status": []map[string]any{{"status": "message", "code": "saved"}}})
		case r.URL.Path == "/rci/":
			d.apply(r.Context(), w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (d *fakeKeeneticDevice) apply(ctx context.Context, w http.ResponseWriter, body string) {
	var commands []map[string]any
	if err := json.Unmarshal([]byte(body), &commands); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.applies++
	answers := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		ip, _ := command["ip"].(map[string]any)
		route, _ := ip["route"].(map[string]any)
		network, _ := route["network"].(string)
		mask, _ := route["mask"].(string)
		attached, _ := route["interface"].(string)
		comment, _ := route["comment"].(string)
		_, remove := route["no"]
		if network == "" || mask == "" || attached == "" {
			answers = append(answers, map[string]any{"status": []map[string]any{{"status": "error", "code": "unknown"}}})
			continue
		}
		if remove {
			delete(d.routes, network+"/"+mask)
			delete(d.comments, network+"/"+mask)
		} else {
			d.routes[network+"/"+mask] = attached
			if d.omitRouteComments {
				d.comments[network+"/"+mask] = ""
			} else {
				d.comments[network+"/"+mask] = comment
			}
		}
		if d.failAfterAdd && !remove {
			d.partialState = maps.Clone(d.routes)
			if d.cancelApply != nil {
				d.cancelApply()
			}
			if d.waitForCallerCancel {
				select {
				case <-ctx.Done():
					close(d.callerCancelled)
				case <-time.After(5 * time.Second):
				}
			}
			answers = append(answers, map[string]any{"status": []map[string]any{{"status": "error", "code": "partial.write"}}})
			break
		}
		answers = append(answers, map[string]any{"status": []map[string]any{{"status": "message", "code": "route.applied"}}})
	}
	writeDeviceJSON(w, answers)
}

func (d *fakeKeeneticDevice) applyCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.applies
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
	// One desired route and one unrelated route already exist on the selected
	// interface. Neither is owned merely because Routevane can see it there.
	device.routes["192.0.2.10/255.255.255.255"] = deviceInterfaceName
	device.routes["10.9.9.0/255.255.255.0"] = deviceInterfaceName
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()

	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID, outputID, _ := createProfileOutput(t, origin, "Видео и общение", "keenetic", "youtube", "discord")
	build := refreshAndBuild(t, origin, profileID, outputID)
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
	if routes, _ := device.snapshot(); routes != 2 {
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
	if applied.Device.FirmwareVersion != "5.1.2" || applied.Device.FormatKey == "" {
		t.Fatalf("device = %#v", applied.Device)
	}
	if applied.Backup.Hash == "" || applied.Backup.SizeBytes == 0 {
		t.Fatalf("backup = %#v", applied.Backup)
	}
	steps := make([]string, 0, len(applied.Events))
	for _, event := range applied.Events {
		steps = append(steps, event.Step+":"+event.Outcome)
	}
	if strings.Join(steps, ",") != "probe:success,backup:success,deploy:success,verify:success,ownership:success" {
		t.Fatalf("audit = %v", steps)
	}
	if routes, _ := device.snapshot(); routes != 3 {
		t.Fatalf("the device holds %d routes", routes)
	}
	device.mu.Lock()
	if device.routes["10.9.9.0/255.255.255.0"] != deviceInterfaceName || device.routes["192.0.2.10/255.255.255.255"] != deviceInterfaceName {
		device.mu.Unlock()
		t.Fatalf("foreign or pre-existing desired route was removed: %#v", device.routes)
	}
	if device.comments["198.51.100.20/255.255.255.255"] != "(Без категории/Discord)" || device.comments["192.0.2.10/255.255.255.255"] != "" {
		device.mu.Unlock()
		t.Fatalf("route descriptions = %#v", device.comments)
	}
	device.mu.Unlock()
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
	if routes, restores := device.snapshot(); routes != 3 || restores != 0 {
		t.Fatalf("a repeated deployment changed the device: routes=%d restores=%d", routes, restores)
	}
	ownershipScope := application.ManagedRouteScope{Endpoint: "http://192.168.1.1", TargetID: "keenetic", Interface: deviceInterfaceName}
	ownershipBefore := readManagedOwnership(t, data, ownershipScope)

	// A device that silently omits the RCI comment fails exact ownership
	// verification and rolls back. Prefix presence alone cannot prove that the
	// user-visible Description was installed.
	device.mu.Lock()
	device.comments["198.51.100.20/255.255.255.255"] = ""
	device.omitRouteComments = true
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
	if ownershipAfter := readManagedOwnership(t, data, ownershipScope); !reflect.DeepEqual(ownershipAfter, ownershipBefore) {
		t.Fatalf("failed verification changed ownership: before=%#v after=%#v", ownershipBefore, ownershipAfter)
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

func TestManualDeploymentOutcomeSurvivesLostResponseAndServerRestart(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	device := newFakeKeeneticDevice()
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()
	deps := runtimeDeps{
		Resolver: resolver, DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()},
		Now: func() time.Time { return now },
	}

	origin, stop, done, stderr := startServeServer(t, catalog, data, deps)
	firstStopped := false
	defer func() {
		if !firstStopped {
			stop()
			<-done
		}
	}()
	profileID, outputID, _ := createProfileOutput(t, origin, "Recoverable", "keenetic", "youtube")
	artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
	attemptID := strings.Repeat("e", 32)
	body, err := json.Marshal(map[string]any{
		"attempt_id": attemptID,
		"device":     "http://192.168.1.1", "username": deviceUser, "password": devicePassword,
		"interface": deviceInterfaceName, "confirm": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	post := func(endpoint string) []byte {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
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
		if response.StatusCode != http.StatusOK {
			t.Fatalf("POST status=%d body=%s", response.StatusCode, payload)
		}
		return payload
	}

	// The first response is deliberately ignored. Only its durable identity is
	// available to the next process, exactly as after a transport drop.
	_ = post(origin + "/v1/artifacts/" + artifactID + "/deploy")
	firstApplies := device.applyCount()
	if firstApplies == 0 {
		t.Fatal("the confirmed deployment never reached the device")
	}
	stop()
	code := <-done
	firstStopped = true
	if code != 0 {
		t.Fatalf("first serve=%d stderr=%s", code, stderr.String())
	}

	restartedOrigin, stopRestarted, restartedDone, restartedStderr := startServeServer(t, catalog, data, deps)
	defer func() {
		stopRestarted()
		if code := <-restartedDone; code != 0 {
			t.Errorf("restarted serve=%d stderr=%s", code, restartedStderr.String())
		}
	}()
	status := httpGet(t, restartedOrigin+"/v1/deployment-attempts/"+attemptID, nil)
	if status.status != http.StatusOK || !strings.Contains(string(status.body), `"status":"succeeded"`) || !strings.Contains(string(status.body), `"applied":true`) {
		t.Fatalf("attempt lookup status=%d body=%s", status.status, status.body)
	}
	replayed := post(restartedOrigin + "/v1/artifacts/" + artifactID + "/deploy")
	if !strings.Contains(string(replayed), `"status":"succeeded"`) {
		t.Fatalf("same attempt reply=%s", replayed)
	}
	if got := device.applyCount(); got != firstApplies {
		t.Fatalf("same attempt replayed device effects: before=%d after=%d", firstApplies, got)
	}
}

func TestManagedRouteClaimsPreserveForeignRoutesAndShareCreatedPrefixes(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})
	device := newFakeKeeneticDevice()
	foreign := "10.9.9.0/255.255.255.0"
	shared := "192.0.2.10/255.255.255.255"
	discord := "198.51.100.20/255.255.255.255"
	device.routes[foreign] = deviceInterfaceName
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()

	origin, stop, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		stop()
		if code := <-done; code != 0 {
			t.Errorf("serve=%d %s", code, stderr.String())
		}
	}()
	profileA, outputA, _ := createProfileOutput(t, origin, "A", "keenetic", "youtube", "discord")
	profileB, outputB, _ := createProfileOutput(t, origin, "B", "keenetic", "youtube")
	artifactA := refreshAndBuild(t, origin, profileA, outputA).Artifact.ID
	artifactB := refreshAndBuild(t, origin, profileB, outputB).Artifact.ID
	deps := runtimeDeps{Resolver: resolver, DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()}, Now: func() time.Time { return now }, Context: context.Background()}
	args := func(artifact string) []string {
		return []string{"deploy", "--artifact", artifact, "--target", "keenetic", "--device", "http://192.168.1.1", "--user", deviceUser, "--interface", deviceInterfaceName, "--catalog-dir", catalog, "--data-dir", data, "--confirm"}
	}
	deployViaCLI(t, deps, args(artifactA), devicePassword)
	deployViaCLI(t, deps, args(artifactB), devicePassword)
	if !fakeDeviceHasRoutes(device, foreign, shared, discord) {
		t.Fatalf("initial shared state = %#v", device.routes)
	}
	device.mu.Lock()
	sharedDescription := device.comments[shared]
	discordDescription := device.comments[discord]
	foreignDescription := device.comments[foreign]
	device.mu.Unlock()
	if sharedDescription != "(Без категории/YouTube)" || discordDescription != "(Без категории/Discord)" || foreignDescription != "" {
		t.Fatalf("route descriptions: shared=%q discord=%q foreign=%q", sharedDescription, discordDescription, foreignDescription)
	}

	postJSON(t, origin+"/v1/profiles/"+profileA+"/update", `{"name":"A","lists":["discord"]}`)
	artifactA = refreshAndBuild(t, origin, profileA, outputA).Artifact.ID
	deployViaCLI(t, deps, args(artifactA), devicePassword)
	if !fakeDeviceHasRoutes(device, foreign, shared, discord) {
		t.Fatalf("the first claimant removed a shared route: %#v", device.routes)
	}

	postJSON(t, origin+"/v1/profiles/"+profileB+"/update", `{"name":"B","lists":["discord"]}`)
	artifactB = refreshAndBuild(t, origin, profileB, outputB).Artifact.ID
	deployViaCLI(t, deps, args(artifactB), devicePassword)
	device.mu.Lock()
	_, sharedPresent := device.routes[shared]
	foreignInterface := device.routes[foreign]
	discordInterface := device.routes[discord]
	device.mu.Unlock()
	if sharedPresent || foreignInterface != deviceInterfaceName || discordInterface != deviceInterfaceName {
		t.Fatalf("last-claim reconciliation = %#v", device.routes)
	}
}

func fakeDeviceHasRoutes(device *fakeKeeneticDevice, routes ...string) bool {
	device.mu.Lock()
	defer device.mu.Unlock()
	if len(device.routes) != len(routes) {
		return false
	}
	for _, route := range routes {
		if device.routes[route] != deviceInterfaceName {
			return false
		}
	}
	return true
}

func readManagedOwnership(t *testing.T, data string, scope application.ManagedRouteScope) application.ManagedRouteOwnership {
	t.Helper()
	store, err := sqlite.OpenExisting(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.ManagedRouteOwnership(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPartialDeviceWriteRestoresTheExactBeforeStateEvenAfterCancellation(t *testing.T) {
	for _, cancelCaller := range []bool{false, true} {
		t.Run(map[bool]string{false: "device rejection", true: "caller cancellation"}[cancelCaller], func(t *testing.T) {
			catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
			data := filepath.Join(t.TempDir(), "data")
			now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
			resolver := &hostAddressResolver{}
			resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})
			origin, stop, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
			defer func() {
				stop()
				if code := <-done; code != 0 {
					t.Errorf("serve=%d %s", code, stderr.String())
				}
			}()
			profileID, outputID, _ := createProfileOutput(t, origin, "Recovery", "keenetic", "youtube", "discord")
			build := refreshAndBuild(t, origin, profileID, outputID)
			beforeArtifact := downloadArtifact(t, origin, build.Artifact.ID)
			device := newFakeKeeneticDevice()
			before := map[string]string{"192.0.2.99/255.255.255.255": deviceInterfaceName, "203.0.113.7/255.255.255.255": "ISP"}
			device.routes = maps.Clone(before)
			device.failAfterAdd = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelCaller {
				device.cancelApply = cancel
			}
			server := httptest.NewServer(device.handler())
			defer server.Close()
			deps := runtimeDeps{Resolver: resolver, DeviceDialer: redirectDialer{target: server.Listener.Addr().String()}, Now: func() time.Time { return now }, Context: ctx}
			args := []string{"deploy", "--artifact", build.Artifact.ID, "--target", "keenetic", "--device", "http://192.168.1.1", "--user", deviceUser, "--interface", deviceInterfaceName, "--catalog-dir", catalog, "--data-dir", data, "--confirm"}
			t.Setenv(DevicePasswordVariable, devicePassword)
			stdout, deployStderr := &syncBuffer{}, &syncBuffer{}
			if code := runWithDeps(stdout, deployStderr, args, deps); code == 0 {
				t.Fatal("partial write reported success")
			}
			var report deployReport
			if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
				t.Fatalf("report=%s error=%v", stdout.String(), err)
			}
			if report.Applied || !report.RolledBack {
				t.Fatalf("recovery=%#v stderr=%s", report, deployStderr.String())
			}
			device.mu.Lock()
			after, partial, restores := maps.Clone(device.routes), maps.Clone(device.partialState), device.restores
			device.mu.Unlock()
			if maps.Equal(partial, before) || partial["192.0.2.10/255.255.255.255"] == "" && partial["198.51.100.20/255.255.255.255"] == "" {
				t.Fatalf("fixture never partially installed a new destination: %v", partial)
			}
			if partial["203.0.113.7/255.255.255.255"] != "ISP" || !maps.Equal(after, before) || restores != 1 {
				t.Fatalf("recovery accumulated or lost state: before=%v partial=%v after=%v restores=%d", before, partial, after, restores)
			}
			if cancelCaller && ctx.Err() == nil {
				t.Fatal("the caller never cancelled")
			}
			if afterArtifact := downloadArtifact(t, origin, build.Artifact.ID); !slices.Equal(beforeArtifact.body, afterArtifact.body) {
				t.Fatal("device failure changed the publication")
			}
			steps := []string{}
			for _, event := range report.Events {
				steps = append(steps, event.Step+":"+event.Outcome)
			}
			if strings.Join(steps, ",") != "probe:success,backup:success,deploy:failed,rollback:success" {
				t.Fatalf("events=%v", steps)
			}
		})
	}
}

func TestDesktopSignalCancelsDeploymentBeforeShutdownAndWaitsForRollback(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})

	// Create the immutable artifact through the ordinary surface first. The
	// desktop process then reopens that same store, which makes its shutdown
	// responsible for the device recovery and the database lifecycle together.
	origin, stop, served, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	profileID, outputID, _ := createProfileOutput(t, origin, "Desktop recovery", "keenetic", "youtube", "discord")
	artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
	stop()
	if code := <-served; code != 0 {
		t.Fatalf("artifact server=%d %s", code, stderr.String())
	}

	device := newFakeKeeneticDevice()
	before := map[string]string{"192.0.2.99/255.255.255.255": deviceInterfaceName, "203.0.113.7/255.255.255.255": "ISP"}
	device.routes = maps.Clone(before)
	device.failAfterAdd = true
	device.waitForCallerCancel = true
	device.callerCancelled = make(chan struct{})
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()

	input, parent := io.Pipe()
	output, ready := io.Pipe()
	defer input.Close()
	defer parent.Close()
	defer output.Close()
	parentCtx, cancelParent := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelParent()
	signalReady := make(chan context.CancelFunc, 1)
	desktopDone := make(chan int, 1)
	device.cancelApply = func() { (<-signalReady)() }
	go func() {
		desktopDone <- runWithDeps(ready, io.Discard, []string{"desktop", "--catalog-dir", catalog, "--data-dir", data}, runtimeDeps{
			Resolver:     resolver,
			DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()},
			Now:          func() time.Time { return now },
			Context:      parentCtx,
			DesktopInput: input,
			SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(parent)
				signalReady <- cancel
				return ctx, cancel
			},
		})
		_ = ready.Close()
	}()

	token := strings.Repeat("a", 64)
	if _, err := io.WriteString(parent, token+"\n"); err != nil {
		t.Fatal(err)
	}
	desktopOrigin, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	desktopOrigin = strings.TrimSpace(desktopOrigin)
	// SignalContext is installed after the ready line is published.
	signalCancel := <-signalReady
	signalReady <- signalCancel

	payload, err := json.Marshal(map[string]any{
		"attempt_id": strings.Repeat("e", 32),
		"device":     "http://192.168.1.1", "username": deviceUser, "password": devicePassword,
		"interface": deviceInterfaceName, "confirm": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, desktopOrigin+"/v1/artifacts/"+artifactID+"/deploy", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	request.Header.Set("X-Routevane-Desktop", token)
	clientDone := make(chan struct{})
	go func() {
		response, _ := http.DefaultClient.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		close(clientDone)
	}()

	select {
	case <-device.callerCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("desktop signal did not cancel the active device request")
	}
	select {
	case code := <-desktopDone:
		if code != 0 {
			t.Fatalf("desktop shutdown=%d", code)
		}
	case <-parentCtx.Done():
		t.Fatal("desktop shutdown did not wait for recovery")
	}
	<-clientDone

	device.mu.Lock()
	after, partial, restores := maps.Clone(device.routes), maps.Clone(device.partialState), device.restores
	device.mu.Unlock()
	if maps.Equal(partial, before) || (partial["192.0.2.10/255.255.255.255"] == "" && partial["198.51.100.20/255.255.255.255"] == "") {
		t.Fatalf("fixture did not reach a partial device state: %v", partial)
	}
	if !maps.Equal(after, before) || restores != 1 {
		t.Fatalf("shutdown left device state behind: before=%v after=%v restores=%d", before, after, restores)
	}
	store, err := sqlite.OpenExisting(context.Background(), data)
	if err != nil {
		t.Fatalf("desktop closed before releasing its store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLISignalWaitsPastFiveSecondsForDeploymentRollback(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})

	origin, stop, served, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	profileID, outputID, _ := createProfileOutput(t, origin, "CLI recovery", "keenetic", "youtube", "discord")
	artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
	stop()
	if code := <-served; code != 0 {
		t.Fatalf("artifact server=%d %s", code, stderr.String())
	}

	device := newFakeKeeneticDevice()
	before := map[string]string{"192.0.2.99/255.255.255.255": deviceInterfaceName, "203.0.113.7/255.255.255.255": "ISP"}
	device.routes = maps.Clone(before)
	device.failAfterAdd = true
	device.waitForCallerCancel = true
	device.callerCancelled = make(chan struct{})
	device.rollbackStarted = make(chan struct{})
	releaseRollback := make(chan struct{})
	defer func() {
		select {
		case <-releaseRollback:
		default:
			close(releaseRollback)
		}
	}()
	device.releaseRollback = releaseRollback
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	processCtx, cancelProcess := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelProcess()
	signalReady := make(chan context.CancelFunc, 1)
	done := make(chan int, 1)
	stdout, processStderr := &syncBuffer{}, &syncBuffer{}
	go func() {
		done <- runWithDeps(stdout, processStderr, []string{"serve", "--catalog-dir", catalog, "--data-dir", data}, runtimeDeps{
			Resolver:     resolver,
			DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()},
			Now:          func() time.Time { return now },
			Context:      processCtx,
			Listen:       func(string, string) (net.Listener, error) { return listener, nil },
			SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(parent)
				signalReady <- cancel
				return ctx, cancel
			},
		})
	}()
	signalCancel := <-signalReady
	cliOrigin := "http://" + listener.Addr().String()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := http.Get(cliOrigin + "/health")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	device.cancelApply = signalCancel
	payload, err := json.Marshal(map[string]any{
		"attempt_id": strings.Repeat("f", 32),
		"device":     "http://192.168.1.1", "username": deviceUser, "password": devicePassword,
		"interface": deviceInterfaceName, "confirm": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, cliOrigin+"/v1/artifacts/"+artifactID+"/deploy", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	clientDone := make(chan struct{})
	go func() {
		response, _ := http.DefaultClient.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		close(clientDone)
	}()

	select {
	case <-device.callerCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("CLI signal did not cancel the active device request")
	}
	select {
	case <-device.rollbackStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled deployment did not begin rollback")
	}
	select {
	case code := <-done:
		t.Fatalf("CLI closed resources before rollback finished: code=%d stderr=%s", code, processStderr.String())
	case <-time.After(6 * time.Second):
	}
	close(releaseRollback)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("CLI shutdown=%d stderr=%s", code, processStderr.String())
		}
	case <-processCtx.Done():
		t.Fatal("CLI shutdown did not finish after rollback")
	}
	<-clientDone

	device.mu.Lock()
	after, restores := maps.Clone(device.routes), device.restores
	device.mu.Unlock()
	if !maps.Equal(after, before) || restores != 1 {
		t.Fatalf("CLI shutdown left device state behind: before=%v after=%v restores=%d", before, after, restores)
	}
	store, err := sqlite.OpenExisting(context.Background(), data)
	if err != nil {
		t.Fatalf("CLI did not release its store after recovery: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
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
