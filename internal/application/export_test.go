package application

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestOneOffExportUsesThePublicationPipelineWithoutPersistingAnOutput(t *testing.T) {
	list, _ := testListAndOutput()
	store := &publicationFakeStore{list: list}
	files := &publicationFakeFiles{}
	service := newPublicationTestService(t, store, files, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))

	formats := service.ExportFormats()
	if len(formats) != 1 || formats[0].ID != "keenetic" || formats[0].RendererID != "keenetic-route-bat" || formats[0].FileExtension != "bat" {
		t.Fatalf("formats=%#v", formats)
	}
	exported, err := service.Export(context.Background(), list.ID, formats[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(exported.Payload) != "payload" || exported.Descriptor.ID != formats[0].RendererID {
		t.Fatalf("export=%#v", exported)
	}
	if len(store.creates) != 0 || len(store.published) != 0 || len(store.attempts) != 0 || files.puts != 0 {
		t.Fatalf("one-off export persisted state: store=%#v files=%#v", store, files)
	}
	if _, err := service.Export(context.Background(), list.ID, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown format error=%v", err)
	}
}

func TestExportFormatsCollapseEquivalentProfilesToTheMostCapableChoice(t *testing.T) {
	list, _ := testListAndOutput()
	service := newPublicationTestService(t, &publicationFakeStore{list: list}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))
	limited := service.config.Targets["keenetic"]
	limited.ID = "limited"
	limited.Constraints.MaxRules = 1
	service.config.Targets[limited.ID] = limited

	formats := service.ExportFormats()
	if len(formats) != 1 || formats[0].ID != "keenetic" {
		t.Fatalf("formats=%#v", formats)
	}
}
