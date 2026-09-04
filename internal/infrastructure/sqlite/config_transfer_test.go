package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestConfigTransferApplyIsFreshAndAtomic(t *testing.T) {
	store := categoryTestStore(t)
	document := application.ConfigTransferDocument{
		Version:  application.ConfigTransferVersion,
		Settings: application.TransferSettings{RefreshInterval: application.RefreshOff},
		CustomServices: []application.TransferCustomService{{
			Ref: "custom-service-1", Title: "Private", Domains: []string{"private.example"},
		}},
		Routes: []application.TransferRoute{{
			Ref: "route-1", Name: "Private", Services: []string{"custom-service-1"},
			ServiceDomains: map[string][]string{}, RefreshInterval: application.RefreshDaily,
		}},
		Devices: []application.TransferDevice{{
			Ref: "device-1", TargetID: "keenetic", Name: "Router", Address: "http://192.168.1.1", Account: "admin", Interface: "WG0",
		}},
		Outputs: []application.TransferOutput{{Ref: "output-1", RouteRef: "route-1", TargetID: "keenetic", DeviceRef: "device-1"}},
	}
	apply := application.ConfigTransferApply{
		Document:          document,
		AppliedAt:         time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
		CustomServiceIDs:  map[string]string{"custom-service-1": "custom-" + strings.Repeat("a", 16)},
		RouteIDs:          map[string]string{"route-1": strings.Repeat("b", 32)},
		DeviceIDs:         map[string]string{"device-1": strings.Repeat("c", 32)},
		OutputIDs:         map[string]string{"output-1": strings.Repeat("d", 32)},
		CustomCategoryIDs: map[string]string{}, CustomSourceIDs: map[string]string{},
		Outputs: map[string]application.Output{"output-1": {
			ID: strings.Repeat("d", 32), ListID: strings.Repeat("b", 32), TargetID: "keenetic", DeviceID: strings.Repeat("c", 32),
			ProfileKey: "profile", RendererID: "renderer", RendererVersion: "v1", TargetRevision: "revision",
		}},
	}
	if err := store.ApplyConfigTransfer(context.Background(), apply); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, store, "SELECT count(*) FROM devices WHERE auto_deliver=1"); got != 0 {
		t.Fatalf("import enabled automatic delivery for %d device(s)", got)
	}
	if got := countRows(t, store, "SELECT count(*) FROM outputs WHERE latest_artifact_id IS NOT NULL OR previous_artifact_id IS NOT NULL"); got != 0 {
		t.Fatalf("import restored %d publication pointer(s)", got)
	}
	if err := store.ApplyConfigTransfer(context.Background(), apply); !isTransferCode(err, "config_transfer_destination_not_empty") {
		t.Fatalf("second apply error = %v", err)
	}

	fresh := categoryTestStore(t)
	fresh.configTransferPreflight = func() error { return errors.New("force rollback") }
	if err := fresh.ApplyConfigTransfer(context.Background(), apply); err == nil {
		t.Fatal("preflight failure committed transfer")
	}
	if got := countRows(t, fresh, "SELECT count(*) FROM lists"); got != 0 {
		t.Fatalf("failed transaction left %d list(s)", got)
	}
}

func TestConfigTransferExportOmitsCustomSourceSecretsAndKeepsCatalogDisable(t *testing.T) {
	store := categoryTestStore(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixNano()
	const customID = "feed-1234567890abcdef"
	const secretURL = "https://secret.example.test/token/SENTINEL/feed?format=json"
	if _, err := store.db.Exec("INSERT INTO custom_sources(id,service_id,url,format,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?,?)", customID, "example", secretURL, "text", now, now); err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"catalog-source", customID} {
		if _, err := store.db.Exec("INSERT INTO service_disabled_sources(service_id,source_id) VALUES(?,?)", "example", sourceID); err != nil {
			t.Fatal(err)
		}
	}

	document, err := store.ExportConfigTransfer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if document.OmittedCustomSources != 1 || len(document.Tunings) != 1 {
		t.Fatalf("export = %#v", document)
	}
	tuning := document.Tunings[0]
	if len(tuning.CustomSources) != 0 || len(tuning.DisabledSources) != 1 || tuning.DisabledSources[0] != "catalog-source" {
		t.Fatalf("exported tuning = %#v", tuning)
	}
}

func isTransferCode(err error, want string) bool {
	var transfer application.TransferError
	return errors.As(err, &transfer) && transfer.Code == want
}
