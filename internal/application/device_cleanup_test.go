package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	cleanupProfile = "33333333333333333333333333333333"
	cleanupOutputC = "44444444444444444444444444444444"
)

var cleanupMoment = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

type cleanupPublicationFake struct {
	profiles map[string]Profile
	outputs  map[string]Output
	retired  []string
}

func (f *cleanupPublicationFake) Profile(_ context.Context, id string) (Profile, error) {
	profile, ok := f.profiles[id]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return profile, nil
}
func (f *cleanupPublicationFake) Output(_ context.Context, id string) (Output, error) {
	output, ok := f.outputs[id]
	if !ok {
		return Output{}, ErrNotFound
	}
	return output, nil
}
func (f *cleanupPublicationFake) Outputs(_ context.Context, profileID string) ([]Output, error) {
	outputs := make([]Output, 0)
	for _, output := range f.outputs {
		if output.ProfileID == profileID {
			outputs = append(outputs, output)
		}
	}
	slices.SortFunc(outputs, func(a, b Output) int { return strings.Compare(a.ID, b.ID) })
	return outputs, nil
}
func (*cleanupPublicationFake) TargetDefinition(id string) (domain.TargetDefinition, error) {
	for _, target := range []domain.TargetDefinition{removalTestTarget(), deployTestTarget()} {
		if target.ID == id {
			return target, nil
		}
	}
	return domain.TargetDefinition{}, ErrNotFound
}
func (f *cleanupPublicationFake) RemoveOutput(_ context.Context, id string) (time.Time, error) {
	f.retired = append(f.retired, "remove "+id)
	return cleanupMoment, nil
}
func (f *cleanupPublicationFake) DeleteProfile(_ context.Context, id string) (time.Time, error) {
	f.retired = append(f.retired, "delete "+id)
	return cleanupMoment, nil
}

type cleanupDevicesFake struct {
	cards       []DeviceCard
	credentials map[string]string
}

func (f *cleanupDevicesFake) DeviceCards(context.Context) ([]DeviceCard, error) { return f.cards, nil }
func (f *cleanupDevicesFake) DeviceCredential(_ context.Context, id string) (string, error) {
	secret, ok := f.credentials[id]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

type deliveredFake struct {
	fqdn   map[string][]string
	routes map[string][]ManagedRouteScope
}

func (f deliveredFake) OutputFQDNEndpoints(_ context.Context, id string) ([]string, error) {
	return f.fqdn[id], nil
}
func (f deliveredFake) OutputManagedRouteScopes(_ context.Context, id string) ([]ManagedRouteScope, error) {
	return f.routes[id], nil
}

type cleanupJournalFake struct {
	begun, completed []DeviceCleanupAttempt
}

func (j *cleanupJournalFake) BeginDeviceCleanupAttempt(_ context.Context, attempt DeviceCleanupAttempt) error {
	j.begun = append(j.begun, attempt)
	return nil
}
func (j *cleanupJournalFake) CompleteDeviceCleanupAttempt(_ context.Context, attempt DeviceCleanupAttempt) error {
	j.completed = append(j.completed, attempt)
	return nil
}

// routeScopeSpy canonicalizes a route scope from the connection, as the real
// deployer does, so a connection on another interface is another surface.
type routeScopeSpy struct{ *managedSpyDeployer }

func (*routeScopeSpy) ManagedRouteScope(target domain.TargetDefinition, connection Connection) (ManagedRouteScope, error) {
	return ManagedRouteScope{Endpoint: strings.TrimSuffix(connection.URL, "/"), TargetID: target.ID, Interface: connection.Interface}, nil
}

func savedConnection(id, targetID, address, iface string, stored bool, created int) DeviceCard {
	return DeviceCard{Device: Device{
		ID: id, TargetID: targetID, Name: "connection " + id[:1], Address: address, Account: "admin", Interface: iface,
		CredentialStored: stored, CreatedAt: cleanupMoment.Add(time.Duration(created) * time.Minute),
	}}
}

type cleanupFixture struct {
	publication *cleanupPublicationFake
	devices     *cleanupDevicesFake
	remover     *removerSpy
	routes      *routeScopeSpy
	fqdnLedger  *memoryFQDNLedger
	routeLedger *memoryManagedRoutes
	delivered   deliveredFake
	journal     *cleanupJournalFake
	backups     *memoryBackups
}

// newCleanupFixture is one archived profile with two DNS formats, each
// delivered to its own router through its own saved connection.
func newCleanupFixture() *cleanupFixture {
	return &cleanupFixture{
		publication: &cleanupPublicationFake{
			profiles: map[string]Profile{cleanupProfile: {ID: cleanupProfile, ArchivedAt: cleanupMoment}},
			outputs: map[string]Output{
				managedOutputA: {ID: managedOutputA, ProfileID: cleanupProfile, TargetID: "keenetic-dns"},
				managedOutputB: {ID: managedOutputB, ProfileID: cleanupProfile, TargetID: "keenetic-dns"},
			},
		},
		devices: &cleanupDevicesFake{
			cards: []DeviceCard{
				savedConnection(strings.Repeat("d", 32), "keenetic-dns", "http://10.255.0.1/", "Wireguard0", true, 0),
				savedConnection(strings.Repeat("e", 32), "keenetic-dns", "http://10.255.0.2", "Wireguard0", true, 1),
			},
			credentials: map[string]string{strings.Repeat("d", 32): deployTestPassword, strings.Repeat("e", 32): deployTestPassword},
		},
		remover: newRemoverSpy(),
		routes:  &routeScopeSpy{&managedSpyDeployer{spyDeployer: &spyDeployer{formatKey: deployTestTarget().FormatKey, backup: []byte("config")}}},
		fqdnLedger: &memoryFQDNLedger{groups: map[string][]ManagedFQDNGroup{
			"http://10.255.0.1 " + managedOutputA: {ownedGroup("routevane-a")},
			"http://10.255.0.2 " + managedOutputB: {ownedGroup("routevane-b2"), ownedGroup("routevane-b1")},
		}},
		routeLedger: &memoryManagedRoutes{},
		delivered: deliveredFake{
			fqdn:   map[string][]string{managedOutputA: {"http://10.255.0.1"}, managedOutputB: {"http://10.255.0.2"}},
			routes: map[string][]ManagedRouteScope{},
		},
		journal: &cleanupJournalFake{},
		backups: &memoryBackups{},
	}
}

func (f *cleanupFixture) service(t *testing.T) *DeviceCleanupService {
	t.Helper()
	service, err := NewDeviceCleanupService(DeviceCleanupConfig{
		Publication: f.publication, Devices: f.devices,
		Deployers: DeployerRegistry{f.remover.ID(): f.remover, f.routes.ID(): f.routes},
		Backups:   f.backups, ManagedRoutes: f.routeLedger, ManagedFQDN: f.fqdnLedger,
		Delivered: f.delivered, Journal: f.journal, Clock: fixedClock(), Entropy: rand.Reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDeviceObjectsNameTheSavedConnectionThatCanCleanEachSurface(t *testing.T) {
	f := newCleanupFixture()
	// A third format holds static routes on the first router.
	f.publication.outputs[cleanupOutputC] = Output{ID: cleanupOutputC, ProfileID: cleanupProfile, TargetID: "keenetic"}
	scope := ManagedRouteScope{Endpoint: "http://10.255.0.1", TargetID: "keenetic", Interface: "Wireguard0"}
	f.delivered.routes[cleanupOutputC] = []ManagedRouteScope{scope}
	f.routeLedger.state = ManagedRouteOwnership{
		Scope: scope,
		Routes: []ManagedRoute{
			{Prefix: managedPrefix("192.0.2.10/32"), CreatedByRoutevane: true},
			{Prefix: managedPrefix("198.51.100.20/32"), CreatedByRoutevane: true},
			{Prefix: managedPrefix("203.0.113.0/24")},
		},
		Claims: []ManagedRouteClaim{
			{OutputID: cleanupOutputC, Prefix: managedPrefix("192.0.2.10/32")},
			{OutputID: cleanupOutputC, Prefix: managedPrefix("198.51.100.20/32")},
			{OutputID: cleanupOutputC, Prefix: managedPrefix("203.0.113.0/24")},
			{OutputID: managedOutputA, Prefix: managedPrefix("192.0.2.10/32")},
		},
	}
	bound := strings.Repeat("f", 32)
	f.publication.outputs[managedOutputA] = Output{ID: managedOutputA, ProfileID: cleanupProfile, TargetID: "keenetic-dns", DeviceID: bound}
	f.devices.cards = append(f.devices.cards,
		// The format's own bound connection to the same router, without a
		// saved password: one that can sign in is preferred over it.
		savedConnection(bound, "keenetic-dns", "http://10.255.0.1", "Wireguard9", false, -5),
		// The same address for another format is not this surface.
		savedConnection(strings.Repeat("a", 32), "keenetic", "http://10.255.0.2", "Wireguard0", true, 2),
		// Static routes need the scope's own interface.
		savedConnection(strings.Repeat("b", 32), "keenetic", "http://10.255.0.1", "Wireguard1", true, 3),
	)
	objects, err := f.service(t).ProfileDeviceObjects(context.Background(), cleanupProfile)
	if err != nil {
		t.Fatal(err)
	}
	want := []DeviceObjects{
		{OutputID: managedOutputA, TargetID: "keenetic-dns", Endpoint: "http://10.255.0.1", Interface: "Wireguard0", FQDNGroups: []string{"routevane-a"},
			Connection: &DeviceObjectsConnection{ID: strings.Repeat("d", 32), Name: "connection d", CredentialStored: true}, Cleanable: true},
		{OutputID: managedOutputB, TargetID: "keenetic-dns", Endpoint: "http://10.255.0.2", Interface: "Wireguard0", FQDNGroups: []string{"routevane-b1", "routevane-b2"},
			Connection: &DeviceObjectsConnection{ID: strings.Repeat("e", 32), Name: "connection e", CredentialStored: true}, Cleanable: true},
		{OutputID: cleanupOutputC, TargetID: "keenetic", Endpoint: "http://10.255.0.1", Interface: "Wireguard0",
			Routes: &DeviceRouteCounts{Claimed: 3, Removable: 1, Shared: 1, Preexisting: 1}, BlockedBy: CleanupBlockedConnectionMissing},
	}
	if !reflect.DeepEqual(objects, want) {
		t.Fatalf("objects =\n%+v\nwant\n%+v", objects, want)
	}

	// Without a saved password on the only match, the surface names the
	// connection and why it cannot clean; without any match it names none.
	f.devices.cards = f.devices.cards[1:]
	objects, _ = f.service(t).OutputDeviceObjects(context.Background(), managedOutputA)
	if len(objects) != 1 || objects[0].Cleanable || objects[0].BlockedBy != CleanupBlockedLoginMissing || objects[0].Connection == nil || objects[0].Connection.ID != bound {
		t.Fatalf("credential missing = %+v", objects)
	}
	f.devices.cards = nil
	objects, _ = f.service(t).OutputDeviceObjects(context.Background(), managedOutputA)
	if len(objects) != 1 || objects[0].BlockedBy != CleanupBlockedConnectionMissing || objects[0].Connection != nil {
		t.Fatalf("connection missing = %+v", objects)
	}
	// A format whose target left the catalog cannot be cleaned by any deployer.
	f.publication.outputs[managedOutputA] = Output{ID: managedOutputA, ProfileID: cleanupProfile, TargetID: "retired-target"}
	objects, _ = f.service(t).OutputDeviceObjects(context.Background(), managedOutputA)
	if len(objects) != 1 || objects[0].BlockedBy != CleanupBlockedRemovalUnsupported {
		t.Fatalf("unsupported = %+v", objects)
	}
	if len(f.remover.calls)+len(f.routes.calls) != 0 {
		t.Fatalf("a read contacted a device: %v %v", f.remover.calls, f.routes.calls)
	}
}

func TestADeviceCleanupRetiresOnlyAfterEveryRemovalVerified(t *testing.T) {
	f := newCleanupFixture()
	retirement, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile)
	if err != nil || !retirement.RetiredAt.Equal(cleanupMoment) || len(retirement.Cleanups) != 2 {
		t.Fatalf("retirement=%+v err=%v", retirement, err)
	}
	// Every device is asked before any is changed.
	if got := strings.Join(f.remover.calls, ","); got != "probe,preview,probe,preview,probe,preview,backup,remove,verify,probe,preview,backup,remove,verify" {
		t.Fatalf("calls = %s", got)
	}
	for index, attempt := range retirement.Cleanups {
		begun := f.journal.begun[index]
		if attempt.Status != DeploymentAttemptSucceeded || !attempt.Result.Applied || attempt.ID != begun.ID || len(attempt.ID) != 32 ||
			begun.Status != DeploymentAttemptPending || !reflect.DeepEqual(f.journal.completed[index], attempt) {
			t.Fatalf("attempt %d = %+v begun=%+v", index, attempt, begun)
		}
	}
	if retirement.Cleanups[0].OutputID != managedOutputA || retirement.Cleanups[0].DeviceID != strings.Repeat("d", 32) || retirement.Cleanups[0].Endpoint != "http://10.255.0.1" ||
		retirement.Cleanups[1].OutputID != managedOutputB || retirement.Cleanups[1].DeviceID != strings.Repeat("e", 32) {
		t.Fatalf("attempt identities = %+v", retirement.Cleanups)
	}
	if !slices.Equal(f.publication.retired, []string{"delete " + cleanupProfile}) || len(f.fqdnLedger.groups) != 0 {
		t.Fatalf("retired=%v ledger=%v", f.publication.retired, f.fqdnLedger.groups)
	}
}

func TestARefusedDeviceCleanupRetiresNothing(t *testing.T) {
	t.Run("a surface without a saved password refuses before any device", func(t *testing.T) {
		f := newCleanupFixture()
		f.devices.cards[1].CredentialStored = false
		_, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile)
		var refusal *DeviceCleanupRefusal
		if !errors.As(err, &refusal) || refusal.Code() != "device_cleanup_unavailable" || len(refusal.Blocked) != 1 ||
			refusal.Blocked[0].OutputID != managedOutputB || refusal.Blocked[0].BlockedBy != CleanupBlockedLoginMissing {
			t.Fatalf("err=%v refusal=%+v", err, refusal)
		}
		if len(f.remover.calls) != 0 || len(f.journal.begun) != 0 || len(f.publication.retired) != 0 {
			t.Fatalf("calls=%v journal=%v retired=%v", f.remover.calls, f.journal.begun, f.publication.retired)
		}
	})
	t.Run("an unreadable saved password refuses before any device", func(t *testing.T) {
		f := newCleanupFixture()
		delete(f.devices.credentials, strings.Repeat("e", 32))
		_, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile)
		var refusal *DeviceCleanupRefusal
		if !errors.As(err, &refusal) || len(refusal.Blocked) != 1 || refusal.Blocked[0].BlockedBy != CleanupBlockedLoginMissing || len(f.remover.calls) != 0 {
			t.Fatalf("err=%v calls=%v", err, f.remover.calls)
		}
	})
	t.Run("a refused password on the second router leaves the first untouched", func(t *testing.T) {
		f := newCleanupFixture()
		f.remover.probeErrs["http://10.255.0.2"] = fmt.Errorf("login: %w", ErrDeviceAuthRejected)
		_, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile)
		var refusal *DeviceCleanupRefusal
		if !errors.As(err, &refusal) || refusal.Code() != "device_auth_rejected" || refusal.Step != StepProbe || refusal.Objects == nil ||
			refusal.Objects.OutputID != managedOutputB || len(refusal.Cleanups) != 0 {
			t.Fatalf("err=%v refusal=%+v", err, refusal)
		}
		if got := strings.Join(f.remover.calls, ","); got != "probe,preview,probe" || len(f.backups.stored) != 0 || len(f.journal.begun) != 0 || len(f.publication.retired) != 0 || len(f.fqdnLedger.groups) != 2 {
			t.Fatalf("calls=%s backups=%d journal=%d retired=%v", got, len(f.backups.stored), len(f.journal.begun), f.publication.retired)
		}
	})
	t.Run("a failed verification on the second router stops after the first was cleaned", func(t *testing.T) {
		f := newCleanupFixture()
		f.remover.verifyErrs["http://10.255.0.2"] = errors.New("group still present")
		retirement, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile)
		var refusal *DeviceCleanupRefusal
		if !errors.As(err, &refusal) || refusal.Code() != "verify_failed" || refusal.Step != StepVerify || refusal.Objects.OutputID != managedOutputB ||
			len(refusal.Cleanups) != 2 || refusal.Cleanups[0].Status != DeploymentAttemptSucceeded || refusal.Cleanups[1].Status != DeploymentAttemptFailed ||
			refusal.Cleanups[1].Error != "verify_failed" || !refusal.Cleanups[1].Result.RolledBack || !reflect.DeepEqual(retirement.Cleanups, refusal.Cleanups) {
			t.Fatalf("err=%v refusal=%+v", err, refusal)
		}
		// The first router stays clean and its rows are gone; the second keeps
		// its rows because its removal was rolled back.
		_, first := f.fqdnLedger.groups["http://10.255.0.1 "+managedOutputA]
		_, second := f.fqdnLedger.groups["http://10.255.0.2 "+managedOutputB]
		if first || !second || len(f.journal.completed) != 2 || len(f.publication.retired) != 0 {
			t.Fatalf("first=%v second=%v journal=%d retired=%v", first, second, len(f.journal.completed), f.publication.retired)
		}
	})
	t.Run("what the retirement itself refuses is refused before any device", func(t *testing.T) {
		f := newCleanupFixture()
		if _, err := f.service(t).RemoveOutput(context.Background(), managedOutputA); !errors.Is(err, ErrProfileArchived) {
			t.Fatalf("archived profile output removal err=%v", err)
		}
		f.publication.profiles[cleanupProfile] = Profile{ID: cleanupProfile}
		if _, err := f.service(t).DeleteProfile(context.Background(), cleanupProfile); !errors.Is(err, ErrProfileNotArchived) {
			t.Fatalf("shelf profile deletion err=%v", err)
		}
		if _, err := f.service(t).RemoveOutput(context.Background(), strings.Repeat("9", 32)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown output err=%v", err)
		}
		if len(f.remover.calls) != 0 || len(f.publication.retired) != 0 {
			t.Fatalf("calls=%v retired=%v", f.remover.calls, f.publication.retired)
		}
	})
}

func TestADeviceCleanupRemovesOneFormatThenRetiresIt(t *testing.T) {
	f := newCleanupFixture()
	f.publication.profiles[cleanupProfile] = Profile{ID: cleanupProfile}
	retirement, err := f.service(t).RemoveOutput(context.Background(), managedOutputB)
	if err != nil || len(retirement.Cleanups) != 1 || retirement.Cleanups[0].OutputID != managedOutputB || !slices.Equal(f.publication.retired, []string{"remove " + managedOutputB}) {
		t.Fatalf("retirement=%+v err=%v retired=%v", retirement, err, f.publication.retired)
	}
	if _, kept := f.fqdnLedger.groups["http://10.255.0.1 "+managedOutputA]; !kept {
		t.Fatal("another format's ledger was emptied")
	}
}

func TestNewDeviceCleanupServiceRefusesAnIncompleteComposition(t *testing.T) {
	if _, err := NewDeviceCleanupService(DeviceCleanupConfig{}); !errors.Is(err, ErrDeviceCleanupComposition) {
		t.Fatalf("err=%v", err)
	}
}
