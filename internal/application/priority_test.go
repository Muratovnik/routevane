package application

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestDefaultPriorityFiltersStaleAndAppendsNewCatalogMembers(t *testing.T) {
	store := &publicationFakeStore{globalPriority: []string{"stale", "other"}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x51}, 256)))
	definition := publication.config.Definitions["example"]
	definition.ID = "other"
	publication.config.Definitions["other"] = definition

	got, err := publication.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other", "example"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default priority=%#v, want %#v", got, want)
	}
	if err := publication.SetDefaultPriority(context.Background(), []string{"example", "other"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.globalPriority, []string{"example", "other"}) {
		t.Fatalf("stored priority=%#v", store.globalPriority)
	}
	for _, invalid := range [][]string{{"example"}, {"example", "example"}, {"example", "missing"}, {"bad id", "other"}} {
		if err := publication.SetDefaultPriority(context.Background(), invalid); err == nil {
			t.Fatalf("invalid priority %#v accepted", invalid)
		}
	}
}

func TestCreateProfileAndForecastUseGlobalPriorityOnlyWhenOmitted(t *testing.T) {
	store := &publicationFakeStore{globalPriority: []string{"other", "example"}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	definition := publication.config.Definitions["example"]
	definition.ID = "other"
	publication.config.Definitions["other"] = definition

	created, err := publication.CreateProfile(context.Background(), "global", ProfileComposition{Lists: []string{"example", "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other", "example"}; !reflect.DeepEqual(created.Priority, want) {
		t.Fatalf("created priority=%#v, want %#v", created.Priority, want)
	}
	forecasts, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"example", "other"}}, []string{"keenetic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forecasts) != 1 {
		t.Fatalf("forecasts=%#v", forecasts)
	}
	if want := []ListRuleForecast{{ListID: "example", Rules: 0}, {ListID: "other", Rules: 2}}; !reflect.DeepEqual(forecasts[0].PerList, want) {
		t.Fatalf("forecast per-list=%#v, want %#v", forecasts[0].PerList, want)
	}
	// A non-empty request remains a profile-local override even when the global
	// order changes later. The profile's stored priority is never rewritten.
	store.globalPriority = []string{"example", "other"}
	if got, err := publication.Profile(context.Background(), created.ID); err != nil || !reflect.DeepEqual(got.Priority, []string{"other", "example"}) {
		t.Fatalf("stored route priority=%#v err=%v", got.Priority, err)
	}
	override, err := publication.CreateProfile(context.Background(), "override", ProfileComposition{Lists: []string{"example", "other"}, Priority: []string{"example", "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example", "other"}; !reflect.DeepEqual(override.Priority, want) {
		t.Fatalf("explicit priority=%#v, want %#v", override.Priority, want)
	}
}

func TestDefaultPriorityUsesCanonicalOrderWithoutOptionalRepository(t *testing.T) {
	publication := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x53}, 256)))
	definition := publication.config.Definitions["example"]
	definition.ID = "other"
	publication.config.Definitions["other"] = definition
	got, err := publication.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"example", "other"}) {
		t.Fatalf("canonical fallback=%#v", got)
	}
}

func TestDefaultPriorityGroupsCategoriesWithoutChangingSavedOrder(t *testing.T) {
	store := &publicationFakeStore{}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x54}, 256)))
	definition := publication.config.Definitions["example"]
	for _, id := range []string{"alpha", "zulu", "shared"} {
		definition.ID = id
		publication.config.Definitions[id] = definition
	}
	publication.config.Categories = map[string]domain.CategoryDefinition{
		"first":  {ID: "first", Title: "First", Lists: []string{"zulu", "shared"}},
		"second": {ID: "second", Title: "Second", Lists: []string{"alpha", "shared"}},
	}
	got, err := publication.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Category members are canonical within each group; multi-membership appears once.
	want := []string{"shared", "zulu", "alpha", "example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grouped priority=%v, want %v", got, want)
	}
	store.globalPriority = []string{"example", "alpha"}
	got, err = publication.DefaultPriority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"example", "alpha", "shared", "zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved priority=%v, want %v", got, want)
	}
}
