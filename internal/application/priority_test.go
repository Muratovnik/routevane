package application

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestDefaultPriorityFiltersStaleAndAppendsNewCatalogMembers(t *testing.T) {
	store := &publicationFakeStore{globalPriority: []string{"stale", "other"}}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x51}, 256)))
	definition := service.config.Definitions["example"]
	definition.ID = "other"
	service.config.Definitions["other"] = definition

	got, err := service.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other", "example"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default priority=%#v, want %#v", got, want)
	}
	if err := service.SetDefaultPriority(context.Background(), []string{"example", "other"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.globalPriority, []string{"example", "other"}) {
		t.Fatalf("stored priority=%#v", store.globalPriority)
	}
	for _, invalid := range [][]string{{"example"}, {"example", "example"}, {"example", "missing"}, {"bad id", "other"}} {
		if err := service.SetDefaultPriority(context.Background(), invalid); err == nil {
			t.Fatalf("invalid priority %#v accepted", invalid)
		}
	}
}

func TestCreateListAndForecastUseGlobalPriorityOnlyWhenOmitted(t *testing.T) {
	store := &publicationFakeStore{globalPriority: []string{"other", "example"}}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	definition := service.config.Definitions["example"]
	definition.ID = "other"
	service.config.Definitions["other"] = definition

	created, err := service.CreateList(context.Background(), "global", ListComposition{Services: []string{"example", "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other", "example"}; !reflect.DeepEqual(created.Priority, want) {
		t.Fatalf("created priority=%#v, want %#v", created.Priority, want)
	}
	forecasts, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"example", "other"}}, []string{"keenetic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forecasts) != 1 {
		t.Fatalf("forecasts=%#v", forecasts)
	}
	if want := []ServiceRuleForecast{{ServiceID: "example", Rules: 0}, {ServiceID: "other", Rules: 2}}; !reflect.DeepEqual(forecasts[0].PerService, want) {
		t.Fatalf("forecast per-service=%#v, want %#v", forecasts[0].PerService, want)
	}
	// A non-empty request remains a route-local override even when the global
	// order changes later. The list's stored priority is never rewritten.
	store.globalPriority = []string{"example", "other"}
	if got, err := service.List(context.Background(), created.ID); err != nil || !reflect.DeepEqual(got.Priority, []string{"other", "example"}) {
		t.Fatalf("stored route priority=%#v err=%v", got.Priority, err)
	}
	override, err := service.CreateList(context.Background(), "override", ListComposition{Services: []string{"example", "other"}, Priority: []string{"example", "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example", "other"}; !reflect.DeepEqual(override.Priority, want) {
		t.Fatalf("explicit priority=%#v, want %#v", override.Priority, want)
	}
}

func TestDefaultPriorityUsesCanonicalOrderWithoutOptionalRepository(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x53}, 256)))
	definition := service.config.Definitions["example"]
	definition.ID = "other"
	service.config.Definitions["other"] = definition
	got, err := service.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"example", "other"}) {
		t.Fatalf("canonical fallback=%#v", got)
	}
}
