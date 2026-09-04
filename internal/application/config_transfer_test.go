package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

type transferFakeState struct {
	document ConfigTransferDocument
	applied  ConfigTransferApply
}

var transferFakeStates sync.Map // *publicationFakeStore -> *transferFakeState

func (s *publicationFakeStore) ExportConfigTransfer(context.Context) (ConfigTransferDocument, error) {
	state, _ := transferFakeStates.Load(s)
	if state == nil {
		return ConfigTransferDocument{}, nil
	}
	return state.(*transferFakeState).document, nil
}

func (s *publicationFakeStore) ApplyConfigTransfer(_ context.Context, apply ConfigTransferApply) error {
	state, _ := transferFakeStates.LoadOrStore(s, &transferFakeState{})
	state.(*transferFakeState).applied = apply
	return nil
}

func TestConfigTransferExportUsesTransferLocalReferences(t *testing.T) {
	store := &publicationFakeStore{}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff},
		CustomServices: []TransferCustomService{{Ref: "custom-deadbeefdeadbeef", Title: "Private", Domains: []string{"private.example"}}},
		Routes:         []TransferRoute{{Ref: strings.Repeat("a", 32), Name: "Private", Services: []string{"custom-deadbeefdeadbeef"}, ServiceDomains: map[string][]string{}, RefreshInterval: RefreshOff}},
		Devices:        []TransferDevice{{Ref: strings.Repeat("b", 32), TargetID: "keenetic", Name: "Router", Address: "http://192.0.2.1", Account: "", Interface: ""}},
		Outputs:        []TransferOutput{{Ref: strings.Repeat("c", 32), RouteRef: strings.Repeat("a", 32), TargetID: "keenetic", DeviceRef: strings.Repeat("b", 32)}},
	}})
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 256)))
	payload, err := service.ExportConfigTransfer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"custom-deadbeefdeadbeef", strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)} {
		if bytes.Contains(payload, []byte(sourceID)) {
			t.Fatalf("export leaked source-local identity %q: %s", sourceID, payload)
		}
	}
	var got ConfigTransferDocument
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.CustomServices[0].Ref != "custom-service-1" || got.Routes[0].Ref != "route-1" || got.Outputs[0].RouteRef != "route-1" || got.Outputs[0].DeviceRef != "device-1" {
		t.Fatalf("transfer references = %#v", got)
	}
}

func TestConfigTransferExportOmitsEveryCustomSourceAndItsDisabledReference(t *testing.T) {
	store := &publicationFakeStore{}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff},
		Tunings: []TransferTuning{{
			ServiceRef:      "example",
			DisabledSources: []string{"catalog-source", "feed-private"},
			CustomSources: []TransferCustomSource{{
				Ref: "feed-private", URL: "https://secret.example.test/token/SENTINEL/feed?format=json", Format: "text",
			}},
		}},
	}})
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	definition := service.config.Definitions["example"]
	definition.Sources = []domain.SourceDefinition{{ID: "catalog-source", Type: domain.SourceDNS}}
	service.config.Definitions["example"] = definition

	payload, err := service.ExportConfigTransfer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SENTINEL", "secret.example.test", "format=json", "feed-private"} {
		if bytes.Contains(payload, []byte(secret)) {
			t.Fatalf("export leaked custom-source material %q: %s", secret, payload)
		}
	}
	var got ConfigTransferDocument
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.OmittedCustomSources != 1 || len(got.Tunings) != 1 || len(got.Tunings[0].CustomSources) != 0 || len(got.Tunings[0].DisabledSources) != 1 || got.Tunings[0].DisabledSources[0] != "catalog-source" {
		t.Fatalf("sanitized transfer = %#v", got)
	}
	preview, _, err := service.PreviewConfigTransfer(payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Warnings) != 1 || preview.Warnings[0].Code != "custom_sources_require_recreation" {
		t.Fatalf("preview warnings = %#v", preview.Warnings)
	}
}

func TestConfigTransferRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	valid := `{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`
	if _, _, err := service.PreviewConfigTransfer([]byte(valid), nil); err != nil {
		t.Fatalf("valid neighbor: %v", err)
	}
	for _, tc := range []struct {
		name, document, path string
	}{
		{"top level", `{"version":"config-transfer-v1.1","version":"config-transfer-v1.1","settings":{"refresh_interval":"off"}}`, "/version"},
		{"nested", `{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off","refresh_interval":"daily"}}`, "/settings/refresh_interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := service.PreviewConfigTransfer([]byte(tc.document), nil)
			var transfer TransferError
			if !errors.As(err, &transfer) || transfer.Code != "config_transfer_duplicate_key" || transfer.Path != tc.path {
				t.Fatalf("preview error = %#v", err)
			}
		})
	}
}

func TestConfigTransferRejectsAnImportedCustomSourceURL(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	for _, version := range []string{configTransferLegacyVersion, ConfigTransferVersion} {
		document := map[string]any{
			"version": version, "settings": TransferSettings{RefreshInterval: RefreshOff},
			"tunings": []TransferTuning{{ServiceRef: "example", CustomSources: []TransferCustomSource{{Ref: "custom-source-1", URL: "https://secret.example.test/token/SENTINEL/feed?format=json", Format: "text"}}}},
		}
		if version == ConfigTransferVersion {
			document["omitted_custom_sources"] = 0
		}
		payload, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = service.PreviewConfigTransfer(payload, nil)
		var transfer TransferError
		if !errors.As(err, &transfer) || transfer.Code != "config_transfer_secret_material" || transfer.Path != "tunings/0/custom_sources" {
			t.Fatalf("version %s preview error = %#v", version, err)
		}
	}
}

func TestConfigTransferLegacyVersionHasNoOmissionMetadata(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	clean := []byte(`{"version":"config-transfer-v1.0","settings":{"refresh_interval":"off"}}`)
	if _, _, err := service.PreviewConfigTransfer(clean, nil); err != nil {
		t.Fatalf("clean legacy transfer: %v", err)
	}
	for _, value := range []string{"0", "1"} {
		withNewField := []byte(`{"version":"config-transfer-v1.0","settings":{"refresh_interval":"off"},"omitted_custom_sources":` + value + `}`)
		_, _, err := service.PreviewConfigTransfer(withNewField, nil)
		var transfer TransferError
		if !errors.As(err, &transfer) || transfer.Code != "config_transfer_unsupported_version" {
			t.Fatalf("legacy transfer with v1.1 field %s = %#v", value, err)
		}
	}
	currentWithoutField := []byte(`{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"}}`)
	_, _, err := service.PreviewConfigTransfer(currentWithoutField, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "omitted_custom_sources" {
		t.Fatalf("current transfer without omission metadata = %#v", err)
	}
	currentWithNull := []byte(`{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"},"omitted_custom_sources":null}`)
	_, _, err = service.PreviewConfigTransfer(currentWithNull, nil)
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "omitted_custom_sources" {
		t.Fatalf("current transfer with null omission metadata = %#v", err)
	}
}

func TestConfigTransferSizeBoundary(t *testing.T) {
	if err := validateConfigTransferSize(ConfigTransferMaxBytes); err != nil {
		t.Fatalf("maximum valid size: %v", err)
	}
	var transfer TransferError
	if err := validateConfigTransferSize(ConfigTransferMaxBytes + 1); !errors.As(err, &transfer) || transfer.Code != "config_transfer_too_large" {
		t.Fatalf("maximum + 1 error = %v", err)
	}
}

func TestConfigTransferEntryPointsEnforceTheSharedSizeBoundary(t *testing.T) {
	assertTooLarge := func(t *testing.T, err error) {
		t.Helper()
		var transfer TransferError
		if !errors.As(err, &transfer) || transfer.Code != "config_transfer_too_large" {
			t.Fatalf("oversized transfer error = %#v", err)
		}
	}

	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	oversized := bytes.Repeat([]byte{'x'}, ConfigTransferMaxBytes+1)
	_, _, err := service.PreviewConfigTransfer(oversized, nil)
	assertTooLarge(t, err)
	_, err = service.ApplyConfigTransfer(context.Background(), "sha256:"+strings.Repeat("0", 64), oversized, nil)
	assertTooLarge(t, err)

	store := &publicationFakeStore{}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Settings: TransferSettings{RefreshInterval: RefreshOff},
		Devices: []TransferDevice{{
			Ref:      "device-oversized",
			TargetID: "keenetic",
			Name:     strings.Repeat("x", ConfigTransferMaxBytes),
		}},
	}})
	t.Cleanup(func() { transferFakeStates.Delete(store) })
	exportService := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x44}, 256)))
	_, err = exportService.ExportConfigTransfer(context.Background())
	assertTooLarge(t, err)
}

func TestConfigTransferRejectsInvalidUTF8(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	payload := append([]byte(`{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"},"custom_services":[{"ref":"custom-service-1","title":"`), 0xff)
	payload = append(payload, []byte(`","domains":["example.test"]}]}`)...)
	_, _, err := service.PreviewConfigTransfer(payload, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_json" {
		t.Fatalf("invalid UTF-8 = %#v", err)
	}
}

func TestConfigTransferRejectsLocalCatalogDependency(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x44}, 256)))
	service.config.LocalServiceIDs = map[string]struct{}{"example": {}}
	payload, err := json.Marshal(ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff},
		Routes: []TransferRoute{{Ref: "route-1", Name: "Local", Services: []string{"example"}, ServiceDomains: map[string][]string{}, RefreshInterval: RefreshOff}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.PreviewConfigTransfer(payload, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_local_catalog_dependency" {
		t.Fatalf("preview error = %v", err)
	}
}

func TestConfigTransferBoundsPortableCollections(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x45}, 256)))
	base := ConfigTransferDocument{Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff}}
	cases := []struct {
		name string
		fill func(*ConfigTransferDocument)
	}{
		{"routes", func(d *ConfigTransferDocument) { d.Routes = make([]TransferRoute, maxTransferRoutes+1) }},
		{"devices", func(d *ConfigTransferDocument) { d.Devices = make([]TransferDevice, maxTransferDevices+1) }},
		{"outputs", func(d *ConfigTransferDocument) { d.Outputs = make([]TransferOutput, maxTransferOutputs+1) }},
		{"memberships and verdicts", func(d *ConfigTransferDocument) { d.Memberships = make([]TransferMembership, maxTransferRows+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			document := base
			tc.fill(&document)
			payload, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = service.PreviewConfigTransfer(payload, nil)
			var transfer TransferError
			if !errors.As(err, &transfer) || transfer.Code != "config_transfer_limit_exceeded" {
				t.Fatalf("preview error = %v", err)
			}
		})
	}
}

func TestConfigTransferApplyAcceptsItsOwnDigestAfterAnotherPreview(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x46}, 256)))
	first, err := json.Marshal(ConfigTransferDocument{Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(ConfigTransferDocument{Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshDaily}})
	if err != nil {
		t.Fatal(err)
	}
	firstPreview, _, err := service.PreviewConfigTransfer(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PreviewConfigTransfer(second, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyConfigTransfer(context.Background(), firstPreview.Digest, first, nil); err != nil {
		t.Fatalf("first preview became unusable after second preview: %v", err)
	}
}

func TestConfigTransferApplyRequiresTheExactPreviewedText(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x47}, 256)))
	previewed := []byte(`{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
	preview, _, err := service.PreviewConfigTransfer(previewed, nil)
	if err != nil {
		t.Fatal(err)
	}
	edited := append([]byte(" "), previewed...)
	_, err = service.ApplyConfigTransfer(context.Background(), preview.Digest, edited, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_preview_mismatch" {
		t.Fatalf("whitespace-edited apply = %#v", err)
	}
}
