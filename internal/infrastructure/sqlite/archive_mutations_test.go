package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestArchivedProfileRefusesStoredOperatorMutationsAndRestoreReenablesThem(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*Store, application.Profile, application.Output) error
	}{
		{"composition", func(store *Store, profile application.Profile, _ application.Output) error {
			profile.Name = "Changed"
			profile.Lists, profile.Priority = []string{"beta"}, []string{"beta"}
			profile.UpdatedAt = now.Add(time.Minute)
			return store.UpdateProfile(context.Background(), profile)
		}},
		{"schedule", func(store *Store, profile application.Profile, _ application.Output) error {
			return store.UpdateProfileRefreshInterval(context.Background(), profile.ID, application.RefreshDaily, now.Add(time.Minute))
		}},
		{"new output", func(store *Store, profile application.Profile, _ application.Output) error {
			output := application.Output{
				ID: strings.Repeat("4", 32), ProfileID: profile.ID, TargetID: "singbox",
				FormatKey: "singbox-source-json-v1", RendererID: "singbox-ruleset-json", RendererVersion: "v1",
				TargetRevision: "revision", CreatedAt: now.Add(time.Minute),
			}
			return store.CreateOutput(context.Background(), application.NewOutput{Output: output})
		}},
		{"prefix", func(store *Store, _ application.Profile, output application.Output) error {
			return store.UpdateOutputFQDNPrefix(context.Background(), output.ID, "changed")
		}},
		{"device binding", func(store *Store, _ application.Profile, output application.Output) error {
			return store.UpdateOutputDevice(context.Background(), output.ID, "")
		}},
		{"subscription", func(store *Store, _ application.Profile, output application.Output) error {
			return store.CreateSubscription(context.Background(), output.ID, strings.Repeat("5", 32), sha256.Sum256([]byte("archive-fixture")), now.Add(time.Minute))
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(ctx, newDataRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			profile := application.Profile{ID: strings.Repeat("1", 32), Name: "Original", Lists: []string{"alpha"}, Priority: []string{"alpha"}, CreatedAt: now, UpdatedAt: now}
			if err := store.CreateProfile(ctx, profile); err != nil {
				t.Fatal(err)
			}
			device := application.Device{
				ID: strings.Repeat("2", 32), TargetID: "keenetic-dns", Name: "Fixture",
				Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0", CreatedAt: now, UpdatedAt: now,
			}
			if err := store.CreateDevice(ctx, device); err != nil {
				t.Fatal(err)
			}
			output := application.Output{
				ID: strings.Repeat("3", 32), ProfileID: profile.ID, TargetID: "keenetic-dns",
				FormatKey: "keenetic-fqdn-group-v1", RendererID: "keenetic-fqdn-group", RendererVersion: "v1",
				TargetRevision: "revision", CreatedAt: now, FQDNGroupPrefix: "original", DeviceID: device.ID,
			}
			if err := store.CreateOutput(ctx, application.NewOutput{Output: output}); err != nil {
				t.Fatal(err)
			}
			type state struct {
				profile       application.Profile
				outputs       []application.Output
				subscriptions int
			}
			readState := func() state {
				t.Helper()
				storedProfile, err := store.Profile(ctx, profile.ID)
				if err != nil {
					t.Fatal(err)
				}
				outputs, err := store.OutputsByProfile(ctx, profile.ID)
				if err != nil {
					t.Fatal(err)
				}
				var subscriptions int
				if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions`).Scan(&subscriptions); err != nil {
					t.Fatal(err)
				}
				return state{profile: storedProfile, outputs: outputs, subscriptions: subscriptions}
			}
			if err := store.SetProfileArchived(ctx, profile.ID, now.Add(time.Second), now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			before := readState()
			if err := testCase.mutate(store, profile, output); !errors.Is(err, application.ErrProfileArchived) {
				t.Fatalf("archived mutation error=%v, want ErrProfileArchived", err)
			}
			if after := readState(); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected mutation changed persisted state: before=%#v after=%#v", before, after)
			}
			if err := store.SetProfileArchived(ctx, profile.ID, time.Time{}, now.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			restored := readState()
			if err := testCase.mutate(store, profile, output); err != nil {
				t.Fatalf("restored profile still refuses the same mutation: %v", err)
			}
			if after := readState(); reflect.DeepEqual(restored, after) {
				t.Fatal("restored mutation reported success without changing persisted state")
			}
		})
	}
}
