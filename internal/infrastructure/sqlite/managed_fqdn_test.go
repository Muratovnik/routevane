package sqlite

import (
	"context"
	"github.com/Muratovnik/routevane/internal/application"
	"reflect"
	"strings"
	"testing"
)

func TestFQDNOwnershipSurvivesRestartAndCannotTransferAnotherOutputsName(t *testing.T) {
	ctx := context.Background()
	root := newDataRoot(t)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	a, b := strings.Repeat("1", 32), strings.Repeat("2", 32)
	insertManagedRouteOutputs(t, store, a, b)
	first := application.ManagedFQDNOwnership{Endpoint: "http://192.168.1.1", OutputID: a, Groups: []application.ManagedFQDNGroup{{Name: "routevane-a", Entries: []string{"a.example"}, Interface: "Wireguard0", Auto: true}}}
	second := application.ManagedFQDNOwnership{Endpoint: first.Endpoint, OutputID: b, Groups: []application.ManagedFQDNGroup{{Name: "routevane-b", Entries: []string{"b.example"}, Interface: "Wireguard1", Auto: true}}}
	if err := store.ReplaceManagedFQDNOwnership(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceManagedFQDNOwnership(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.ManagedFQDNOwnership(ctx, first.Endpoint, a)
	if err != nil || !reflect.DeepEqual(loaded, first) {
		t.Fatalf("reopen=%#v %v", loaded, err)
	}
	collision := second
	collision.Groups = first.Groups
	if err := store.ReplaceManagedFQDNOwnership(ctx, collision); err == nil {
		t.Fatal("another output adopted an owned name")
	}
	loaded, err = store.ManagedFQDNOwnership(ctx, second.Endpoint, b)
	if err != nil || !reflect.DeepEqual(loaded, second) {
		t.Fatalf("failed transaction changed prior ownership: %#v %v", loaded, err)
	}
	if err := store.RetireManagedFQDNOwnership(ctx, first.Endpoint, "Wireguard0"); err != nil {
		t.Fatal(err)
	}
	retired, err := store.ManagedFQDNOwnership(ctx, first.Endpoint, a)
	if err != nil || len(retired.Groups) != 0 {
		t.Fatalf("retirement=%#v %v", retired, err)
	}
	loaded, err = store.ManagedFQDNOwnership(ctx, second.Endpoint, b)
	if err != nil || !reflect.DeepEqual(loaded, second) {
		t.Fatalf("retirement touched unrelated interface: %#v %v", loaded, err)
	}
}

func TestOutputPrefixPersistsAndTransfersWithoutOwnership(t *testing.T) {
	ctx := context.Background()
	root := newDataRoot(t)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("1", 32)
	insertManagedRouteOutputs(t, store, id)
	// The immutable target is selected at creation; use an independently created
	// DNS output rather than bypassing its identity trigger for setup.
	_, err = store.db.Exec(`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'keenetic-dns','keenetic-fqdn-group-v1','keenetic-fqdn-group','keenetic-fqdn-group-v1','test',1)`, strings.Repeat("2", 32), strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	id = strings.Repeat("2", 32)
	if err := store.UpdateOutputFQDNPrefix(ctx, id, "home"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOutputFQDNPrefix(ctx, id, "bad prefix"); err == nil {
		t.Fatal("unsafe prefix persisted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	output, err := store.Output(ctx, id)
	if err != nil || output.FQDNGroupPrefix != "home" {
		t.Fatalf("prefix reopen=%#v %v", output, err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var exported application.ConfigTransferDocument
	if err := exportOutputs(ctx, tx, &exported); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range exported.Outputs {
		if item.Ref == id {
			found = item.FQDNGroupPrefix == "home"
		}
	}
	if !found {
		t.Fatalf("portable prefix missing: %#v", exported.Outputs)
	}
}
