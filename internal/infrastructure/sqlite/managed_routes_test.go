package sqlite

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func sqliteManagedScope() application.ManagedRouteScope {
	return application.ManagedRouteScope{Endpoint: "http://192.168.1.1", TargetID: "keenetic", Interface: "Wireguard0"}
}

func TestManagedRouteOwnershipPersistsExactlyAndRetirementStartsSafe(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	scope := sqliteManagedScope()
	insertManagedRouteOutputs(t, store, "11111111111111111111111111111111", "22222222222222222222222222222222", "33333333333333333333333333333333")
	created := netip.MustParsePrefix("192.0.2.10/32")
	preexisting := netip.MustParsePrefix("198.51.100.0/24")
	state := application.ManagedRouteOwnership{
		Scope: scope,
		Routes: []application.ManagedRoute{
			{Prefix: created, CreatedByRoutevane: true},
			{Prefix: preexisting, CreatedByRoutevane: false},
		},
		Claims: []application.ManagedRouteClaim{
			{OutputID: "11111111111111111111111111111111", Prefix: created},
			{OutputID: "22222222222222222222222222222222", Prefix: created},
			{OutputID: "22222222222222222222222222222222", Prefix: preexisting},
		},
	}
	if err := store.ReplaceManagedRouteOwnership(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.ManagedRouteOwnership(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("loaded = %#v, want %#v", loaded, state)
	}

	if err := store.RetireManagedRouteOwnership(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	retiredView, err := store.ManagedRouteOwnership(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(retiredView.Routes) != 0 || len(retiredView.Claims) != 0 || retiredView.Scope != scope {
		t.Fatalf("retired view = %#v", retiredView)
	}
	var retiredScopes, retainedClaims int
	if err := store.db.QueryRow(`SELECT count(*) FROM managed_route_scopes WHERE retired_at_ns<>0`).Scan(&retiredScopes); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM managed_route_claims`).Scan(&retainedClaims); err != nil {
		t.Fatal(err)
	}
	if retiredScopes != 1 || retainedClaims != len(state.Claims) {
		t.Fatalf("retirement discarded history: scopes=%d claims=%d", retiredScopes, retainedClaims)
	}

	fresh := application.ManagedRouteOwnership{
		Scope:  scope,
		Routes: []application.ManagedRoute{{Prefix: preexisting, CreatedByRoutevane: false}},
		Claims: []application.ManagedRouteClaim{{OutputID: "33333333333333333333333333333333", Prefix: preexisting}},
	}
	if err := store.ReplaceManagedRouteOwnership(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.ManagedRouteOwnership(context.Background(), scope)
	if err != nil || !reflect.DeepEqual(loaded, fresh) {
		t.Fatalf("fresh ownership = %#v err=%v", loaded, err)
	}
}

func TestInvalidManagedRouteReplacementLeavesThePreviousLedgerUnchanged(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	insertManagedRouteOutputs(t, store, "11111111111111111111111111111111")
	prefix := netip.MustParsePrefix("192.0.2.10/32")
	state := application.ManagedRouteOwnership{
		Scope:  sqliteManagedScope(),
		Routes: []application.ManagedRoute{{Prefix: prefix, CreatedByRoutevane: true}},
		Claims: []application.ManagedRouteClaim{{OutputID: "11111111111111111111111111111111", Prefix: prefix}},
	}
	if err := store.ReplaceManagedRouteOwnership(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	invalid := state
	invalid.Claims = append(invalid.Claims, invalid.Claims[0])
	if err := store.ReplaceManagedRouteOwnership(context.Background(), invalid); err == nil {
		t.Fatal("duplicate claim was accepted")
	}
	loaded, err := store.ManagedRouteOwnership(context.Background(), state.Scope)
	if err != nil || !reflect.DeepEqual(loaded, state) {
		t.Fatalf("invalid replacement changed ledger: %#v err=%v", loaded, err)
	}
}

func insertManagedRouteOutputs(t *testing.T, store *Store, outputIDs ...string) {
	t.Helper()
	for index, outputID := range outputIDs {
		listID := strings.Repeat(string(rune('a'+index)), 32)
		if _, err := store.db.Exec(`INSERT INTO lists(id,name,created_at_ns,updated_at_ns) VALUES(?,?,?,?)`, listID, "managed route test", index+1, index+1); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO outputs(id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,?,?,?,?,?,?)`, outputID, listID, "keenetic", "keenetic-bat-ipv4-v1", "keenetic-route-bat", "keenetic-bat-ipv4-v1", "test", index+1); err != nil {
			t.Fatal(err)
		}
	}
}
