package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

func transferSettings(priority ...string) TransferSettings {
	return TransferSettings{
		RefreshInterval: RefreshOff,
		DefaultPriority: append([]string{}, priority...),
	}
}

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

func TestConfigTransferExportCarriesAndRemapsDefaultPriority(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	custom, err := service.CreateCustomService(context.Background(), "Private", []string{"private.example"})
	if err != nil {
		t.Fatal(err)
	}
	store.globalPriority = []string{custom.ID, "example", "stale"}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Settings:       TransferSettings{RefreshInterval: RefreshOff},
		CustomServices: []TransferCustomService{{Ref: custom.ID, Title: custom.Title, Domains: custom.Domains}},
	}})
	t.Cleanup(func() { transferFakeStates.Delete(store) })

	payload, err := service.ExportConfigTransfer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got ConfigTransferDocument
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"custom-service-1", "example"}; !slices.Equal(got.Settings.DefaultPriority, want) {
		t.Fatalf("default priority=%#v, want %#v", got.Settings.DefaultPriority, want)
	}
}

func TestConfigTransferExportDropsCatalogServicesRemovedFromTheLibrary(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x53}, 256)))
	if err := service.RemoveService(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Settings: TransferSettings{RefreshInterval: RefreshOff},
		Removals: []TransferRemoval{{Kind: RemovalList, ID: "example"}},
	}})
	t.Cleanup(func() { transferFakeStates.Delete(store) })
	if _, err := service.ExportConfigTransfer(context.Background()); err != nil {
		t.Fatalf("removed catalog export: %v", err)
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
	valid := `{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`
	if _, _, err := service.PreviewConfigTransfer([]byte(valid), nil); err != nil {
		t.Fatalf("valid neighbor: %v", err)
	}
	for _, tc := range []struct {
		name, document, path string
	}{
		{"top level", `{"version":"config-transfer-v1.2","version":"config-transfer-v1.2","settings":{"refresh_interval":"off"}}`, "/version"},
		{"nested", `{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off","refresh_interval":"daily"}}`, "/settings/refresh_interval"},
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
	for _, version := range []string{configTransferLegacyVersion, configTransferLegacyOmissionVersion, configTransferOmissionVersion, ConfigTransferVersion} {
		document := map[string]any{
			"version": version, "settings": TransferSettings{RefreshInterval: RefreshOff},
			"tunings": []TransferTuning{{ServiceRef: "example", CustomSources: []TransferCustomSource{{Ref: "custom-source-1", URL: "https://secret.example.test/token/SENTINEL/feed?format=json", Format: "text"}}}},
		}
		if version != configTransferLegacyVersion {
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
	previous := []byte(`{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
	if _, _, err := service.PreviewConfigTransfer(previous, nil); err != nil {
		t.Fatalf("v1.1 transfer: %v", err)
	}
	for _, value := range []string{"0", "1"} {
		withNewField := []byte(`{"version":"config-transfer-v1.0","settings":{"refresh_interval":"off"},"omitted_custom_sources":` + value + `}`)
		_, _, err := service.PreviewConfigTransfer(withNewField, nil)
		var transfer TransferError
		if !errors.As(err, &transfer) || transfer.Code != "config_transfer_unsupported_version" {
			t.Fatalf("legacy transfer with v1.1 field %s = %#v", value, err)
		}
	}
	currentWithoutField := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"}}`)
	_, _, err := service.PreviewConfigTransfer(currentWithoutField, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "omitted_custom_sources" {
		t.Fatalf("current transfer without omission metadata = %#v", err)
	}
	currentWithNull := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},"omitted_custom_sources":null}`)
	_, _, err = service.PreviewConfigTransfer(currentWithNull, nil)
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "omitted_custom_sources" {
		t.Fatalf("current transfer with null omission metadata = %#v", err)
	}
	currentPriorityNull := []byte(`{"version":"config-transfer-v1.3","settings":{"refresh_interval":"off","default_priority":null},"omitted_custom_sources":0}`)
	_, _, err = service.PreviewConfigTransfer(currentPriorityNull, nil)
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "settings/default_priority" {
		t.Fatalf("current transfer with null default priority = %#v", err)
	}
	currentPriorityMissing := []byte(`{"version":"config-transfer-v1.3","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
	_, _, err = service.PreviewConfigTransfer(currentPriorityMissing, nil)
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != "settings/default_priority" {
		t.Fatalf("current transfer without default priority = %#v", err)
	}
	previousWithoutPriority := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
	if _, _, err = service.PreviewConfigTransfer(previousWithoutPriority, nil); err != nil {
		t.Fatalf("v1.2 transfer without default priority: %v", err)
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
	payload := append([]byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},"custom_services":[{"ref":"custom-service-1","title":"`), 0xff)
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
		Version: ConfigTransferVersion, Settings: transferSettings(),
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
	base := ConfigTransferDocument{Version: ConfigTransferVersion, Settings: transferSettings("example")}
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
	first, err := json.Marshal(ConfigTransferDocument{Version: ConfigTransferVersion, Settings: transferSettings("example")})
	if err != nil {
		t.Fatal(err)
	}
	secondSettings := transferSettings("example")
	secondSettings.RefreshInterval = RefreshDaily
	second, err := json.Marshal(ConfigTransferDocument{Version: ConfigTransferVersion, Settings: secondSettings})
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
	previewed := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
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

func TestConfigTransferValidatesEffectiveCompositionsBeforeApply(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x48}, 256)))
	service.config.Categories = map[string]domain.CategoryDefinition{
		"collection": {ID: "collection", Title: "Collection", Services: []string{"example"}},
		"empty":      {ID: "empty", Title: "Empty"},
	}
	base := ConfigTransferDocument{Version: ConfigTransferVersion, Settings: transferSettings("example")}
	domains := make([]string, maxDomainsPerService+1)
	for i := range domains {
		domains[i] = fmt.Sprintf("entry-%d.example", i)
	}
	for _, tc := range []struct {
		name  string
		route TransferRoute
	}{
		{"direct service contradiction", TransferRoute{Ref: "route-1", Name: "Route", Services: []string{"example"}, Exclusions: []string{"example"}, RefreshInterval: RefreshOff}},
		{"category excluded to empty", TransferRoute{Ref: "route-1", Name: "Route", Categories: []string{"collection"}, Exclusions: []string{"example"}, RefreshInterval: RefreshOff}},
		{"empty category", TransferRoute{Ref: "route-1", Name: "Route", Categories: []string{"empty"}, RefreshInterval: RefreshOff}},
		{"domain key outside effective composition", TransferRoute{Ref: "route-1", Name: "Route", Services: []string{"example"}, ServiceDomains: map[string][]string{"absent": {"example.test"}}, RefreshInterval: RefreshOff}},
		{"unnormalizable domain", TransferRoute{Ref: "route-1", Name: "Route", Services: []string{"example"}, ServiceDomains: map[string][]string{"example": {"bad domain"}}, RefreshInterval: RefreshOff}},
		{"too many route domains", TransferRoute{Ref: "route-1", Name: "Route", Services: []string{"example"}, ServiceDomains: map[string][]string{"example": domains}, RefreshInterval: RefreshOff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := base
			document.Routes = []TransferRoute{tc.route}
			payload, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.PreviewConfigTransfer(payload, nil); err == nil {
				t.Fatal("invalid composition reached preview")
			}
			if _, err := service.ApplyConfigTransfer(context.Background(), "sha256:"+strings.Repeat("0", 64), payload, nil); err == nil {
				t.Fatal("invalid composition reached apply")
			}
			if _, applied := transferFakeStates.Load(store); applied {
				t.Fatalf("invalid composition was persisted: %#v", applied)
			}
			if _, err := service.validComposition(ListComposition{Services: []string{"example"}}); err != nil {
				t.Fatalf("invalid import poisoned active registry: %v", err)
			}
		})
	}
}

func TestConfigTransferNormalizesValidRouteDomainsBeforeApply(t *testing.T) {
	store := &publicationFakeStore{}
	entropy := append(bytes.Repeat([]byte{0x49}, 8), bytes.Repeat([]byte{0x4a}, 8)...)
	entropy = append(entropy, bytes.Repeat([]byte{0x4b}, 32)...)
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(entropy))
	payload, err := json.Marshal(ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: transferSettings("custom-service-1", "example"),
		CustomServices:   []TransferCustomService{{Ref: "custom-service-1", Title: " Custom service ", Domains: []string{"CUSTOM.Example.COM."}}},
		CustomCategories: []TransferCustomCategory{{Ref: "custom-category-1", Title: " Custom category "}},
		Routes:           []TransferRoute{{Ref: "route-1", Name: " Route ", Services: []string{"example"}, Priority: []string{"example"}, ServiceDomains: map[string][]string{"example": {"WWW.Example.COM."}}, RefreshInterval: RefreshOff}},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := service.PreviewConfigTransfer(payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyConfigTransfer(context.Background(), preview.Digest, payload, nil); err != nil {
		t.Fatal(err)
	}
	state, ok := transferFakeStates.Load(store)
	if !ok {
		t.Fatal("valid transfer was not persisted")
	}
	got := state.(*transferFakeState).applied.Document.Routes[0].ServiceDomains["example"]
	if len(got) != 1 || got[0] != "www.example.com" {
		t.Fatalf("persisted route domains = %#v", got)
	}
	if name := state.(*transferFakeState).applied.Document.Routes[0].Name; name != "Route" {
		t.Fatalf("persisted route name = %q", name)
	}
	if got := state.(*transferFakeState).applied.Document.Routes[0].Priority; !slices.Equal(got, []string{"example"}) {
		t.Fatalf("persisted route priority = %#v", got)
	}
	applied := state.(*transferFakeState).applied.Document
	if service := applied.CustomServices[0]; service.Title != "Custom service" || len(service.Domains) != 1 || service.Domains[0] != "custom.example.com" {
		t.Fatalf("persisted custom service = %#v", service)
	}
	if title := applied.CustomCategories[0].Title; title != "Custom category" {
		t.Fatalf("persisted custom category title = %q", title)
	}
}

func TestConfigTransferRejectsMalformedDocumentLocalReferences(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x4a}, 256)))
	base := ConfigTransferDocument{Version: ConfigTransferVersion, Settings: transferSettings("example")}
	for _, tc := range []struct {
		name, path string
		mutate     func(*ConfigTransferDocument)
	}{
		{"empty route ref", "routes/0/ref", func(d *ConfigTransferDocument) {
			d.Routes = []TransferRoute{{Name: "Route", Services: []string{"example"}, RefreshInterval: RefreshOff}}
		}},
		{"route control character", "routes/0/name", func(d *ConfigTransferDocument) {
			d.Routes = []TransferRoute{{Ref: "route-1", Name: "bad\nname", Services: []string{"example"}, RefreshInterval: RefreshOff}}
		}},
		{"empty device ref", "devices/0/ref", func(d *ConfigTransferDocument) { d.Devices = []TransferDevice{{TargetID: "keenetic", Name: "Router"}} }},
		{"empty output ref", "outputs/0/ref", func(d *ConfigTransferDocument) {
			d.Routes = []TransferRoute{{Ref: "route-1", Name: "Route", Services: []string{"example"}, RefreshInterval: RefreshOff}}
			d.Outputs = []TransferOutput{{RouteRef: "route-1", TargetID: "keenetic"}}
		}},
		{"unversioned output route ref", "outputs/0/route_ref", func(d *ConfigTransferDocument) {
			d.Routes = []TransferRoute{{Ref: "route-1", Name: "Route", Services: []string{"example"}, RefreshInterval: RefreshOff}}
			d.Outputs = []TransferOutput{{Ref: "output-1", RouteRef: strings.Repeat("a", 32), TargetID: "keenetic"}}
		}},
		{"unversioned output device ref", "outputs/0/device_ref", func(d *ConfigTransferDocument) {
			d.Routes = []TransferRoute{{Ref: "route-1", Name: "Route", Services: []string{"example"}, RefreshInterval: RefreshOff}}
			d.Devices = []TransferDevice{{Ref: "device-1", TargetID: "keenetic", Name: "Router"}}
			d.Outputs = []TransferOutput{{Ref: "output-1", RouteRef: "route-1", TargetID: "keenetic", DeviceRef: strings.Repeat("b", 32)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := base
			tc.mutate(&document)
			payload, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = service.PreviewConfigTransfer(payload, nil)
			var transfer TransferError
			if !errors.As(err, &transfer) || transfer.Code != "config_transfer_invalid_shape" || transfer.Path != tc.path {
				t.Fatalf("preview error = %#v", err)
			}
		})
	}
}

func TestConfigTransferAllowsAnExplicitlyUnboundOutput(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x4b}, 256)))
	payload, err := json.Marshal(ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: transferSettings("example"),
		Routes:  []TransferRoute{{Ref: "route-1", Name: "Route", Services: []string{"example"}, RefreshInterval: RefreshOff}},
		Outputs: []TransferOutput{{Ref: "output-1", RouteRef: "route-1", TargetID: "keenetic"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PreviewConfigTransfer(payload, nil); err != nil {
		t.Fatalf("unbound output was refused: %v", err)
	}
}

func TestConfigTransferRejectsDocumentsThatWouldCollideInStorage(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x4c}, 256)))
	base := ConfigTransferDocument{Version: ConfigTransferVersion, Settings: transferSettings("example")}
	for _, tc := range []struct {
		name, code, path string
		mutate           func(*ConfigTransferDocument)
	}{
		{
			name: "same destination included and excluded", code: "config_transfer_invalid_shape", path: "tunings/0",
			mutate: func(d *ConfigTransferDocument) {
				d.Tunings = []TransferTuning{{ServiceRef: "example", Includes: []string{"conflict.example"}, Excludes: []string{"conflict.example"}}}
			},
		},
		{
			name: "duplicate catalog removal", code: "config_transfer_duplicate_key", path: "removals/1",
			mutate: func(d *ConfigTransferDocument) {
				d.Removals = []TransferRemoval{{Kind: RemovalList, ID: "example"}, {Kind: RemovalList, ID: "example"}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := base
			tc.mutate(&document)
			payload, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = service.PreviewConfigTransfer(payload, nil)
			var transfer TransferError
			if !errors.As(err, &transfer) || transfer.Code != tc.code || transfer.Path != tc.path {
				t.Fatalf("preview error = %#v", err)
			}
		})
	}

	for _, document := range []ConfigTransferDocument{
		{Version: ConfigTransferVersion, Settings: transferSettings("example"), Tunings: []TransferTuning{{ServiceRef: "example", Includes: []string{"included.example"}}}},
		{Version: ConfigTransferVersion, Settings: transferSettings(), Removals: []TransferRemoval{{Kind: RemovalList, ID: "example"}}},
	} {
		payload, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := service.PreviewConfigTransfer(payload, nil); err != nil {
			t.Fatalf("valid storage-key neighbor was refused: %v", err)
		}
	}
}

// ADR 0039 renamed the fields this format carries. A file exported by an
// earlier version still imports, and one that names a field twice under both
// names is refused rather than reconciled by guesswork.
func TestConfigTransferReadsTheRetiredFieldNamesButRefusesBoth(t *testing.T) {
	store := &publicationFakeStore{}
	transferFakeStates.Store(store, &transferFakeState{document: ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff},
	}})
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))

	// What this build writes is what it reads back, under the current names.
	exported, err := service.ExportConfigTransfer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exported, []byte(`"version":"config-transfer-v1.4"`)) {
		t.Fatalf("export must state the current version: %s", exported)
	}
	for _, name := range []string{`"custom_lists"`, `"profiles"`} {
		if !bytes.Contains(exported, []byte(name)) {
			t.Fatalf("export must use %s: %s", name, exported)
		}
	}
	for _, retired := range []string{`"custom_services"`, `"routes"`, `"route_ref"`, `"service_ref"`} {
		if bytes.Contains(exported, []byte(retired)) {
			t.Fatalf("export must not write the retired %s: %s", retired, exported)
		}
	}
	if _, _, err := service.PreviewConfigTransfer(exported, nil); err != nil {
		t.Fatalf("an exported document must import: %v", err)
	}

	// A document written before the rename still names its parts the old way.
	// v1.2 is used because the default-priority requirement arrived in v1.3 and
	// is a separate contract from the field names.
	retired := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},` +
		`"custom_services":[],"custom_categories":[],"memberships":[],"removals":[],"tunings":[],` +
		`"routes":[],"devices":[],"outputs":[],"omitted_custom_sources":0}`)
	if _, _, err := service.PreviewConfigTransfer(retired, nil); err != nil {
		t.Fatalf("a document written before the rename must still import: %v", err)
	}

	// Naming both leaves nobody able to read the author's intent.
	var transfer TransferError
	both := []byte(`{"version":"config-transfer-v1.2","settings":{"refresh_interval":"off"},` +
		`"custom_lists":[],"custom_services":[],"custom_categories":[],"memberships":[],"removals":[],` +
		`"tunings":[],"profiles":[],"devices":[],"outputs":[],"omitted_custom_sources":0}`)
	if _, _, err := service.PreviewConfigTransfer(both, nil); !errors.As(err, &transfer) {
		t.Fatalf("a document naming both must be refused, got %#v", err)
	}

	// A version this build never wrote is still refused.
	future := []byte(`{"version":"config-transfer-v1.5","settings":{"refresh_interval":"off"},"omitted_custom_sources":0}`)
	if _, _, err := service.PreviewConfigTransfer(future, nil); !errors.As(err, &transfer) || transfer.Code != "config_transfer_unsupported_version" {
		t.Fatalf("unsupported version = %#v", err)
	}
}
