package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// ADR 0050: version sixteen records that a password is saved as its own fact.
// Before it, a password existed only for a device that was opted in to automatic
// delivery and whose deployer takes a credential, which is a device with an
// account. The data step must report that fact and nothing else.
func TestVersionFifteenDevicesMigrateToTheSavedCredentialFact(t *testing.T) {
	const previousVersion = 15
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previous := &Store{db: db, path: path}
	if err := previous.initialize(context.Background(), migrations[:previousVersion]); err == nil {
		t.Fatal("a database short of the current version was accepted")
	}
	var columns int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('devices') WHERE name='credential_stored'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("version fifteen already had the column: count=%d err=%v", columns, err)
	}
	rows := []struct {
		id      string
		target  string
		account string
		auto    int
	}{
		{"11111111111111111111111111111111", "keenetic", "admin", 1}, // opted in, credential-backed
		{"22222222222222222222222222222222", "singbox", "", 1},       // opted in, credential-free
		{"33333333333333333333333333333333", "keenetic", "admin", 0}, // never opted in or opted out
		{"44444444444444444444444444444444", "singbox", "", 0},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO devices(id,target_id,name,address,account,auto_deliver,created_at_ns,updated_at_ns,interface) VALUES(?,?,?,?,?,?,1,1,'')`,
			row.id, row.target, "Device", "http://192.0.2.1", row.account, row.auto); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("migrating an existing database: %v", err)
	}
	defer store.Close()
	want := map[string]bool{
		"11111111111111111111111111111111": true,
		"22222222222222222222222222222222": false,
		"33333333333333333333333333333333": false,
		"44444444444444444444444444444444": false,
	}
	for id, stored := range want {
		device, err := store.Device(context.Background(), id)
		if err != nil || device.CredentialStored != stored {
			t.Fatalf("device %s credential_stored=%v err=%v, want %v", id, device.CredentialStored, err, stored)
		}
	}
	// The consent flag is untouched by the data step.
	if device, err := store.Device(context.Background(), "22222222222222222222222222222222"); err != nil || !device.AutoDeliver {
		t.Fatalf("credential-free consent was lost: %#v err=%v", device, err)
	}
	if _, err := store.db.Exec(`UPDATE devices SET credential_stored=2`); err == nil {
		t.Fatal("a value other than 0 or 1 was accepted")
	}
}

func TestDeviceCredentialFactRoundTripsThroughUpdate(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	device := application.Device{
		ID: "55555555555555555555555555555555", TargetID: "keenetic", Name: "Router",
		Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Device(context.Background(), device.ID); err != nil || stored.CredentialStored {
		t.Fatalf("new device = %#v err = %v", stored, err)
	}
	device.CredentialStored = true
	if err := store.UpdateDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Device(context.Background(), device.ID); err != nil || !stored.CredentialStored || stored.AutoDeliver {
		t.Fatalf("saved device = %#v err = %v", stored, err)
	}
	devices, err := store.Devices(context.Background())
	if err != nil || len(devices) != 1 || !devices[0].CredentialStored {
		t.Fatalf("devices = %#v err = %v", devices, err)
	}
	device.CredentialStored = false
	if err := store.UpdateDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Device(context.Background(), device.ID); err != nil || stored.CredentialStored {
		t.Fatalf("removed device = %#v err = %v", stored, err)
	}
}
