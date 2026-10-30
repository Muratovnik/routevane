package keenetic

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

var _ application.ManagedFQDNRemover = (*FQDNDeployer)(nil)

func removeFQDNOutput(f *fqdnFixture, ledger *fqdnLedger, id string) (application.DeployResult, error) {
	return application.RemoveFromDevice(context.Background(), application.RemovalRequest{
		Target:     domain.TargetDefinition{ID: "keenetic-dns", RendererID: keeneticdns.ID, FormatKey: keeneticdns.Version},
		Connection: f.connection, OutputID: id, ManagedFQDN: ledger,
	}, application.DeployerRegistry{keeneticdns.ID: f.deployer}, fqdnBackups{}, application.ClockFunc(time.Now))
}

// ADR 0060: a removal takes exactly the groups one output's ledger names, route
// first, and leaves another output's groups and the operator's own untouched.
func TestFQDNRemovalTakesOnlyTheOutputsOwnGroupsRouteFirst(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	f.device.fqdn["routevane-manual"] = []string{"manual.example"}
	f.device.dnsRoutes["routevane-manual"] = "ISP"
	a, b := strings.Repeat("1", 32), strings.Repeat("2", 32)
	for _, item := range []struct{ id, value string }{{a, "a.example"}, {b, "b.example"}} {
		if result, err := applyFQDNOutput(f, ledger, item.id, outputFQDNArtifact(t, item.id, "", "same-list", item.value)); err != nil || !result.Applied {
			t.Fatalf("apply=%#v %v", result, err)
		}
	}
	owned, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	if len(owned.Groups) != 1 {
		t.Fatalf("owned = %#v", owned)
	}
	name := owned.Groups[0].Name
	groupsBefore, routesBefore := f.device.groupSnapshot()

	preview, err := f.deployer.PreviewFQDNRemoval(context.Background(), f.probe(t), f.connection, owned.Groups)
	want := []string{"no dns-proxy route object-group " + name + " " + deviceInterface, "no object-group fqdn " + name}
	if err != nil || !reflect.DeepEqual(preview, want) {
		t.Fatalf("preview = %v err=%v, want %v", preview, err, want)
	}
	if groups, routes := f.device.groupSnapshot(); !reflect.DeepEqual(groups, groupsBefore) || !reflect.DeepEqual(routes, routesBefore) {
		t.Fatal("a removal preview wrote to the device")
	}

	logged := len(f.device.commandLog())
	result, err := removeFQDNOutput(f, ledger, a)
	if err != nil || !result.Applied || result.RolledBack {
		t.Fatalf("removal = %#v err=%v", result, err)
	}
	if submitted := f.device.commandLog()[logged:]; !reflect.DeepEqual(submitted, append(want, "system configuration save")) {
		t.Fatalf("submitted = %v", submitted)
	}
	groups, routes := f.device.groupSnapshot()
	delete(groupsBefore, name)
	delete(routesBefore, name)
	if !reflect.DeepEqual(groups, groupsBefore) || !reflect.DeepEqual(routes, routesBefore) {
		t.Fatalf("after removal groups=%v routes=%v", groups, routes)
	}
	if left, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a); len(left.Groups) != 0 {
		t.Fatalf("the removed output still owns %v", left.Groups)
	}
	if kept, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", b); len(kept.Groups) != 1 {
		t.Fatalf("another output's ownership changed: %v", kept.Groups)
	}

	// The ledger is now empty for this output: a repeated removal reaches no
	// device at all.
	logged = len(f.device.commandLog())
	if result, err := removeFQDNOutput(f, ledger, a); err != nil || !result.Applied || len(result.Events) != 0 || len(f.device.commandLog()) != logged {
		t.Fatalf("repeated removal = %#v err=%v commands=%v", result, err, f.device.commandLog()[logged:])
	}
}

// A group the ledger owns but the device no longer holds needs nothing: the
// removal submits no command and still verifies.
func TestFQDNRemovalOfGroupsAlreadyGoneSubmitsNothing(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	a := strings.Repeat("1", 32)
	if _, err := applyFQDNOutput(f, ledger, a, outputFQDNArtifact(t, a, "", "same-list", "a.example")); err != nil {
		t.Fatal(err)
	}
	owned, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	f.device.mu.Lock()
	delete(f.device.dnsRoutes, owned.Groups[0].Name)
	delete(f.device.fqdn, owned.Groups[0].Name)
	f.device.mu.Unlock()
	submitted, err := f.deployer.RemoveFQDNGroups(context.Background(), f.probe(t), f.connection, owned.Groups)
	if submitted || err != nil {
		t.Fatalf("submitted=%v err=%v", submitted, err)
	}
	if err := f.deployer.VerifyFQDNGroupsRemoved(context.Background(), f.probe(t), f.connection, owned.Groups); err != nil {
		t.Fatal(err)
	}
}

// Verification reads the device: an owned group still there, or a route that
// still names one, fails it.
func TestFQDNRemovalVerificationFindsWhatIsStillThere(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	a := strings.Repeat("1", 32)
	if _, err := applyFQDNOutput(f, ledger, a, outputFQDNArtifact(t, a, "", "same-list", "a.example")); err != nil {
		t.Fatal(err)
	}
	owned, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	err := f.deployer.VerifyFQDNGroupsRemoved(context.Background(), f.probe(t), f.connection, owned.Groups)
	if err == nil || !strings.Contains(err.Error(), owned.Groups[0].Name) {
		t.Fatalf("verification of a present group = %v", err)
	}
}
