package sqlite

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// The ledger index lists where one output owns objects: every FQDN endpoint,
// and every active route scope where it holds a claim. A retired scope is
// history and is not listed (ADR 0032).
func TestTheLedgerIndexListsWhereAnOutputOwnsObjects(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, b := strings.Repeat("1", 32), strings.Repeat("2", 32)
	insertManagedRouteOutputs(t, store, a, b)
	group := func(name string) []application.ManagedFQDNGroup {
		return []application.ManagedFQDNGroup{{Name: name, Entries: []string{name + ".example"}, Interface: "Wireguard0", Auto: true}}
	}
	for _, state := range []application.ManagedFQDNOwnership{
		{Endpoint: "http://192.0.2.2", OutputID: a, Groups: group("routevane-a2")},
		{Endpoint: "http://192.0.2.1", OutputID: a, Groups: group("routevane-a1")},
		{Endpoint: "http://192.0.2.1", OutputID: b, Groups: group("routevane-b1")},
	} {
		if err := store.ReplaceManagedFQDNOwnership(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	if endpoints, err := store.OutputFQDNEndpoints(ctx, a); err != nil || !slices.Equal(endpoints, []string{"http://192.0.2.1", "http://192.0.2.2"}) {
		t.Fatalf("endpoints of a = %v err=%v", endpoints, err)
	}
	prefix := netip.MustParsePrefix("192.0.2.10/32")
	active := sqliteManagedScope()
	retired := application.ManagedRouteScope{Endpoint: "http://192.0.2.9", TargetID: "keenetic", Interface: "Wireguard0"}
	for _, scope := range []application.ManagedRouteScope{active, retired} {
		if err := store.ReplaceManagedRouteOwnership(ctx, application.ManagedRouteOwnership{
			Scope:  scope,
			Routes: []application.ManagedRoute{{Prefix: prefix, CreatedByRoutevane: true}},
			Claims: []application.ManagedRouteClaim{{OutputID: a, Prefix: prefix}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RetireManagedRouteOwnership(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if scopes, err := store.OutputManagedRouteScopes(ctx, a); err != nil || !reflect.DeepEqual(scopes, []application.ManagedRouteScope{active}) {
		t.Fatalf("scopes of a = %v err=%v", scopes, err)
	}
	if scopes, err := store.OutputManagedRouteScopes(ctx, b); err != nil || len(scopes) != 0 {
		t.Fatalf("scopes of b = %v err=%v", scopes, err)
	}
	if _, err := store.OutputFQDNEndpoints(ctx, "not-an-id"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("invalid identity err=%v", err)
	}
}

// A cleanup attempt is claimed before its first device effect and completed
// once; the schema then refuses any change and any deletion.
func TestADeviceCleanupAttemptIsRecordedOnceAndNeverChanged(t *testing.T) {
	ctx := context.Background()
	root := newDataRoot(t)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	output := strings.Repeat("1", 32)
	insertManagedRouteOutputs(t, store, output)
	started := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	pending := application.DeviceCleanupAttempt{
		ID: strings.Repeat("a", 32), OutputID: output, DeviceID: strings.Repeat("d", 32), Endpoint: "http://192.0.2.1",
		Status: application.DeploymentAttemptPending, StartedAt: started,
	}
	if err := store.BeginDeviceCleanupAttempt(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginDeviceCleanupAttempt(ctx, pending); err == nil {
		t.Fatal("an attempt identity was claimed twice")
	}
	failed := pending
	failed.Status, failed.Error = application.DeploymentAttemptFailed, "verify_failed"
	failed.CompletedAt = started.Add(time.Second)
	failed.Result = application.DeployResult{RolledBack: true, Events: []application.DeployEvent{{Step: application.StepVerify, Outcome: "failed"}}}
	succeededWithoutApply := failed
	succeededWithoutApply.Status, succeededWithoutApply.Error = application.DeploymentAttemptSucceeded, ""
	if err := store.CompleteDeviceCleanupAttempt(ctx, succeededWithoutApply); err == nil {
		t.Fatal("a success that applied nothing was recorded")
	}
	if err := store.CompleteDeviceCleanupAttempt(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteDeviceCleanupAttempt(ctx, failed); !errors.Is(err, application.ErrDeploymentAttemptMismatch) {
		t.Fatalf("a completed attempt was completed again: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	attempts, err := store.DeviceCleanupAttempts(ctx, output)
	if err != nil || len(attempts) != 1 || !reflect.DeepEqual(attempts[0], failed) {
		t.Fatalf("attempts = %+v err=%v", attempts, err)
	}
	for _, statement := range []string{
		`UPDATE device_cleanup_attempts SET error_code='other'`,
		`UPDATE device_cleanup_attempts SET endpoint='http://192.0.2.9'`,
		`DELETE FROM device_cleanup_attempts`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("%s was accepted", statement)
		}
	}
	unknownOutput := pending
	unknownOutput.ID, unknownOutput.OutputID = strings.Repeat("b", 32), strings.Repeat("9", 32)
	if err := store.BeginDeviceCleanupAttempt(ctx, unknownOutput); err == nil {
		t.Fatal("an attempt for an unknown output was recorded")
	}
}
