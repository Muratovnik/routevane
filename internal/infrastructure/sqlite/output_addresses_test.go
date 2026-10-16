package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestOutputKeepAddressesPersistsAndTransfers(t *testing.T) {
	ctx := context.Background()
	root := newDataRoot(t)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	insertManagedRouteOutputs(t, store, strings.Repeat("1", 32))
	id := strings.Repeat("2", 32)
	if _, err := store.db.Exec(`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'keenetic-dns','keenetic-fqdn-group-v1','keenetic-fqdn-group','keenetic-fqdn-group-v1','test',1)`, id, strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	output, err := store.Output(ctx, id)
	if err != nil || output.KeepAddresses {
		t.Fatalf("a new output keeps addresses by default: %#v %v", output, err)
	}
	if err := store.UpdateOutputKeepAddresses(ctx, id, true); err != nil {
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
	output, err = store.Output(ctx, id)
	if err != nil || !output.KeepAddresses {
		t.Fatalf("keep addresses after reopen=%#v %v", output, err)
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
	kept := map[string]bool{}
	for _, item := range exported.Outputs {
		kept[item.Ref] = item.KeepAddresses
	}
	if len(kept) != 2 || !kept[id] || kept[strings.Repeat("1", 32)] {
		t.Fatalf("exported keep addresses=%v", kept)
	}
}

// The column refuses anything but 0 and 1, so a value written around the
// store cannot be read back as a third state.
func TestOutputKeepAddressesColumnRefusesOtherValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := strings.Repeat("1", 32)
	insertManagedRouteOutputs(t, store, id)
	if _, err := store.db.Exec(`UPDATE outputs SET keep_addresses=2 WHERE id=?`, id); err == nil {
		t.Fatal("keep_addresses accepted 2")
	}
}
