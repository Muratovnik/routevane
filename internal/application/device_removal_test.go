package application

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

// removerSpy is an FQDN deployer that can remove what it owns. Its probe and
// verification can refuse per device address, so one test can stand for
// several routers.
type removerSpy struct {
	*spyDeployer
	probeErrs  map[string]error
	previewErr error
	removeErr  error
	verifyErrs map[string]error
	// submitted is what RemoveFQDNGroups reports about its own mutation.
	submitted bool
	removed   [][]ManagedFQDNGroup
}

func newRemoverSpy() *removerSpy {
	return &removerSpy{
		spyDeployer: &spyDeployer{formatKey: removalTestTarget().FormatKey, backup: []byte("startup-config")},
		probeErrs:   map[string]error{}, verifyErrs: map[string]error{}, submitted: true,
	}
}

func (*removerSpy) ID() string { return "keenetic-fqdn-group" }

// FQDNEndpoint canonicalizes as the real deployer does for these inputs: a
// trailing slash is not part of the endpoint.
func (*removerSpy) FQDNEndpoint(connection Connection) (string, error) {
	return strings.TrimSuffix(connection.URL, "/"), nil
}
func (*removerSpy) DesiredFQDNGroups(DeviceInfo, DeployArtifact) ([]ManagedFQDNGroup, error) {
	return nil, nil
}
func (*removerSpy) PreviewFQDNGroups(context.Context, DeviceInfo, Connection, DeployArtifact) ([]string, error) {
	return nil, nil
}
func (r *removerSpy) Probe(ctx context.Context, connection Connection) (DeviceInfo, error) {
	if err := r.probeErrs[connection.URL]; err != nil {
		r.calls = append(r.calls, StepProbe)
		return DeviceInfo{}, err
	}
	return r.spyDeployer.Probe(ctx, connection)
}
func (r *removerSpy) PreviewFQDNRemoval(context.Context, DeviceInfo, Connection, []ManagedFQDNGroup) ([]string, error) {
	r.calls = append(r.calls, StepPreview)
	return nil, r.previewErr
}
func (r *removerSpy) RemoveFQDNGroups(_ context.Context, _ DeviceInfo, _ Connection, owned []ManagedFQDNGroup) (bool, error) {
	r.calls = append(r.calls, StepRemove)
	r.removed = append(r.removed, owned)
	return r.submitted, r.removeErr
}
func (r *removerSpy) VerifyFQDNGroupsRemoved(_ context.Context, _ DeviceInfo, connection Connection, _ []ManagedFQDNGroup) error {
	r.calls = append(r.calls, StepVerify)
	return r.verifyErrs[connection.URL]
}

// memoryFQDNLedger is the FQDN ownership repository in memory.
type memoryFQDNLedger struct {
	groups   map[string][]ManagedFQDNGroup
	replaces int
	err      error
}

func (m *memoryFQDNLedger) ManagedFQDNOwnership(_ context.Context, endpoint, outputID string) (ManagedFQDNOwnership, error) {
	return ManagedFQDNOwnership{Endpoint: endpoint, OutputID: outputID, Groups: m.groups[endpoint+" "+outputID]}, nil
}
func (m *memoryFQDNLedger) ReplaceManagedFQDNOwnership(_ context.Context, state ManagedFQDNOwnership) error {
	m.replaces++
	if m.err != nil {
		return m.err
	}
	if len(state.Groups) == 0 {
		delete(m.groups, state.Endpoint+" "+state.OutputID)
		return nil
	}
	m.groups[state.Endpoint+" "+state.OutputID] = state.Groups
	return nil
}
func (*memoryFQDNLedger) RetireManagedFQDNOwnership(context.Context, string, string) error {
	return nil
}

func removalTestTarget() domain.TargetDefinition {
	return domain.TargetDefinition{ID: "keenetic-dns", FormatKey: "keenetic-fqdn-group-v1", RendererID: "keenetic-fqdn-group"}
}

func ownedGroup(name string) ManagedFQDNGroup {
	return ManagedFQDNGroup{Name: name, Entries: []string{name + ".example"}, Interface: "Wireguard0", Auto: true}
}

func fqdnRemovalRequest(ledger *memoryFQDNLedger) RemovalRequest {
	return RemovalRequest{
		Target:     removalTestTarget(),
		Connection: Connection{URL: "http://10.255.0.1", Username: "admin", Password: deployTestPassword, Interface: "Wireguard0"},
		OutputID:   managedOutputA, ManagedFQDN: ledger,
	}
}

func eventSteps(result DeployResult) string {
	steps := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		steps = append(steps, event.Step+"="+event.Outcome)
	}
	return strings.Join(steps, ",")
}

func TestAnFQDNRemovalFollowsTheSendLifecycleAndEmptiesOnlyItsOwnLedger(t *testing.T) {
	for _, test := range []struct {
		name string
		// arrange changes the spy and the ledger before the removal.
		arrange   func(*removerSpy, *memoryFQDNLedger)
		wantCalls string
		wantSteps string
		wantCode  string
		applied   bool
		rolled    bool
		// ledgerKept is true when this output's rows must survive.
		ledgerKept bool
	}{
		{name: "removed and verified", wantCalls: "probe,preview,backup,remove,verify",
			wantSteps: "probe=success,backup=success,remove=success,verify=success,ownership=success", applied: true},
		{name: "password refused at the probe", arrange: func(r *removerSpy, _ *memoryFQDNLedger) {
			r.probeErrs["http://10.255.0.1"] = fmt.Errorf("login: %w", ErrDeviceAuthRejected)
		}, wantCalls: "probe", wantSteps: "probe=failed", wantCode: "device_auth_rejected", ledgerKept: true},
		{name: "unremovable owned object found by the preview", arrange: func(r *removerSpy, _ *memoryFQDNLedger) {
			r.previewErr = fmt.Errorf("route policy: %w", ErrDeviceAnswerInvalid)
		}, wantCalls: "probe,preview", wantSteps: "probe=success", wantCode: "device_answer_invalid", ledgerKept: true},
		{name: "verification failed after a submitted removal", arrange: func(r *removerSpy, _ *memoryFQDNLedger) {
			r.verifyErrs["http://10.255.0.1"] = errors.New("group still present")
		}, wantCalls: "probe,preview,backup,remove,verify,rollback",
			wantSteps: "probe=success,backup=success,remove=success,verify=failed,rollback=success", wantCode: "verify_failed", rolled: true, ledgerKept: true},
		{name: "verification failed with nothing submitted", arrange: func(r *removerSpy, _ *memoryFQDNLedger) {
			r.submitted = false
			r.verifyErrs["http://10.255.0.1"] = errors.New("another writer added it back")
		}, wantCalls: "probe,preview,backup,remove,verify",
			wantSteps: "probe=success,backup=success,remove=success,verify=failed", wantCode: "verify_failed", ledgerKept: true},
		{name: "command rejected mid-removal", arrange: func(r *removerSpy, _ *memoryFQDNLedger) {
			r.removeErr = fmt.Errorf("batch: %w", ErrDeviceCommandRejected)
		}, wantCalls: "probe,preview,backup,remove,rollback",
			wantSteps: "probe=success,backup=success,remove=failed,rollback=success", wantCode: "device_command_rejected", rolled: true, ledgerKept: true},
		{name: "ledger write failed after verification", arrange: func(_ *removerSpy, l *memoryFQDNLedger) {
			l.err = errors.New("disk full")
		}, wantCalls: "probe,preview,backup,remove,verify,rollback",
			wantSteps: "probe=success,backup=success,remove=success,verify=success,ownership=failed,rollback=success", wantCode: "device_cleanup_failed", rolled: true, ledgerKept: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			remover := newRemoverSpy()
			ledger := &memoryFQDNLedger{groups: map[string][]ManagedFQDNGroup{
				"http://10.255.0.1 " + managedOutputA: {ownedGroup("routevane-a")},
				"http://10.255.0.1 " + managedOutputB: {ownedGroup("routevane-b")},
			}}
			if test.arrange != nil {
				test.arrange(remover, ledger)
			}
			result, err := RemoveFromDevice(context.Background(), fqdnRemovalRequest(ledger), DeployerRegistry{remover.ID(): remover}, &memoryBackups{}, fixedClock())
			if got := strings.Join(remover.calls, ","); got != test.wantCalls {
				t.Fatalf("calls = %s, want %s", got, test.wantCalls)
			}
			if got := eventSteps(result); got != test.wantSteps {
				t.Fatalf("steps = %s, want %s", got, test.wantSteps)
			}
			if DeviceCleanupFailureCode(err) != test.wantCode || result.Applied != test.applied || result.RolledBack != test.rolled {
				t.Fatalf("err=%v code=%s applied=%v rolled=%v", err, DeviceCleanupFailureCode(err), result.Applied, result.RolledBack)
			}
			if _, kept := ledger.groups["http://10.255.0.1 "+managedOutputA]; kept != test.ledgerKept {
				t.Fatalf("own ledger kept=%v, want %v", kept, test.ledgerKept)
			}
			if len(ledger.groups["http://10.255.0.1 "+managedOutputB]) != 1 {
				t.Fatal("another output's ledger changed")
			}
			if test.applied && !reflect.DeepEqual(remover.removed, [][]ManagedFQDNGroup{{ownedGroup("routevane-a")}}) {
				t.Fatalf("removal was asked to remove %v", remover.removed)
			}
		})
	}
}

// A surface where the output owns nothing needs no device at all.
func TestARemovalWithNothingOwnedContactsNoDevice(t *testing.T) {
	remover := newRemoverSpy()
	ledger := &memoryFQDNLedger{groups: map[string][]ManagedFQDNGroup{}}
	result, err := RemoveFromDevice(context.Background(), fqdnRemovalRequest(ledger), DeployerRegistry{remover.ID(): remover}, &memoryBackups{}, fixedClock())
	if err != nil || !result.Applied || len(result.Events) != 0 || len(remover.calls) != 0 || ledger.replaces != 0 {
		t.Fatalf("result=%#v err=%v calls=%v replaces=%d", result, err, remover.calls, ledger.replaces)
	}
	if err := PreviewRemoval(context.Background(), fqdnRemovalRequest(ledger), DeployerRegistry{remover.ID(): remover}, fixedClock()); err != nil || len(remover.calls) != 0 {
		t.Fatalf("preview err=%v calls=%v", err, remover.calls)
	}
}

// A deployer that keeps no ownership ledger never recorded anything as
// Routevane's, so it has nothing it may remove.
func TestARemovalRefusesADeployerWithoutOwnership(t *testing.T) {
	plain := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("config")}
	request := RemovalRequest{Target: deployTestTarget(), Connection: deployTestRequest().Connection, OutputID: managedOutputA}
	_, err := RemoveFromDevice(context.Background(), request, DeployerRegistry{plain.ID(): plain}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrRemovalUnsupported) || DeviceCleanupFailureCode(err) != "device_cleanup_unavailable" || len(plain.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, plain.calls)
	}
}

// Static routes: the output's claims go; a route goes only when Routevane
// created it and no other output claims it. A shared route and one the router
// had before Routevane stay, and the ledger keeps only the other claims.
func TestAStaticRouteRemovalKeepsSharedAndPreexistingRoutes(t *testing.T) {
	own, shared, preexisting, foreign := managedPrefix("198.51.100.20/32"), managedPrefix("192.0.2.10/32"), managedPrefix("203.0.113.0/24"), managedPrefix("10.9.9.0/24")
	prior := ManagedRouteOwnership{
		Scope: managedScope(),
		Routes: []ManagedRoute{
			{Prefix: shared, CreatedByRoutevane: true},
			{Prefix: own, CreatedByRoutevane: true},
			{Prefix: preexisting},
		},
		Claims: []ManagedRouteClaim{
			{OutputID: managedOutputA, Prefix: shared}, {OutputID: managedOutputA, Prefix: own}, {OutputID: managedOutputA, Prefix: preexisting},
			{OutputID: managedOutputB, Prefix: shared},
		},
	}
	present := managedSpecs(foreign, shared, own, preexisting)
	afterwards := managedSpecs(foreign, shared, preexisting)
	base := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{spyDeployer: base, current: [][]ManagedRouteSpec{present, afterwards}}
	ledger := &memoryManagedRoutes{state: prior}
	request := RemovalRequest{Target: deployTestTarget(), Connection: deployTestRequest().Connection, OutputID: managedOutputA, ManagedRoutes: ledger}
	result, err := RemoveFromDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if err != nil || !result.Applied || eventSteps(result) != "probe=success,backup=success,remove=success,verify=success,ownership=success" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(deployer.mutation, ManagedRouteMutation{Remove: []netip.Prefix{own}}) {
		t.Fatalf("mutation = %#v", deployer.mutation)
	}
	want := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: shared, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputB, Prefix: shared}},
	}
	if !reflect.DeepEqual(ledger.state, want) {
		t.Fatalf("ledger = %#v", ledger.state)
	}
}

// A failed read of the route table is refused before any change and restores
// nothing over another writer's later edits.
func TestAStaticRouteRemovalWhoseReadFailsChangesNothing(t *testing.T) {
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: managedPrefix("198.51.100.20/32"), CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: managedPrefix("198.51.100.20/32")}},
	}
	base := &spyDeployer{formatKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{spyDeployer: base}
	ledger := &memoryManagedRoutes{state: prior}
	request := RemovalRequest{Target: deployTestTarget(), Connection: deployTestRequest().Connection, OutputID: managedOutputA, ManagedRoutes: ledger}
	result, err := RemoveFromDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrDeviceUnchanged) || result.Applied || result.RolledBack || deployer.applyCalls != 0 || ledger.replaces != 0 {
		t.Fatalf("result=%#v err=%v apply=%d replaces=%d", result, err, deployer.applyCalls, ledger.replaces)
	}
}

func TestDeviceCleanupFailureCodeKeepsTheSendVocabulary(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrDeviceCleanupUnavailable, "device_cleanup_unavailable"},
		{fmt.Errorf("%w: x", ErrRemovalUnsupported), "device_cleanup_unavailable"},
		{fmt.Errorf("%w: %w", ErrDeployFailed, ErrDeviceAuthRejected), "device_auth_rejected"},
		{fmt.Errorf("%w: x", ErrVerifyFailed), "verify_failed"},
		{fmt.Errorf("%w: x", ErrDeployFailed), "device_cleanup_failed"},
		{fmt.Errorf("%w: x", ErrOwnershipPersist), "device_cleanup_failed"},
	} {
		if got := DeviceCleanupFailureCode(test.err); got != test.want {
			t.Errorf("code(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
