package application

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestCreateCustomListStoresNormalizedDefinitionAndJoinsTheCatalog(t *testing.T) {
	store := &publicationFakeStore{}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x41}, 256)))

	created, err := publication.CreateCustomList(context.Background(), "  Мои сайты  ", []string{"Example.COM.", "b.example", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "custom-") || len(created.ID) != len("custom-")+16 {
		t.Fatalf("identity = %q", created.ID)
	}
	if created.Title != "Мои сайты" || !reflect.DeepEqual(created.Domains, []string{"b.example", "example.com"}) {
		t.Fatalf("created = %#v", created)
	}
	if stored, ok := store.custom[created.ID]; !ok || !reflect.DeepEqual(stored, created) {
		t.Fatalf("stored = %#v", store.custom)
	}

	ids := publication.Lists()
	if !reflect.DeepEqual(ids, []string{created.ID, "example"}) {
		t.Fatalf("lists = %#v", ids)
	}
	details := publication.ListDetails()
	if len(details) != 2 || details[0].ID != created.ID || !details[0].Custom || details[0].SourceCount != 0 {
		t.Fatalf("details = %#v", details)
	}
	if details[1].Custom {
		t.Fatalf("shipped list reported as custom: %#v", details[1])
	}
	wantDomains := []ListDomain{{Value: "b.example", IncludeSubdomains: true}, {Value: "example.com", IncludeSubdomains: true}}
	if !reflect.DeepEqual(details[0].Domains, wantDomains) {
		t.Fatalf("domains = %#v", details[0].Domains)
	}

	profile, err := publication.CreateProfile(context.Background(), "Свой набор", ProfileComposition{Lists: []string{created.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved := publication.ResolvedLists(profile); !reflect.DeepEqual(resolved, []string{created.ID}) {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestCreateCustomListRejectsInvalidInput(t *testing.T) {
	publication := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 256)))
	cases := []struct {
		name    string
		title   string
		domains []string
	}{
		{"empty title", "   ", []string{"example.com"}},
		{"control character", "a\x00b", []string{"example.com"}},
		{"no domains", "Сайты", nil},
		{"invalid domain", "Сайты", []string{"not a domain"}},
		{"too many domains", "Сайты", manyDomains(65)},
	}
	for _, tc := range cases {
		if _, err := publication.CreateCustomList(context.Background(), tc.title, tc.domains); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func manyDomains(count int) []string {
	domains := make([]string, 0, count)
	for i := 0; i < count; i++ {
		domains = append(domains, "host"+strconv.Itoa(i)+".example")
	}
	return domains
}

func TestUpdateCustomListReplacesTheMutablePartOnly(t *testing.T) {
	store := &publicationFakeStore{}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)))
	created, err := publication.CreateCustomList(context.Background(), "Сайты", []string{"a.example"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := publication.UpdateCustomList(context.Background(), created.ID, "Сайты 2", []string{"b.example"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || !updated.CreatedAt.Equal(created.CreatedAt) || updated.Title != "Сайты 2" || !reflect.DeepEqual(updated.Domains, []string{"b.example"}) {
		t.Fatalf("updated = %#v", updated)
	}
	definition, ok := publication.definition(created.ID)
	if !ok || len(definition.Seeds) != 1 || definition.Seeds[0].Value != "b.example" || definition.Seeds[0].Kind != domain.RuleDomainSuffix {
		t.Fatalf("definition = %#v", definition)
	}
	// The planner accepts exactly one catalog revision per plan, so a custom
	// list must carry the shipped catalog's revision or a mixed list could
	// never build.
	if definition.CatalogRevision != publication.config.Definitions["example"].CatalogRevision {
		t.Fatalf("revision = %q differs from the catalog", definition.CatalogRevision)
	}
	if _, err := publication.UpdateCustomList(context.Background(), "custom-missing0000000000", "Сайты", []string{"a.example"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update err = %v", err)
	}
}

// An operator's own list reports its domains with origin "manual", exactly
// like a value the operator added by verdict. The row must still tell the two
// apart, because only an include verdict is withdrawn by auto: a domain of the
// definition is switched off by exclude, and the plan and the card agree.
func TestCustomListDefinitionDomainsSwitchOffByExclusion(t *testing.T) {
	ctx := context.Background()
	store := &publicationFakeStore{}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x45}, 256)))
	created, err := publication.CreateCustomList(ctx, "Свои", []string{"myip.wtf", "2ip.io"})
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.SetListValues(ctx, created.ID, []string{"added.example"}, DomainVerdictInclude); err != nil {
		t.Fatal(err)
	}
	rowsOf := func() map[string]ListContentsRow {
		t.Helper()
		contents, err := publication.ListContents(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		rows := make(map[string]ListContentsRow, len(contents.Rows))
		for _, row := range contents.Rows {
			rows[row.Value] = row
		}
		return rows
	}
	seedValues := func() []string {
		t.Helper()
		definition, ok := publication.definition(created.ID)
		if !ok {
			t.Fatal("custom list vanished")
		}
		values := make([]string, 0, len(definition.Seeds))
		for _, seed := range definition.Seeds {
			values = append(values, seed.Value)
		}
		return values
	}

	rows := rowsOf()
	want := map[string]ListContentsRow{
		"2ip.io":        {Value: "2ip.io", Kind: "domain", Origin: "manual", Enabled: true, Definition: true},
		"myip.wtf":      {Value: "myip.wtf", Kind: "domain", Origin: "manual", Enabled: true, Definition: true},
		"added.example": {Value: "added.example", Kind: "domain", Origin: "manual", Enabled: true},
	}
	for value, row := range want {
		if rows[value] != row {
			t.Fatalf("row %q = %#v, want %#v", value, rows[value], row)
		}
	}

	// Auto removes a standing verdict; a definition domain has none, so it
	// cannot switch the domain off. This is the request the card sent.
	if err := publication.SetListValues(ctx, created.ID, []string{"2ip.io"}, DomainVerdictAuto); err != nil {
		t.Fatal(err)
	}
	if row := rowsOf()["2ip.io"]; !row.Enabled {
		t.Fatalf("auto switched a definition domain off: %#v", row)
	}

	if err := publication.SetListValues(ctx, created.ID, []string{"2ip.io"}, DomainVerdictExclude); err != nil {
		t.Fatal(err)
	}
	if row := rowsOf()["2ip.io"]; row.Enabled || !row.Definition || row.Missing {
		t.Fatalf("excluded definition domain = %#v", row)
	}
	if seeds := seedValues(); !reflect.DeepEqual(seeds, []string{"myip.wtf", "added.example"}) {
		t.Fatalf("planned seeds = %#v", seeds)
	}

	if err := publication.SetListValues(ctx, created.ID, []string{"2ip.io"}, DomainVerdictAuto); err != nil {
		t.Fatal(err)
	}
	if row := rowsOf()["2ip.io"]; !row.Enabled || !row.Definition {
		t.Fatalf("restored definition domain = %#v", row)
	}
	if seeds := seedValues(); len(seeds) != 3 {
		t.Fatalf("restored seeds = %#v", seeds)
	}

	// The include verdict is what auto withdraws: the row leaves the table.
	if err := publication.SetListValues(ctx, created.ID, []string{"added.example"}, DomainVerdictAuto); err != nil {
		t.Fatal(err)
	}
	if row, present := rowsOf()["added.example"]; present {
		t.Fatalf("withdrawn include still listed: %#v", row)
	}
}

func TestLoadCustomListsRefusesAnInvalidStoredRow(t *testing.T) {
	store := &publicationFakeStore{custom: map[string]CustomList{
		"stolen-id": {ID: "stolen-id", Title: "X", Domains: []string{"a.example"}, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)},
	}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x44}, 256)))
	if err := publication.LoadCustomLists(context.Background()); err == nil {
		t.Fatal("an id outside the reserved prefix was accepted")
	}
}
