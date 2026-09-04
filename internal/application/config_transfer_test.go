package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
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

func TestConfigTransferRejectsQueryBearingCustomSource(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	payload, err := json.Marshal(ConfigTransferDocument{
		Version: ConfigTransferVersion, Settings: TransferSettings{RefreshInterval: RefreshOff},
		Tunings: []TransferTuning{{ServiceRef: "example", CustomSources: []TransferCustomSource{{Ref: "custom-source-1", URL: "https://example.test/feed?token=sentinel", Format: "text"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.PreviewConfigTransfer(payload, nil)
	var transfer TransferError
	if !errors.As(err, &transfer) || transfer.Code != "config_transfer_secret_material" {
		t.Fatalf("preview error = %v", err)
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
