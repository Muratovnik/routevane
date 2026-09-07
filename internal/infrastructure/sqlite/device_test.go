package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestDeviceConnectionMetadataAndOutputBindingRoundTrip(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	profile := application.Profile{ID: "11111111111111111111111111111111", Name: "Profile", Lists: []string{"youtube"}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	device := application.Device{
		ID: "22222222222222222222222222222222", TargetID: "keenetic", Name: "Router",
		Address: "http://192.168.1.1", Account: "admin", Interface: "Wireguard0",
		AutoDeliver: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	output := application.Output{
		ID: "33333333333333333333333333333333", ProfileID: profile.ID, TargetID: "keenetic",
		FormatKey: "keenetic-rci-v1", RendererID: "keenetic-routes-bat", RendererVersion: "1",
		TargetRevision: "revision", CreatedAt: now,
	}
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: output}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOutputDevice(context.Background(), output.ID, device.ID); err != nil {
		t.Fatal(err)
	}
	storedDevice, err := store.Device(context.Background(), device.ID)
	if err != nil || storedDevice.Interface != "Wireguard0" || !storedDevice.AutoDeliver {
		t.Fatalf("device = %#v err = %v", storedDevice, err)
	}
	storedOutput, err := store.Output(context.Background(), output.ID)
	if err != nil || storedOutput.DeviceID != device.ID {
		t.Fatalf("output = %#v err = %v", storedOutput, err)
	}
	if err := store.DeleteDevice(context.Background(), device.ID); err != nil {
		t.Fatal(err)
	}
	storedOutput, err = store.Output(context.Background(), output.ID)
	if err != nil || storedOutput.DeviceID != "" {
		t.Fatalf("forgotten device left a dangling binding: output = %#v err = %v", storedOutput, err)
	}
}
