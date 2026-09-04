package keenetic

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/netpolicy"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

const (
	deviceUser      = "admin"
	devicePassword  = "correct-horse"
	deviceRealm     = "Keenetic"
	deviceChallenge = "0123456789abcdef"
	deviceInterface = "Wireguard0"
)

// deviceDouble answers the requests this deployer makes, in the shape the
// community-documented RCI surface uses. It records what it was asked to do so a
// test can assert the ordering the deployment contract requires.
type deviceDouble struct {
	mu sync.Mutex

	firmware string
	// routes is the device's static-route table keyed by "network/mask" with the
	// interface it is attached to.
	routes map[string]string
	// fqdn is the device's FQDN object groups and dnsRoutes the dns-proxy
	// routes pointing at them. They are the second artifact format's state and
	// live beside the route table because it is one device.
	fqdn      map[string][]string
	dnsRoutes map[string]string
	config    []byte

	authenticated bool
	deployCalls   int
	saveCalls     int
	restoreCalls  int
	batches       [][]map[string]any
	// failDeploy and failVerify make the device reject an operation.
	failDeploy bool
	// driftAfterDeploy makes the device silently lose one route, which is what a
	// partial write looks like from outside.
	driftAfterDeploy bool
	// requests records every path, so a test can prove no credential was sent
	// anywhere unexpected.
	requests []string
	// commands records every parsed command line in the order the device read
	// it, which is what makes the reconciliation order assertable.
	commands []string
}

// applyLine interprets one command line the way the device does, including the
// refusals that make the deployer's ordering necessary rather than tidy.
func (d *deviceDouble) applyLine(line string) map[string]any {
	refuse := func(code, message string) map[string]any {
		return map[string]any{"status": []map[string]any{{"status": "error", "code": code, "message": message}}}
	}
	accept := map[string]any{"status": []map[string]any{{"status": "message", "code": "applied"}}}
	fields := strings.Fields(line)
	switch {
	case line == "system configuration save":
		d.saveCalls++
		return accept
	case len(fields) == 3 && fields[0] == "object-group" && fields[1] == "fqdn":
		if _, exists := d.fqdn[fields[2]]; !exists {
			d.fqdn[fields[2]] = []string{}
		}
		return accept
	case len(fields) == 5 && fields[0] == "object-group" && fields[1] == "fqdn" && fields[3] == "include":
		group, exists := d.fqdn[fields[2]]
		if !exists {
			return refuse("objectGroup.missing", "no such group")
		}
		d.fqdn[fields[2]] = append(group, fields[4])
		return accept
	case len(fields) == 4 && fields[0] == "no" && fields[1] == "object-group" && fields[2] == "fqdn":
		// A group a route still points at cannot be removed. This refusal is
		// why the deployer withdraws routes first.
		if _, bound := d.dnsRoutes[fields[3]]; bound {
			return refuse("objectGroup.inUse", "group is in use by dns-proxy")
		}
		delete(d.fqdn, fields[3])
		return accept
	case len(fields) == 6 && fields[0] == "no" && fields[1] == "object-group" && fields[2] == "fqdn" && fields[4] == "include":
		kept := make([]string, 0, len(d.fqdn[fields[3]]))
		for _, entry := range d.fqdn[fields[3]] {
			if entry != fields[5] {
				kept = append(kept, entry)
			}
		}
		d.fqdn[fields[3]] = kept
		return accept
	case len(fields) == 6 && fields[0] == "dns-proxy" && fields[1] == "route" && fields[2] == "object-group" && fields[5] == "auto":
		if _, exists := d.fqdn[fields[3]]; !exists {
			return refuse("dnsProxy.missingGroup", "no such group")
		}
		d.dnsRoutes[fields[3]] = fields[4]
		return accept
	case len(fields) == 6 && fields[0] == "no" && fields[1] == "dns-proxy" && fields[2] == "route" && fields[3] == "object-group":
		delete(d.dnsRoutes, fields[4])
		return accept
	default:
		return refuse("parse.unknown", "unsupported command")
	}
}

func (d *deviceDouble) fqdnList() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	groups := make(map[string]any, len(d.fqdn))
	for name, entries := range d.fqdn {
		include := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			include = append(include, map[string]any{"address": entry})
		}
		groups[name] = map[string]any{"include": include}
	}
	return groups
}

func (d *deviceDouble) dnsRouteList() []map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	routes := make([]map[string]any, 0, len(d.dnsRoutes))
	for group, attached := range d.dnsRoutes {
		routes = append(routes, map[string]any{"group": group, "interface": attached})
	}
	sort.Slice(routes, func(i, j int) bool {
		left, _ := routes[i]["group"].(string)
		right, _ := routes[j]["group"].(string)
		return left < right
	})
	return routes
}

// groupSnapshot is the device's own view of what it holds, so an assertion
// reads the device rather than the deployer's belief about it.
func (d *deviceDouble) groupSnapshot() (map[string][]string, map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	groups := make(map[string][]string, len(d.fqdn))
	for name, entries := range d.fqdn {
		ordered := append([]string(nil), entries...)
		sort.Strings(ordered)
		groups[name] = ordered
	}
	routes := make(map[string]string, len(d.dnsRoutes))
	for group, attached := range d.dnsRoutes {
		routes[group] = attached
	}
	return groups, routes
}

func (d *deviceDouble) commandLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.commands...)
}

func newDeviceDouble(firmware string) *deviceDouble {
	return &deviceDouble{
		firmware:  firmware,
		routes:    map[string]string{},
		fqdn:      map[string][]string{},
		dnsRoutes: map[string]string{},
		config:    []byte("! Keenetic startup-config\nsystem hostname router\n"),
	}
}

func (d *deviceDouble) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.requests = append(d.requests, r.Method+" "+r.URL.Path)
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
			var credentials struct {
				Login    string `json:"login"`
				Password string `json:"password"`
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
			if err := json.Unmarshal(body, &credentials); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if credentials.Login != deviceUser || credentials.Password != expectedProof() {
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
			d.mu.Lock()
			firmware := d.firmware
			d.mu.Unlock()
			writeJSON(w, map[string]any{"release": firmware, "model": "Giga", "manufacturer": "Keenetic"})
		case r.URL.Path == "/rci/show/interface":
			writeJSON(w, map[string]any{deviceInterface: map[string]any{"state": "up"}, "ISP": map[string]any{"state": "up"}})
		case r.URL.Path == "/rci/show/sc/ip/route":
			writeJSON(w, map[string]any{"route": d.routeList()})
		case r.URL.Path == "/rci/object-group/fqdn":
			writeJSON(w, d.fqdnList())
		case r.URL.Path == "/rci/dns-proxy/route":
			writeJSON(w, d.dnsRouteList())
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet:
			d.mu.Lock()
			config := append([]byte(nil), d.config...)
			d.mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(config)
		case r.URL.Path == "/ci/startup-config" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			d.mu.Lock()
			d.config = body
			d.restoreCalls++
			// A restore returns the device to the state the backup describes.
			d.routes = map[string]string{}
			d.fqdn = map[string][]string{}
			d.dnsRoutes = map[string]string{}
			d.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/rci/system/configuration/save":
			d.mu.Lock()
			d.saveCalls++
			d.mu.Unlock()
			writeJSON(w, map[string]any{"status": []map[string]any{{"status": "message", "code": "saved"}}})
		case r.URL.Path == "/rci/":
			d.applyBatch(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (d *deviceDouble) applyBatch(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var commands []map[string]any
	if err := json.Unmarshal(body, &commands); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deployCalls++
	d.batches = append(d.batches, commands)
	if d.failDeploy {
		// The device answers 200 with an error status, which is how a rejected
		// command actually looks.
		writeJSON(w, []map[string]any{{"status": []map[string]any{{"status": "error", "code": "route.invalid", "message": "refused"}}}})
		return
	}
	answers := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		// A batch is either structured route objects or parsed command lines.
		// The device accepts both; which one arrives says which artifact format
		// is being installed.
		if line, parsed := command["parse"].(string); parsed {
			d.commands = append(d.commands, line)
			answers = append(answers, map[string]any{"parse": d.applyLine(line)})
			continue
		}
		route, ok := routeOf(command)
		if !ok {
			answers = append(answers, map[string]any{"status": []map[string]any{{"status": "error", "code": "unknown", "message": "unsupported"}}})
			continue
		}
		key := route.network + "/" + route.mask
		if route.remove {
			delete(d.routes, key)
		} else {
			d.routes[key] = route.deviceInterface
		}
		answers = append(answers, map[string]any{"status": []map[string]any{{"status": "message", "code": "route.applied"}}})
	}
	if d.driftAfterDeploy {
		for key := range d.routes {
			delete(d.routes, key)
			break
		}
	}
	writeJSON(w, answers)
}

type deviceRoute struct {
	network, mask, deviceInterface string
	remove                         bool
}

func routeOf(command map[string]any) (deviceRoute, bool) {
	ip, ok := command["ip"].(map[string]any)
	if !ok {
		return deviceRoute{}, false
	}
	route, ok := ip["route"].(map[string]any)
	if !ok {
		return deviceRoute{}, false
	}
	network, _ := route["network"].(string)
	mask, _ := route["mask"].(string)
	deviceInterfaceName, _ := route["interface"].(string)
	_, remove := route["no"]
	if network == "" || mask == "" || deviceInterfaceName == "" {
		return deviceRoute{}, false
	}
	return deviceRoute{network: network, mask: mask, deviceInterface: deviceInterfaceName, remove: remove}, true
}

func (d *deviceDouble) routeList() []map[string]any {
	routes := make([]map[string]any, 0, len(d.routes))
	for key, attached := range d.routes {
		parts := strings.SplitN(key, "/", 2)
		routes = append(routes, map[string]any{"network": parts[0], "mask": parts[1], "interface": attached})
	}
	return routes
}

func (d *deviceDouble) routeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.routes)
}

func (d *deviceDouble) counts() (deploys, saves, restores int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deployCalls, d.saveCalls, d.restoreCalls
}

func (d *deviceDouble) sentPaths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.requests...)
}

func expectedProof() string {
	inner := md5.Sum([]byte(deviceUser + ":" + deviceRealm + ":" + devicePassword)) // #nosec G401 -- reproduces the device's documented challenge scheme in a test.
	outer := sha256.Sum256([]byte(deviceChallenge + hex.EncodeToString(inner[:])))
	return hex.EncodeToString(outer[:])
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	payload, _ := json.Marshal(value)
	_, _ = w.Write(payload)
}

// deviceFixture wires a deployer to a device double. The connection URL is a
// private address, so the device destination policy is exercised for real; the
// dialer sends that connection to the local double.
type deviceFixture struct {
	device     *deviceDouble
	deployer   *Deployer
	connection application.Connection
}

func newDeviceFixture(t *testing.T, firmware string) *deviceFixture {
	t.Helper()
	device := newDeviceDouble(firmware)
	server := httptest.NewServer(device.handler())
	t.Cleanup(server.Close)
	return &deviceFixture{
		device:   device,
		deployer: New(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}}),
		connection: application.Connection{
			URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface,
		},
	}
}

type fixtureDialer struct{ target string }

func (d fixtureDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, d.target)
}

func testArtifact(t *testing.T, prefixes ...string) application.DeployArtifact {
	t.Helper()
	var builder strings.Builder
	for _, prefix := range prefixes {
		parts := strings.SplitN(prefix, "/", 2)
		bits := 32
		if len(parts) == 2 {
			bits = 0
			for _, digit := range parts[1] {
				bits = bits*10 + int(digit-'0')
			}
		}
		mask := net.CIDRMask(bits, 32)
		fmt.Fprintf(&builder, "route ADD %s MASK %d.%d.%d.%d 0.0.0.0\r\n", parts[0], mask[0], mask[1], mask[2], mask[3])
	}
	payload := []byte(builder.String())
	if err := keenetic.Validate(payload); err != nil {
		t.Fatalf("test artifact is not a valid Keenetic artifact: %v", err)
	}
	return application.DeployArtifact{
		ArtifactID: strings.Repeat("a", 32), RendererID: keenetic.ID,
		ArtifactHash: strings.Repeat("b", 64), ContentType: "application/x-bat", Payload: payload,
	}
}

func TestStoredConnectionValidationRefusesSecretsAndNonDeviceDestinations(t *testing.T) {
	deployer := newDeviceFixture(t, "5.1.2").deployer
	valid := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Interface: deviceInterface}
	if err := deployer.ValidateStoredConnection(valid); err != nil {
		t.Fatal(err)
	}
	for name, connection := range map[string]application.Connection{
		"userinfo":  {URL: "http://admin@192.168.1.1", Username: deviceUser, Interface: deviceInterface},
		"query":     {URL: "http://192.168.1.1?password=secret", Username: deviceUser, Interface: deviceInterface},
		"fragment":  {URL: "http://192.168.1.1/#secret", Username: deviceUser, Interface: deviceInterface},
		"public IP": {URL: "https://203.0.113.10", Username: deviceUser, Interface: deviceInterface},
		"password":  {URL: valid.URL, Username: deviceUser, Password: devicePassword, Interface: deviceInterface},
	} {
		t.Run(name, func(t *testing.T) {
			if err := deployer.ValidateStoredConnection(connection); err == nil {
				t.Fatalf("connection accepted: %#v", connection)
			}
		})
	}
}

func TestProbeReportsTheFirmwareAndRefusesAnUnsupportedOne(t *testing.T) {
	supported := newDeviceFixture(t, "5.1.2")
	device, err := supported.deployer.Probe(context.Background(), supported.connection)
	if err != nil {
		t.Fatal(err)
	}
	if device.ProfileKey != keenetic.Version || device.FirmwareVersion != "5.1.2" || device.Model != "Giga" {
		t.Fatalf("device = %#v", device)
	}
	if device.Interface != deviceInterface {
		t.Fatalf("device = %#v", device)
	}

	// An older firmware reports no profile, which is what makes the deployment
	// refuse before anything changes.
	old := newDeviceFixture(t, "4.9.9")
	older, err := old.deployer.Probe(context.Background(), old.connection)
	if err != nil {
		t.Fatal(err)
	}
	if older.ProfileKey != "" || older.FirmwareVersion != "4.9.9" {
		t.Fatalf("device = %#v", older)
	}
	// An unsupported device is never asked about its interfaces or its routes.
	for _, path := range old.device.sentPaths() {
		if strings.Contains(path, "/rci/show/sc/ip/route") {
			t.Fatalf("an unsupported device was asked to enumerate routes: %v", old.device.sentPaths())
		}
	}

	// Naming no interface is a refused connection, not a device answer: nothing
	// was asked of the device.
	missing := newDeviceFixture(t, "5.1.2")
	missing.connection.Interface = ""
	if err := missing.deployer.ValidateConnection(missing.connection); !errors.Is(err, ErrDeviceRefused) {
		t.Fatalf("err = %v, want ErrDeviceRefused", err)
	}
	if _, err := missing.deployer.Probe(context.Background(), missing.connection); !errors.Is(err, ErrDeviceRefused) {
		t.Fatalf("err = %v, want ErrDeviceRefused", err)
	}
	if len(missing.device.sentPaths()) != 0 {
		t.Fatalf("a refused connection reached the device: %v", missing.device.sentPaths())
	}
	// An interface the device does not have is a device answer.
	unknown := newDeviceFixture(t, "5.1.2")
	unknown.connection.Interface = "Nonexistent0"
	if _, err := unknown.deployer.Probe(context.Background(), unknown.connection); !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("err = %v, want ErrDeviceAnswer", err)
	}
}

func TestSupportedFirmwareComparesVersionsRatherThanStrings(t *testing.T) {
	supported := []string{"5.0.4", "5.0.5", "5.1", "5.1.2", "5.10.0", "6.0.0", "5.0.4-1", "5.1.2 (AAAA)"}
	for _, version := range supported {
		if !SupportedFirmware(version) {
			t.Fatalf("%q must be supported", version)
		}
	}
	unsupported := []string{"", "5.0.3", "5.0", "4.9.9", "3.7.5", "beta", "5.0.4.1", "99999999.0.0"}
	for _, version := range unsupported {
		if SupportedFirmware(version) {
			t.Fatalf("%q must not be supported", version)
		}
	}
}

func TestDeployWithoutALedgerIsIdempotentAndAdditive(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	// With no ledger, routes on every interface are foreign and must survive.
	fixture.device.routes["10.9.9.0/255.255.255.0"] = "ISP"
	fixture.device.routes["10.8.8.0/255.255.255.0"] = deviceInterface
	device, err := fixture.deployer.Probe(context.Background(), fixture.connection)
	if err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact(t, "192.0.2.10/32", "198.51.100.0/24")
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.deployer.Verify(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	firstDeploys, firstSaves, _ := fixture.device.counts()

	// The same artifact again changes nothing and issues no command batch.
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.deployer.Verify(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	secondDeploys, secondSaves, _ := fixture.device.counts()
	if secondDeploys != firstDeploys || secondSaves != firstSaves {
		t.Fatalf("a repeated deployment changed the device: batches %d->%d saves %d->%d", firstDeploys, secondDeploys, firstSaves, secondSaves)
	}
	if fixture.device.routeCount() != 4 {
		t.Fatalf("route table = %#v", fixture.device.routes)
	}
	if fixture.device.routes["10.9.9.0/255.255.255.0"] != "ISP" {
		t.Fatalf("a route on another interface was disturbed: %#v", fixture.device.routes)
	}

	// A different artifact remains additive: without a persisted claim, even a
	// route from an earlier process cannot be proven removable.
	replacement := testArtifact(t, "203.0.113.5/32")
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, replacement); err != nil {
		t.Fatal(err)
	}
	if err := fixture.deployer.Verify(context.Background(), device, fixture.connection, replacement); err != nil {
		t.Fatal(err)
	}
	if fixture.device.routeCount() != 5 {
		t.Fatalf("route table = %#v", fixture.device.routes)
	}
	if fixture.device.routes["10.8.8.0/255.255.255.0"] != deviceInterface {
		t.Fatalf("a foreign same-interface route was removed: %#v", fixture.device.routes)
	}
}

func TestVerifyFailsWhenTheDeviceDoesNotHoldTheArtifact(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	fixture.device.driftAfterDeploy = true
	device, err := fixture.deployer.Probe(context.Background(), fixture.connection)
	if err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact(t, "192.0.2.10/32", "198.51.100.0/24")
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	err = fixture.deployer.Verify(context.Background(), device, fixture.connection, artifact)
	if !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("err = %v, want ErrDeviceAnswer", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("verification must name what is missing: %v", err)
	}
}

func TestDeployRefusesADeviceRejectionAndAForeignArtifact(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	fixture.device.failDeploy = true
	device, err := fixture.deployer.Probe(context.Background(), fixture.connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, testArtifact(t, "192.0.2.10/32")); !errors.Is(err, ErrDeviceRefused) {
		t.Fatalf("err = %v, want ErrDeviceRefused", err)
	}
	if _, saves, _ := fixture.device.counts(); saves != 0 {
		t.Fatal("a rejected batch must not be saved to the device configuration")
	}

	foreign := application.DeployArtifact{ArtifactID: "x", RendererID: "singbox-ruleset-json", Payload: []byte("{}")}
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, foreign); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("err = %v, want ErrArtifactMismatch", err)
	}
	corrupt := testArtifact(t, "192.0.2.10/32")
	corrupt.Payload = []byte("route DELETE everything\r\n")
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, corrupt); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("err = %v, want ErrArtifactMismatch", err)
	}
}

func TestBackupAndRollbackRestoreTheCapturedConfiguration(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	device, err := fixture.deployer.Probe(context.Background(), fixture.connection)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := fixture.deployer.Backup(context.Background(), device, fixture.connection)
	if err != nil {
		t.Fatal(err)
	}
	if len(backup.Payload) == 0 || !strings.Contains(string(backup.Payload), "startup-config") {
		t.Fatalf("backup = %q", backup.Payload)
	}
	artifact := testArtifact(t, "192.0.2.10/32")
	if err := fixture.deployer.Deploy(context.Background(), device, fixture.connection, artifact); err != nil {
		t.Fatal(err)
	}
	if fixture.device.routeCount() == 0 {
		t.Fatal("the deployment installed nothing")
	}
	if err := fixture.deployer.Rollback(context.Background(), device, fixture.connection, backup); err != nil {
		t.Fatal(err)
	}
	if fixture.device.routeCount() != 0 {
		t.Fatalf("rollback left routes behind: %#v", fixture.device.routes)
	}
	if _, _, restores := fixture.device.counts(); restores != 1 {
		t.Fatalf("restore calls = %d", restores)
	}
	if err := fixture.deployer.Rollback(context.Background(), device, fixture.connection, application.BackupPayload{}); !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("a rollback without a backup must be refused: %v", err)
	}
}

func TestTheDeployerNeverLeavesTheLocalNetwork(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	refused := []string{
		"http://127.0.0.1",
		"http://203.0.113.10",
		"http://8.8.8.8",
		"http://169.254.169.254",
		"http://router.local",
		"http://192.168.1.1/rci/",
		"ftp://192.168.1.1",
		"http://user:secret@192.168.1.1",
		"",
	}
	for _, url := range refused {
		t.Run(url, func(t *testing.T) {
			connection := fixture.connection
			connection.URL = url
			if _, err := fixture.deployer.Probe(context.Background(), connection); !errors.Is(err, netpolicy.ErrNotADeviceDestination) {
				t.Fatalf("err = %v, want ErrNotADeviceDestination", err)
			}
		})
	}
	// A missing credential is refused before any request is made.
	noCredential := fixture.connection
	noCredential.Password = ""
	if _, err := fixture.deployer.Probe(context.Background(), noCredential); !errors.Is(err, ErrDeviceRefused) {
		t.Fatalf("err = %v, want ErrDeviceRefused", err)
	}
}

func TestTheDevicePasswordNeverAppearsInAnErrorOrAnAnswer(t *testing.T) {
	fixture := newDeviceFixture(t, "5.1.2")
	wrong := fixture.connection
	wrong.Password = "wrong-password-value"
	_, err := fixture.deployer.Probe(context.Background(), wrong)
	if err == nil {
		t.Fatal("a wrong password must be refused")
	}
	if strings.Contains(err.Error(), "wrong-password-value") {
		t.Fatalf("the error leaked the credential: %v", err)
	}
	device, probeErr := fixture.deployer.Probe(context.Background(), fixture.connection)
	if probeErr != nil {
		t.Fatal(probeErr)
	}
	// Nothing the deployer returns carries the credential.
	encoded, marshalErr := json.Marshal(device)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), devicePassword) {
		t.Fatalf("device info leaked the credential: %s", encoded)
	}
	if strings.Contains(string(encoded), expectedProof()) {
		t.Fatalf("device info leaked the authentication proof: %s", encoded)
	}
}

func TestTheDeployerHonorsTheCallerDeadline(t *testing.T) {
	slow := newDeviceDouble("5.1.2")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		slow.handler().ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	deployer := New(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
	connection := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := deployer.Probe(ctx, connection); err == nil {
		t.Fatal("a device that does not answer must fail the probe")
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("the deadline was not enforced: %v", elapsed)
	}
}
