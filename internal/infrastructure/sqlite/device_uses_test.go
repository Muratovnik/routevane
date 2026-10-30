package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// A connection lists the live formats bound to it with their profiles: an
// archived profile's formats stay and say so, while a removed format and a
// deleted profile's formats leave the list.
func TestDeviceUsesListTheLiveFormatsBoundToEachConnection(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store
	spare := application.Device{ID: strings.Repeat("f", 32), TargetID: "keenetic", Name: "Spare", Address: "http://192.0.2.2", Account: "admin", Interface: "Wireguard0", CreatedAt: retirementNow, UpdatedAt: retirementNow}
	if err := store.CreateDevice(ctx, spare); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOutputDevice(ctx, f.other.ID, f.device.ID); err != nil {
		t.Fatal(err)
	}
	dacha := application.DeviceUse{ProfileID: f.profile.ID, ProfileName: "Дача", OutputID: f.output.ID, TargetID: "keenetic"}
	office := application.DeviceUse{ProfileID: f.sibling.ID, ProfileName: "Офис", OutputID: f.other.ID, TargetID: "keenetic"}
	expect := func(label string, want []application.DeviceUse) {
		t.Helper()
		uses, err := store.DeviceUses(ctx)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if _, listed := uses[spare.ID]; listed {
			t.Fatalf("%s: a connection nothing is bound to is listed: %v", label, uses[spare.ID])
		}
		if got := uses[f.device.ID]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: uses=%+v want %+v", label, got, want)
		}
	}

	expect("two bound formats", []application.DeviceUse{dacha, office})

	later := retirementNow.Add(time.Hour)
	if err := store.SetProfileArchived(ctx, f.profile.ID, later, later); err != nil {
		t.Fatal(err)
	}
	archivedDacha := dacha
	archivedDacha.Archived = true
	expect("an archived profile follows the active ones", []application.DeviceUse{office, archivedDacha})

	if err := store.RemoveOutput(ctx, f.other.ID, later); err != nil {
		t.Fatal(err)
	}
	expect("a removed format", []application.DeviceUse{archivedDacha})

	if err := store.DeleteProfile(ctx, f.profile.ID, later); err != nil {
		t.Fatal(err)
	}
	expect("a deleted profile", nil)
}
