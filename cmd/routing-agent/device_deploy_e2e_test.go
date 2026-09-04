package main

import (
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
	mu                sync.Mutex
	authenticated     bool
	routes            map[string]string
	comments          map[string]string
	config            []byte
	restores          int
	bodies            []string
	backupRoutes      map[string]string
	backupComments    map[string]string
	partialState      map[string]string
	failAfterAdd      bool
	omitRouteComments bool
	cancelApply       context.CancelFunc
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
			answers = append(answers, map[string]any{"status": []map[string]any{{"status": "error", "code": "partial.write"}}})
			break
		}
		answers = append(answers, map[string]any{"status": []map[string]any{{"status": "message", "code": "route.applied"}}})
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
	listA, outputA, _ := createListOutput(t, origin, "A", "keenetic", "youtube", "discord")
	listB, outputB, _ := createListOutput(t, origin, "B", "keenetic", "youtube")
	artifactA := refreshAndBuild(t, origin, listA, outputA).Artifact.ID
	artifactB := refreshAndBuild(t, origin, listB, outputB).Artifact.ID
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

	postJSON(t, origin+"/v1/lists/"+listA+"/update", `{"name":"A","services":["discord"]}`)
	artifactA = refreshAndBuild(t, origin, listA, outputA).Artifact.ID
	deployViaCLI(t, deps, args(artifactA), devicePassword)
	if !fakeDeviceHasRoutes(device, foreign, shared, discord) {
		t.Fatalf("the first claimant removed a shared route: %#v", device.routes)
	}

	postJSON(t, origin+"/v1/lists/"+listB+"/update", `{"name":"B","services":["discord"]}`)
	artifactB = refreshAndBuild(t, origin, listB, outputB).Artifact.ID
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
			listID, outputID, _ := createListOutput(t, origin, "Recovery", "keenetic", "youtube", "discord")
			build := refreshAndBuild(t, origin, listID, outputID)
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
