package application

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	managedOutputA = "11111111111111111111111111111111"
	managedOutputB = "22222222222222222222222222222222"
)

func managedPrefix(value string) netip.Prefix { return netip.MustParsePrefix(value) }

func managedSpecs(prefixes ...netip.Prefix) []ManagedRouteSpec {
	result := make([]ManagedRouteSpec, 0, len(prefixes))
	for _, prefix := range prefixes {
		result = append(result, ManagedRouteSpec{Prefix: prefix})
	}
	return result
}

func labeledManagedSpec(prefix netip.Prefix, labels ...string) ManagedRouteSpec {
	labels = domain.StableStrings(labels)
	return ManagedRouteSpec{Prefix: prefix, Labels: labels, Description: CompactManagedRouteDescription(labels)}
}

func TestManagedRouteDescriptionIsDeterministicBoundedAndUTF8Safe(t *testing.T) {
	if got := CompactManagedRouteDescription([]string{"(Видео/YouTube)"}); got != "(Видео/YouTube)" {
		t.Fatalf("one label = %q", got)
	}
	if got := CompactManagedRouteDescription([]string{"(Игры/Discord)", "(Видео/YouTube)", "(Игры/Discord)"}); got != "(Видео/YouTube) +1" {
		t.Fatalf("multiple labels = %q", got)
	}
	long := "(" + strings.Repeat("東京", 80) + "/Список)"
	got := CompactManagedRouteDescription([]string{long})
	if len(got) > MaxManagedRouteDescriptionBytes || !strings.HasPrefix(got, "(") || !strings.HasSuffix(got, ")") || !utf8.ValidString(got) {
		t.Fatalf("truncated label = %q (%d bytes)", got, len(got))
	}
	for _, ambiguous := range []string{"(Видео/YouTube/Extra)", "(Видео)/YouTube)", "(Видео/You+Tube)", "(Видео\\Личное/YouTube)", "(Видео/ YouTube)"} {
		if got := CompactManagedRouteDescription([]string{ambiguous}); got != "" {
			t.Fatalf("ambiguous label %q produced %q", ambiguous, got)
		}
	}
}

func TestManagedRouteDescriptionsUnionAcrossClaimsAndFollowTheLastClaim(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	youtube := labeledManagedSpec(prefix, "(Видео/YouTube)")
	discord := labeledManagedSpec(prefix, "(Игры/Discord)")
	mutation, one, err := reconcileManagedRoutes(ManagedRouteOwnership{Scope: managedScope()}, managedOutputA, []ManagedRouteSpec{youtube}, nil)
	if err != nil || !reflect.DeepEqual(mutation.Upsert, []ManagedRouteSpec{youtube}) {
		t.Fatalf("first mutation=%#v ownership=%#v err=%v", mutation, one, err)
	}
	mutation, both, err := reconcileManagedRoutes(one, managedOutputB, []ManagedRouteSpec{discord}, []ManagedRouteSpec{{Prefix: prefix, Description: youtube.Description}})
	if err != nil {
		t.Fatal(err)
	}
	wantBoth := labeledManagedSpec(prefix, "(Видео/YouTube)", "(Игры/Discord)")
	if !reflect.DeepEqual(mutation.Upsert, []ManagedRouteSpec{wantBoth}) || both.Routes[0].Description != wantBoth.Description {
		t.Fatalf("combined mutation=%#v ownership=%#v", mutation, both)
	}
	mutation, onlyDiscord, err := reconcileManagedRoutes(both, managedOutputA, nil, []ManagedRouteSpec{{Prefix: prefix, Description: wantBoth.Description}})
	if err != nil || !reflect.DeepEqual(mutation.Upsert, []ManagedRouteSpec{discord}) || onlyDiscord.Routes[0].Description != discord.Description {
		t.Fatalf("last claim mutation=%#v ownership=%#v err=%v", mutation, onlyDiscord, err)
	}
}

func TestAClaimOnAForeignRouteNeverRewritesItsDescription(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	desired := labeledManagedSpec(prefix, "(Видео/YouTube)")
	mutation, next, err := reconcileManagedRoutes(ManagedRouteOwnership{Scope: managedScope()}, managedOutputA, []ManagedRouteSpec{desired}, []ManagedRouteSpec{{Prefix: prefix, Description: "operator route"}})
	if err != nil || len(mutation.Upsert)+len(mutation.Remove) != 0 || len(next.Routes) != 1 || next.Routes[0].CreatedByRoutevane || next.Routes[0].Description != "" {
		t.Fatalf("mutation=%#v ownership=%#v err=%v", mutation, next, err)
	}
}

func TestLegacyEmptyClaimDoesNotClearTheLastKnownManagedDescription(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	description := "(Видео/YouTube)"
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: prefix, Description: description, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputB, Prefix: prefix}},
	}
	mutation, next, err := reconcileManagedRoutes(prior, managedOutputA, nil, []ManagedRouteSpec{{Prefix: prefix, Description: description}})
	if err != nil || len(mutation.Upsert)+len(mutation.Remove) != 0 || next.Routes[0].Description != description {
		t.Fatalf("mutation=%#v ownership=%#v err=%v", mutation, next, err)
	}
}

func managedScope() ManagedRouteScope {
	return ManagedRouteScope{Endpoint: "http://192.168.1.1", TargetID: "keenetic", Interface: "Wireguard0"}
}

func TestManagedRouteReconciliationPreservesForeignAndPreexistingRoutes(t *testing.T) {
	foreign := managedPrefix("10.9.9.0/24")
	preexisting := managedPrefix("192.0.2.10/32")
	created := managedPrefix("198.51.100.20/32")
	prior := ManagedRouteOwnership{Scope: managedScope()}

	mutation, first, err := reconcileManagedRoutes(prior, managedOutputA, managedSpecs(preexisting, created), managedSpecs(foreign, preexisting))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutation, ManagedRouteMutation{Upsert: managedSpecs(created)}) {
		t.Fatalf("first mutation = %#v", mutation)
	}
	createdFlags := map[netip.Prefix]bool{}
	for _, route := range first.Routes {
		createdFlags[route.Prefix] = route.CreatedByRoutevane
	}
	if createdFlags[preexisting] || !createdFlags[created] {
		t.Fatalf("first ownership = %#v", first)
	}

	replacement := managedPrefix("203.0.113.5/32")
	mutation, next, err := reconcileManagedRoutes(first, managedOutputA, managedSpecs(replacement), managedSpecs(foreign, preexisting, created))
	if err != nil {
		t.Fatal(err)
	}
	want := ManagedRouteMutation{Upsert: managedSpecs(replacement), Remove: []netip.Prefix{created}}
	if !reflect.DeepEqual(mutation, want) {
		t.Fatalf("replacement mutation = %#v, want %#v", mutation, want)
	}
	for _, removed := range mutation.Remove {
		if removed == preexisting || removed == foreign {
			t.Fatalf("foreign or pre-existing route became removable: %v", removed)
		}
	}
	if len(next.Routes) != 1 || next.Routes[0].Prefix != replacement || !next.Routes[0].CreatedByRoutevane {
		t.Fatalf("next ownership = %#v", next)
	}
}

func TestManagedRouteClaimsRemoveOnlyAfterTheLastOutputLeaves(t *testing.T) {
	shared := managedPrefix("192.0.2.10/32")
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: shared, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: shared}},
	}
	mutation, both, err := reconcileManagedRoutes(prior, managedOutputB, managedSpecs(shared), managedSpecs(shared))
	if err != nil {
		t.Fatal(err)
	}
	if len(mutation.Upsert)+len(mutation.Remove) != 0 || len(both.Claims) != 2 {
		t.Fatalf("shared claim = mutation %#v ownership %#v", mutation, both)
	}

	otherA := managedPrefix("198.51.100.10/32")
	mutation, onlyB, err := reconcileManagedRoutes(both, managedOutputA, managedSpecs(otherA), managedSpecs(shared))
	if err != nil {
		t.Fatal(err)
	}
	if len(mutation.Remove) != 0 {
		t.Fatalf("first claimant removed shared route: %#v", mutation)
	}
	otherB := managedPrefix("203.0.113.10/32")
	mutation, _, err = reconcileManagedRoutes(onlyB, managedOutputB, managedSpecs(otherB), managedSpecs(shared, otherA))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutation.Remove, []netip.Prefix{shared}) {
		t.Fatalf("last claimant mutation = %#v", mutation)
	}
}

func TestRepeatedManagedRouteDeploymentIsANoop(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: prefix, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: prefix}},
	}
	mutation, next, err := reconcileManagedRoutes(prior, managedOutputA, managedSpecs(prefix), managedSpecs(prefix))
	if err != nil {
		t.Fatal(err)
	}
	if len(mutation.Upsert)+len(mutation.Remove) != 0 || !reflect.DeepEqual(next, prior) {
		t.Fatalf("repeat = mutation %#v ownership %#v", mutation, next)
	}
}

func TestManagedRouteReconciliationRemovesOwnedStaleRoutesWhenAnOutputHasNoRoutes(t *testing.T) {
	owned := managedPrefix("192.0.2.10/32")
	foreign := managedPrefix("10.9.9.0/24")
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: owned, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: owned}},
	}

	mutation, next, err := reconcileManagedRoutes(prior, managedOutputA, nil, managedSpecs(owned, foreign))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutation, ManagedRouteMutation{Remove: []netip.Prefix{owned}}) {
		t.Fatalf("mutation = %#v", mutation)
	}
	if len(next.Routes) != 0 || len(next.Claims) != 0 {
		t.Fatalf("empty desired output retained ownership: %#v", next)
	}
}

func TestManagedRouteOwnershipRefusesIPv6BeforeItCanAuthorizeAChange(t *testing.T) {
	state := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: managedPrefix("2001:db8::/32"), CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: managedPrefix("2001:db8::/32")}},
	}
	if err := state.Validate(); err == nil {
		t.Fatal("IPv6 ownership was accepted")
	}
	if _, _, err := reconcileManagedRoutes(ManagedRouteOwnership{Scope: managedScope()}, managedOutputA, managedSpecs(managedPrefix("2001:db8::/32")), nil); err == nil {
		t.Fatal("IPv6 desired route was accepted")
	}
}

type memoryManagedRoutes struct {
	state    ManagedRouteOwnership
	replaces int
	err      error
}

func (m *memoryManagedRoutes) ManagedRouteOwnership(_ context.Context, scope ManagedRouteScope) (ManagedRouteOwnership, error) {
	if m.state.Scope == (ManagedRouteScope{}) {
		return ManagedRouteOwnership{Scope: scope}, nil
	}
	return m.state, nil
}

func (m *memoryManagedRoutes) ReplaceManagedRouteOwnership(_ context.Context, state ManagedRouteOwnership) error {
	m.replaces++
	if m.err != nil {
		return m.err
	}
	m.state = state
	return nil
}

func (m *memoryManagedRoutes) RetireManagedRouteOwnership(context.Context, ManagedRouteScope) error {
	return nil
}

type managedSpyDeployer struct {
	*spyDeployer
	desired  []ManagedRouteSpec
	current  [][]ManagedRouteSpec
	mutation ManagedRouteMutation
}

func (*managedSpyDeployer) ManagedRouteScope(domain.TargetProfile, Connection) (ManagedRouteScope, error) {
	return managedScope(), nil
}

func (m *managedSpyDeployer) DesiredManagedRoutes(DeployArtifact) ([]ManagedRouteSpec, error) {
	return m.desired, nil
}

func (m *managedSpyDeployer) CurrentManagedRoutes(context.Context, DeviceInfo, Connection) ([]ManagedRouteSpec, error) {
	if len(m.current) == 0 {
		return nil, errors.New("unexpected route read")
	}
	current := m.current[0]
	m.current = m.current[1:]
	return current, nil
}

func (m *managedSpyDeployer) ApplyManagedRoutes(_ context.Context, _ DeviceInfo, _ Connection, mutation ManagedRouteMutation) error {
	m.mutation = mutation
	return nil
}

func TestFailedManagedRouteVerificationRollsBackWithoutChangingTheLedger(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	base := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{spyDeployer: base, desired: managedSpecs(prefix), current: [][]ManagedRouteSpec{nil, nil}}
	ledger := &memoryManagedRoutes{}
	request := deployTestRequest()
	request.OutputID = managedOutputA
	request.ManagedRoutes = ledger
	result, err := DeployToDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrVerifyFailed) || !result.RolledBack {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if ledger.replaces != 0 || ledger.state.Scope != (ManagedRouteScope{}) {
		t.Fatalf("failed verification changed ledger: %#v", ledger)
	}
	if !reflect.DeepEqual(deployer.mutation.Upsert, managedSpecs(prefix)) {
		t.Fatalf("mutation = %#v", deployer.mutation)
	}
}

func TestManagedRouteDescriptionMismatchRollsBackWithoutChangingTheLedger(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	desired := labeledManagedSpec(prefix, "(Видео/YouTube)")
	base := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{
		spyDeployer: base,
		desired:     []ManagedRouteSpec{desired},
		current:     [][]ManagedRouteSpec{nil, {{Prefix: prefix, Description: ""}}},
	}
	ledger := &memoryManagedRoutes{}
	request := deployTestRequest()
	request.OutputID = managedOutputA
	request.ManagedRoutes = ledger
	result, err := DeployToDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrVerifyFailed) || !result.RolledBack || ledger.replaces != 0 {
		t.Fatalf("result=%#v ledger=%#v err=%v", result, ledger, err)
	}
	if !reflect.DeepEqual(deployer.mutation.Upsert, []ManagedRouteSpec{desired}) {
		t.Fatalf("mutation = %#v", deployer.mutation)
	}
}

func TestManagedRoutePersistenceFailureRollsBackAndKeepsTheOldLedger(t *testing.T) {
	oldPrefix := managedPrefix("192.0.2.10/32")
	newPrefix := managedPrefix("198.51.100.20/32")
	prior := ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: oldPrefix, CreatedByRoutevane: true}},
		Claims: []ManagedRouteClaim{{OutputID: managedOutputA, Prefix: oldPrefix}},
	}
	base := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{spyDeployer: base, desired: managedSpecs(newPrefix), current: [][]ManagedRouteSpec{managedSpecs(oldPrefix), managedSpecs(newPrefix)}}
	ledger := &memoryManagedRoutes{state: prior, err: errors.New("disk full")}
	request := deployTestRequest()
	request.OutputID = managedOutputA
	request.ManagedRoutes = ledger
	result, err := DeployToDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock())
	if !errors.Is(err, ErrOwnershipPersist) || !result.RolledBack {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if ledger.replaces != 1 || !reflect.DeepEqual(ledger.state, prior) {
		t.Fatalf("persistence failure changed ledger: %#v", ledger)
	}
	if !reflect.DeepEqual(deployer.mutation, ManagedRouteMutation{Upsert: managedSpecs(newPrefix), Remove: []netip.Prefix{oldPrefix}}) {
		t.Fatalf("mutation = %#v", deployer.mutation)
	}
	if strings.Join(base.calls, ",") != "probe,backup,rollback" {
		t.Fatalf("lifecycle = %v", base.calls)
	}
}

func TestCorruptManagedRouteLedgerPreventsDeploymentAndWrites(t *testing.T) {
	prefix := managedPrefix("192.0.2.10/32")
	base := &spyDeployer{profileKey: "keenetic-bat-ipv4-v1", backup: []byte("startup-config")}
	deployer := &managedSpyDeployer{spyDeployer: base, desired: managedSpecs(prefix)}
	ledger := &memoryManagedRoutes{state: ManagedRouteOwnership{
		Scope:  managedScope(),
		Routes: []ManagedRoute{{Prefix: prefix, CreatedByRoutevane: true}},
	}}
	request := deployTestRequest()
	request.OutputID = managedOutputA
	request.ManagedRoutes = ledger
	if _, err := DeployToDevice(context.Background(), request, DeployerRegistry{deployer.ID(): deployer}, &memoryBackups{}, fixedClock()); !errors.Is(err, ErrDeployComposition) {
		t.Fatalf("err=%v", err)
	}
	if ledger.replaces != 0 || strings.Join(base.calls, ",") != "probe" {
		t.Fatalf("corrupt ledger changed device or ledger: replaces=%d calls=%v", ledger.replaces, base.calls)
	}
}
