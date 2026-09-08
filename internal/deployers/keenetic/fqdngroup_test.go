package keenetic

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

// fqdnFixture wires the FQDN deployer to the same device double the route
// deployer uses. One router, two artifact formats.
type fqdnFixture struct {
	device     *deviceDouble
	deployer   *FQDNDeployer
	connection application.Connection
	owned      []application.ManagedFQDNGroup
}

func newFQDNFixture(t *testing.T, firmware string) *fqdnFixture {
	t.Helper()
	device := newDeviceDouble(firmware)
	server := httptest.NewServer(device.handler())
	t.Cleanup(server.Close)
	return &fqdnFixture{
		device:   device,
		deployer: NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}}),
		connection: application.Connection{
			URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface,
		},
	}
}

// fqdnArtifact builds a valid artifact from "list=entry,entry" pairs.
func fqdnArtifact(t *testing.T, groups ...string) application.DeployArtifact {
	t.Helper()
	var builder strings.Builder
	names := make([]string, 0, len(groups))
	entries := make(map[string][]string, len(groups))
	for _, group := range groups {
		name, values, found := strings.Cut(group, "=")
		if !found {
			t.Fatalf("malformed test group %q", group)
		}
		full := keeneticdns.GroupPrefix + name
		names = append(names, full)
		entries[full] = strings.Split(values, ",")
	}
	sort.Strings(names)
	for _, name := range names {
		ordered := append([]string(nil), entries[name]...)
		sort.Strings(ordered)
		for _, entry := range ordered {
			builder.WriteString("object-group fqdn " + name + " include " + entry + "\n")
		}
	}
	payload := []byte(builder.String())
	if err := keeneticdns.Validate(payload); err != nil {
		t.Fatalf("test artifact is not a valid FQDN artifact: %v", err)
	}
	return application.DeployArtifact{
		ArtifactID: strings.Repeat("a", 32), RendererID: keeneticdns.ID,
		ArtifactHash: strings.Repeat("b", 64), ContentType: keeneticdns.ContentType, Payload: payload,
	}
}

func (f *fqdnFixture) probe(t *testing.T) application.DeviceInfo {
	t.Helper()
	info, err := f.deployer.Probe(context.Background(), f.connection)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func (f *fqdnFixture) apply(t *testing.T, artifact application.DeployArtifact) {
	t.Helper()
	info := f.probe(t)
	artifact.OwnedFQDNGroups = f.owned
	if err := f.deployer.Deploy(context.Background(), info, f.connection, artifact); err != nil {
		t.Fatal(err)
	}
	if err := f.deployer.Verify(context.Background(), info, f.connection, artifact); err != nil {
		t.Fatalf("the device did not hold what was just applied: %v", err)
	}
	f.owned, _ = f.deployer.DesiredFQDNGroups(info, artifact)
}

// A list that lost a list must stop costing the device its group budget.
// This is the whole reason reconciliation exists: without it every refresh
// leaves the previous groups behind until the 128-group budget is exhausted.
func TestDeployRemovesTheGroupsAnEarlierProfileLeftBehind(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com,ytimg.com", "netflix=netflix.com"))

	groups, routes := fixture.device.groupSnapshot()
	if len(groups) != 2 || len(routes) != 2 {
		t.Fatalf("first apply left groups=%v routes=%v", groups, routes)
	}

	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com,ytimg.com"))

	groups, routes = fixture.device.groupSnapshot()
	want := map[string][]string{keeneticdns.GroupPrefix + "youtube": {"youtube.com", "ytimg.com"}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
	if len(routes) != 1 || routes[keeneticdns.GroupPrefix+"youtube"] != deviceInterface {
		t.Fatalf("routes = %v", routes)
	}
}

// The device refuses to delete a group a route still points at, so withdrawing
// the route first is a correctness requirement rather than tidiness. The order
// is asserted on the device's own command log.
func TestDeployWithdrawsARouteBeforeItRemovesTheGroupItNames(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com", "netflix=netflix.com"))
	before := len(fixture.device.commandLog())

	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com"))

	log := fixture.device.commandLog()[before:]
	stale := keeneticdns.GroupPrefix + "netflix"
	withdrawn := indexOf(log, "no dns-proxy route object-group "+stale+" "+deviceInterface)
	removed := indexOf(log, "no object-group fqdn "+stale)
	if withdrawn < 0 || removed < 0 {
		t.Fatalf("the stale group was not cleaned up: %v", log)
	}
	if withdrawn > removed {
		t.Fatalf("the group was removed before its route was withdrawn: %v", log)
	}
}

// Entries that left the list are removed from a group that stays, so a group
// never accumulates names the list no longer carries.
func TestDeployRemovesTheEntriesAGroupNoLongerCarries(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com,ytimg.com,googlevideo.com"))
	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com,youtu.be"))

	groups, _ := fixture.device.groupSnapshot()
	want := map[string][]string{keeneticdns.GroupPrefix + "youtube": {"youtu.be", "youtube.com"}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
}

// The group budget is shared with whatever else is on the router. A name this
// product did not write is never read as ours, never counted, and never
// removed — including a dns-proxy route pointing at it.
func TestDeployNeverTouchesAGroupItDidNotCreate(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	fixture.device.fqdn["my-own-group"] = []string{"example.com"}
	fixture.device.dnsRoutes["my-own-group"] = "ISP"

	fixture.apply(t, fqdnArtifact(t, "youtube=youtube.com"))

	groups, routes := fixture.device.groupSnapshot()
	if !reflect.DeepEqual(groups["my-own-group"], []string{"example.com"}) {
		t.Fatalf("a foreign group was modified: %v", groups)
	}
	if routes["my-own-group"] != "ISP" {
		t.Fatalf("a foreign route was modified: %v", routes)
	}
	for _, command := range fixture.device.commandLog() {
		if strings.Contains(command, "my-own-group") {
			t.Fatalf("a command named a foreign group: %q", command)
		}
	}
}

// Applying the same artifact twice must change nothing, or every scheduled
// rebuild would rewrite the whole device.
func TestDeployIsIdempotent(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	artifact := fqdnArtifact(t, "youtube=youtube.com,ytimg.com", "discord=discord.com")
	fixture.apply(t, artifact)
	settled := len(fixture.device.commandLog())

	fixture.apply(t, artifact)

	if again := len(fixture.device.commandLog()); again != settled {
		t.Fatalf("a repeated deployment sent %d further commands", again-settled)
	}
}

// A group whose route the operator moved elsewhere is put back on the named
// interface rather than left pointing at a tunnel the deployment did not ask
// for. The old binding is withdrawn first: two routes for one group would be a
// second answer to the same question.
func TestDeployReattachesAGroupRoutedElsewhere(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	artifact := fqdnArtifact(t, "youtube=youtube.com")
	fixture.apply(t, artifact)
	name := keeneticdns.GroupPrefix + "youtube"

	fixture.device.mu.Lock()
	fixture.device.dnsRoutes[name] = "ISP"
	fixture.device.mu.Unlock()
	before := len(fixture.device.commandLog())

	fixture.apply(t, artifact)

	log := fixture.device.commandLog()[before:]
	withdrawn := indexOf(log, "no dns-proxy route object-group "+name+" ISP")
	attached := indexOf(log, "dns-proxy route object-group "+name+" "+deviceInterface+" auto")
	if withdrawn < 0 || attached < 0 || withdrawn > attached {
		t.Fatalf("the route was not moved back in order: %v", log)
	}
	_, routes := fixture.device.groupSnapshot()
	if routes[name] != deviceInterface {
		t.Fatalf("routes = %v", routes)
	}
}

// A route is attached only once its group is complete, so a resolved answer is
// never sent to a group that is still being filled.
func TestReconcileAttachesTheRouteAfterTheGroupIsFilled(t *testing.T) {
	commands := reconcileGroups(
		map[string][]string{"routevane-youtube": {"youtube.com", "ytimg.com"}},
		map[string][]string{},
		map[string]fqdnRoute{},
		deviceInterface,
	)
	want := []string{
		"object-group fqdn routevane-youtube",
		"object-group fqdn routevane-youtube include youtube.com",
		"object-group fqdn routevane-youtube include ytimg.com",
		"dns-proxy route object-group routevane-youtube " + deviceInterface + " auto",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}

// Removals precede additions inside one group, so a heavily changed group never
// briefly holds more than the device's per-group bound.
func TestReconcileRemovesBeforeItAdds(t *testing.T) {
	commands := reconcileGroups(
		map[string][]string{"routevane-a": {"new.example"}},
		map[string][]string{"routevane-a": {"old.example"}},
		map[string]fqdnRoute{"routevane-a": {Interface: deviceInterface, Auto: true}},
		deviceInterface,
	)
	want := []string{
		"no object-group fqdn routevane-a include old.example",
		"object-group fqdn routevane-a include new.example",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}

// Nothing to change is a successful deployment that sends nothing at all.
func TestReconcileSaysNothingWhenTheDeviceAlreadyAgrees(t *testing.T) {
	commands := reconcileGroups(
		map[string][]string{"routevane-a": {"one.example", "two.example"}},
		map[string][]string{"routevane-a": {"two.example", "one.example"}},
		map[string]fqdnRoute{"routevane-a": {Interface: deviceInterface, Auto: true}},
		deviceInterface,
	)
	if len(commands) != 0 {
		t.Fatalf("commands = %v", commands)
	}
}

// DNS-based routes arrived in KeeneticOS 5.0. An older device is refused with
// the version it reported rather than probed for behaviour, and a version that
// cannot be parsed is refused too.
func TestProbeRefusesFirmwareWithoutDNSBasedRoutes(t *testing.T) {
	supported := newFQDNFixture(t, "5.1.2")
	info := supported.probe(t)
	if info.FormatKey != keeneticdns.Version || info.DeployerID != FQDNDeployerID {
		t.Fatalf("info = %#v", info)
	}

	old := newFQDNFixture(t, "4.3.6")
	stale := old.probe(t)
	if stale.FormatKey != "" || stale.FirmwareVersion != "4.3.6" {
		t.Fatalf("an unsupported firmware must be reported, not accepted: %#v", stale)
	}

	for _, version := range []string{"", "unknown", "5", "5.0", "5.0.0", "4.3.6"} {
		if SupportedFQDNFirmware(version) {
			t.Fatalf("%q must not be supported", version)
		}
	}
	if !SupportedFQDNFirmware("5.0.1") || !SupportedFQDNFirmware("5.2 Beta 1") {
		t.Fatal("a supported firmware was refused")
	}
}

// DNS-based routes arrived before the firmware floor the route deployer needs,
// so this deployer must make its own interface check rather than borrow one it
// would never reach on a 5.0.2 device.
func TestProbeChecksTheInterfaceOnFirmwareTheRouteDeployerRefuses(t *testing.T) {
	fixture := newFQDNFixture(t, "5.0.2")
	if SupportedFirmware("5.0.2") {
		t.Fatal("this test is only meaningful below the route deployer's floor")
	}
	fixture.connection.Interface = "Wireguard9"
	_, err := fixture.deployer.Probe(context.Background(), fixture.connection)
	if !errors.Is(err, ErrDeviceAnswer) || !strings.Contains(err.Error(), "Wireguard9") {
		t.Fatalf("probe = %v", err)
	}

	fixture.connection.Interface = deviceInterface
	info := fixture.probe(t)
	if info.FormatKey != keeneticdns.Version {
		t.Fatalf("a 5.0.2 device supports DNS-based routes: %#v", info)
	}
}

// Verification reads the device rather than trusting the write, and names both
// what is missing and what the device holds that the artifact does not.
func TestVerifyReportsDriftAndAnUnexpectedGroup(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	artifact := fqdnArtifact(t, "youtube=youtube.com,ytimg.com")
	fixture.apply(t, artifact)
	info := fixture.probe(t)

	fixture.device.mu.Lock()
	fixture.device.fqdn[keeneticdns.GroupPrefix+"youtube"] = []string{"youtube.com"}
	fixture.device.fqdn[keeneticdns.GroupPrefix+"stale"] = []string{"gone.example"}
	fixture.device.mu.Unlock()

	err := fixture.deployer.Verify(context.Background(), info, fixture.connection, artifact)
	if !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("verify = %v", err)
	}
	message := err.Error()
	if !strings.Contains(message, "ytimg.com") || strings.Contains(message, keeneticdns.GroupPrefix+"stale") {
		t.Fatalf("verify must name owned drift and ignore the unowned extra group: %s", message)
	}
}

// The deployer installs only what the renderer's own validator accepts, so a
// foreign or corrupted file never reaches the device.
func TestFQDNDeployRefusesAForeignArtifact(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	info := fixture.probe(t)
	foreign := fqdnArtifact(t, "youtube=youtube.com")
	foreign.RendererID = "singbox"
	if err := fixture.deployer.Deploy(context.Background(), info, fixture.connection, foreign); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("deploy = %v", err)
	}
	corrupt := fqdnArtifact(t, "youtube=youtube.com")
	corrupt.Payload = []byte("ip route 10.0.0.0 255.0.0.0 Wireguard0\n")
	if err := fixture.deployer.Deploy(context.Background(), info, fixture.connection, corrupt); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("deploy = %v", err)
	}
	if commands := fixture.device.commandLog(); len(commands) != 0 {
		t.Fatalf("a refused artifact reached the device: %v", commands)
	}
}

// A command the device rejects fails the whole deployment, because the device
// answers 200 even for a refusal.
func TestFQDNDeployRefusesADeviceRejection(t *testing.T) {
	fixture := newFQDNFixture(t, "5.1.2")
	info := fixture.probe(t)
	fixture.device.mu.Lock()
	fixture.device.failDeploy = true
	fixture.device.mu.Unlock()
	err := fixture.deployer.Deploy(context.Background(), info, fixture.connection, fqdnArtifact(t, "youtube=youtube.com"))
	if !errors.Is(err, ErrDeviceRefused) {
		t.Fatalf("deploy = %v", err)
	}
}

func indexOf(commands []string, want string) int {
	for index, command := range commands {
		if command == want {
			return index
		}
	}
	return -1
}
