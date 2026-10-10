package keenetic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

func TestFQDNCapacityDriftAfterBackupDoesNotRestoreOverAnotherWriter(t *testing.T) {
	double := newDeviceDouble("5.1.2")
	for i := 0; i < keeneticdns.MaxGroups-1; i++ {
		double.fqdn[fmt.Sprintf("manual-%d", i)] = []string{"manual.example"}
	}
	base := double.handler()
	var added atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base.ServeHTTP(w, r)
		if r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet && added.CompareAndSwap(false, true) {
			double.mu.Lock()
			double.fqdn["manual-created-after-backup"] = []string{"new.example"}
			double.mu.Unlock()
		}
	}))
	defer server.Close()
	deployer := NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
	id := strings.Repeat("1", 32)
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	result, err := application.DeployToDevice(context.Background(), application.DeployRequest{
		Target:     domain.TargetDefinition{ID: "keenetic-dns", RendererID: keeneticdns.ID, FormatKey: keeneticdns.Version},
		Connection: application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface},
		OutputID:   id, ManagedFQDN: ledger, Artifact: outputFQDNArtifact(t, id, "", "claude", "claude.ai"),
	}, application.DeployerRegistry{keeneticdns.ID: deployer}, fqdnBackups{}, application.ClockFunc(time.Now))
	if err == nil || result.Applied || result.RolledBack {
		t.Fatalf("pre-write capacity refusal: result=%#v err=%v", result, err)
	}
	groups, _ := double.groupSnapshot()
	if len(groups) != keeneticdns.MaxGroups || len(groups["manual-created-after-backup"]) != 1 {
		t.Fatalf("pre-write refusal lost an external change: %v", groups)
	}
	if deploys, saves, restores := double.counts(); deploys+saves+restores != 0 || len(ledger.states) != 0 {
		t.Fatalf("refusal changed device or ownership: deploys=%d saves=%d restores=%d", deploys, saves, restores)
	}
}

// A repeated send can submit no commands, then fail read-back or local
// persistence. Restoring its earlier backup would erase another writer's work.
func TestFQDNNoOpFailurePreservesChangesMadeAfterItsBackup(t *testing.T) {
	for _, failure := range []string{"readback", "ownership persistence"} {
		t.Run(failure, func(t *testing.T) {
			double := newDeviceDouble("5.1.2")
			id := strings.Repeat("1", 32)
			artifact := outputFQDNArtifact(t, id, "", "claude", "claude.ai")
			groups, err := keeneticdns.Parse(artifact.Payload)
			if err != nil {
				t.Fatal(err)
			}
			group := groups[0]
			double.fqdn[group.Name], double.fqdnDescriptions[group.Name] = group.Entries, group.Description
			double.dnsRoutes[group.Name], double.dnsAuto[group.Name] = deviceInterface, true
			prior := application.ManagedFQDNOwnership{Endpoint: "http://192.168.1.1", OutputID: id,
				Groups: []application.ManagedFQDNGroup{{Name: group.Name, Entries: group.Entries, Description: group.Description, Interface: deviceInterface, Auto: true}}}
			ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{prior.Endpoint + id: prior}, fail: failure == "ownership persistence"}
			base := double.handler()
			var reads atomic.Int32
			var added atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rci/object-group/fqdn" && reads.Add(1) == 4 && failure == "readback" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				base.ServeHTTP(w, r)
				if r.URL.Path == "/ci/startup-config" && r.Method == http.MethodGet && added.CompareAndSwap(false, true) {
					double.mu.Lock()
					double.fqdn["manual-created-after-backup"] = []string{"manual.example"}
					double.mu.Unlock()
				}
			}))
			defer server.Close()
			deployer := NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			result, err := application.DeployToDevice(context.Background(), application.DeployRequest{
				Target:     domain.TargetDefinition{ID: "keenetic-dns", RendererID: keeneticdns.ID, FormatKey: keeneticdns.Version},
				Connection: application.Connection{URL: prior.Endpoint, Username: deviceUser, Password: devicePassword, Interface: deviceInterface},
				OutputID:   id, ManagedFQDN: ledger, Artifact: artifact,
			}, application.DeployerRegistry{keeneticdns.ID: deployer}, fqdnBackups{}, application.ClockFunc(time.Now))
			wantError := application.ErrVerifyFailed
			if failure == "ownership persistence" {
				wantError = application.ErrOwnershipPersist
			}
			if !errors.Is(err, wantError) || result.Applied || result.RolledBack {
				t.Fatalf("no-op failure result=%#v err=%v", result, err)
			}
			after, _ := double.groupSnapshot()
			if len(after) != 2 || !reflect.DeepEqual(after["manual-created-after-backup"], []string{"manual.example"}) {
				t.Fatalf("no-op recovery lost another writer's change: %v", after)
			}
			if deploys, saves, restores := double.counts(); deploys+saves+restores != 0 || !reflect.DeepEqual(ledger.states[prior.Endpoint+id], prior) {
				t.Fatalf("no-op failure changed device or ownership: %d %d %d %#v", deploys, saves, restores, ledger.states)
			}
		})
	}
}

func TestFQDNRejectsUnsafeObservedCLIInputsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name, entry, attached string
	}{
		{name: "entry with extra arguments", entry: "example.com include other.example", attached: deviceInterface},
		{name: "entry with separator", entry: "example.com;system", attached: deviceInterface},
		{name: "interface with extra arguments", entry: "example.com", attached: "Wireguard0 auto reject"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFQDNFixture(t, "5.1.2")
			name := "routevane-example"
			fixture.device.fqdn[name] = []string{test.entry}
			fixture.device.dnsRoutes[name] = test.attached
			fixture.device.dnsAuto[name] = true
			artifact := fqdnArtifact(t, "example=new.example")
			artifact.OwnedFQDNGroups = []application.ManagedFQDNGroup{{Name: name, Entries: []string{"example.com"}, Interface: deviceInterface, Auto: true}}
			err := fixture.deployer.Deploy(context.Background(), fixture.probe(t), fixture.connection, artifact)
			if !errors.Is(err, ErrDeviceAnswer) || len(fixture.device.commandLog()) != 0 {
				t.Fatalf("unsafe device input reached command submission: err=%v commands=%v", err, fixture.device.commandLog())
			}
		})
	}
}

func TestFQDNReadbackRefusesExcludedEntriesAndGatewayRoutes(t *testing.T) {
	for _, test := range []struct {
		name     string
		excluded bool
		gateway  string
	}{
		{name: "plain interface route"},
		{name: "excluded domain", excluded: true},
		{name: "gateway route", gateway: "192.168.1.254"},
	} {
		t.Run(test.name, func(t *testing.T) {
			double := newDeviceDouble("5.1.2")
			base := double.handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/rci/object-group/fqdn":
					group := map[string]any{"include": []map[string]string{{"address": "example.com"}}}
					if test.excluded {
						group["exclude"] = []map[string]string{{"address": "part.example.com"}}
					}
					writeJSON(w, map[string]any{"routevane-example": group})
				case "/rci/dns-proxy/route":
					writeJSON(w, []map[string]any{{"group": "routevane-example", "interface": deviceInterface, "auto": true, "gateway": test.gateway}})
				default:
					base.ServeHTTP(w, r)
				}
			}))
			defer server.Close()
			deployer := NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			connection := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}
			device, err := deployer.Probe(context.Background(), connection)
			if err != nil {
				t.Fatal(err)
			}
			artifact := fqdnArtifact(t, "example=example.com")
			artifact.OwnedFQDNGroups = []application.ManagedFQDNGroup{{Name: "routevane-example", Entries: []string{"example.com"}, Interface: deviceInterface, Auto: true}}
			err = deployer.Verify(context.Background(), device, connection, artifact)
			if test.excluded || test.gateway != "" {
				if !errors.Is(err, ErrDeviceAnswer) {
					t.Fatalf("accepted a different routing policy: %v", err)
				}
			} else if err != nil {
				t.Fatalf("refused a supported interface route: %v", err)
			}
			if deploys, saves, restores := double.counts(); deploys+saves+restores != 0 {
				t.Fatal("read-back mutated the device")
			}
		})
	}
}

func TestFQDNChangedGroupIsUnboundUntilItsEntriesAreComplete(t *testing.T) {
	wanted := map[string]fqdnGroup{"routevane-example": {Entries: []string{"new.example"}}}
	existing := map[string]fqdnGroup{"routevane-example": {Entries: []string{"old.example"}}}
	routes := map[string]fqdnRoute{"routevane-example": {Interface: deviceInterface, Auto: true}}
	commands := reconcileGroups(wanted, existing, routes, deviceInterface)
	withdrawn := indexOf(commands, "no dns-proxy route object-group routevane-example "+deviceInterface)
	removed := indexOf(commands, "no object-group fqdn routevane-example include old.example")
	added := indexOf(commands, "object-group fqdn routevane-example include new.example")
	attached := indexOf(commands, "dns-proxy route object-group routevane-example "+deviceInterface+" auto")
	if withdrawn < 0 || removed <= withdrawn || added <= removed || attached <= added {
		t.Fatalf("a route can observe a partially replaced group: %v", commands)
	}
}

func TestFQDNConnectionAcceptsNativeInterfaceIDsAndRefusesCLIArguments(t *testing.T) {
	d := NewFQDNDeployer(Options{})
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"Wireguard0", true},
		{"GigabitEthernet0/Vlan7", true},
		{"TunnelGRE0", true},
		{"Wireguard0 auto reject", false},
		{"Wireguard0;system", false},
		{"Wireguard0\nno object-group fqdn manual", false},
	} {
		t.Run(test.value, func(t *testing.T) {
			connection := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: test.value}
			if err := d.ValidateConnection(connection); (err == nil) != test.valid {
				t.Fatalf("interface %q valid=%v err=%v", test.value, test.valid, err)
			}
			if err := d.ValidateStoredConnection(connection.Redacted()); (err == nil) != test.valid {
				t.Fatalf("stored interface %q valid=%v err=%v", test.value, test.valid, err)
			}
		})
	}
}
