package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func tuningTestService(t *testing.T, store *publicationFakeStore) *PublicationService {
	t.Helper()
	// Distinct entropy bytes, so consecutive generated identities differ.
	entropy := make([]byte, 512)
	for i := range entropy {
		entropy[i] = byte(i)
	}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(entropy))
	definition := publication.config.Definitions["example"]
	definition.Sources = []domain.SourceDefinition{
		{ID: "vendor", Type: domain.SourceHTTP, ComponentID: "web", URL: "https://feeds.example/feed", Format: domain.FeedFormatText, Class: domain.SourceCommunity, Revision: strings.Repeat("a", 64)},
		{ID: "dns-main", Type: domain.SourceDNS, ComponentID: "web", Names: []string{"example.com"}, Revision: strings.Repeat("b", 64)},
	}
	publication.config.Definitions["example"] = definition
	return publication
}

func TestDisablingASourceLeavesTheDefinitionAndTheObservationFilter(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	if err := publication.SetListSourceEnabled(context.Background(), "example", "vendor", false); err != nil {
		t.Fatal(err)
	}
	definition, ok := publication.definition("example")
	if !ok || len(definition.Sources) != 1 || definition.Sources[0].ID != "dns-main" {
		t.Fatalf("sources = %#v", definition.Sources)
	}
	revisions := sourceRevisions(definition)
	if _, kept := revisions["vendor"]; kept || revisions["dns-main"] != strings.Repeat("b", 64) {
		t.Fatalf("revisions = %#v", revisions)
	}
	// Enabling removes the standing row, so the catalog default returns.
	if err := publication.SetListSourceEnabled(context.Background(), "example", "vendor", true); err != nil {
		t.Fatal(err)
	}
	definition, _ = publication.definition("example")
	if len(definition.Sources) != 2 {
		t.Fatalf("sources after enable = %#v", definition.Sources)
	}
	if err := publication.SetListSourceEnabled(context.Background(), "example", "absent", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown source err = %v", err)
	}
}

func TestAddListSourceValidatesAndJoinsTheDefinition(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	publication.config.FeedURL = func(raw string) error {
		if !strings.HasPrefix(raw, "https://") {
			return fmt.Errorf("not https")
		}
		return nil
	}
	if _, err := publication.AddListSource(context.Background(), "example", "http://plain.example/feed", domain.FeedFormatText); err == nil {
		t.Fatal("an invalid URL was accepted")
	}
	if _, err := publication.AddListSource(context.Background(), "example", "https://my.example/feed", "csv"); err == nil {
		t.Fatal("an unknown format was accepted")
	}
	if _, err := publication.AddListSource(context.Background(), "absent", "https://my.example/feed", domain.FeedFormatText); !errors.Is(err, ErrNotFound) {
		t.Fatal("an unknown list was accepted")
	}
	feed, err := publication.AddListSource(context.Background(), "example", "https://my.example/feed", domain.FeedFormatDomainList)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(feed.ID, "feed-") || feed.ListID != "example" {
		t.Fatalf("feed = %#v", feed)
	}
	definition, _ := publication.definition("example")
	if len(definition.Sources) != 3 {
		t.Fatalf("sources = %#v", definition.Sources)
	}
	var added domain.SourceDefinition
	for _, source := range definition.Sources {
		if source.ID == feed.ID {
			added = source
		}
	}
	if added.Type != domain.SourceHTTP || added.Class != domain.SourceCommunity || added.URL != "https://my.example/feed" || len(added.Revision) != 64 || added.ComponentID != "web" {
		t.Fatalf("added = %#v", added)
	}

	for i := 0; i < maxCustomSourcesPerList; i++ {
		_, err := publication.AddListSource(context.Background(), "example", fmt.Sprintf("https://my.example/feed-%d", i), domain.FeedFormatText)
		if i < maxCustomSourcesPerList-1 && err != nil {
			t.Fatalf("feed %d refused: %v", i, err)
		}
		if i == maxCustomSourcesPerList-1 && err == nil {
			t.Fatal("the per-list feed limit was not applied")
		}
	}

	if err := publication.RemoveListSource(context.Background(), "example", feed.ID); err != nil {
		t.Fatal(err)
	}
	definition, _ = publication.definition("example")
	for _, source := range definition.Sources {
		if source.ID == feed.ID {
			t.Fatalf("removed feed still declared: %#v", definition.Sources)
		}
	}
	if err := publication.RemoveListSource(context.Background(), "example", "vendor"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a catalog source was removable: %v", err)
	}
}

func TestDestinationVerdictsRewriteTheStaticSeeds(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	if err := publication.SetListValues(context.Background(), "example", []string{"example.com"}, DomainVerdictExclude); err != nil {
		t.Fatal(err)
	}
	if err := publication.SetListValues(context.Background(), "example", []string{"Extra.Example."}, DomainVerdictInclude); err != nil {
		t.Fatal(err)
	}
	definition, _ := publication.definition("example")
	domains := make([]string, 0)
	for _, seed := range definition.Seeds {
		if seed.Kind.IsDomain() {
			domains = append(domains, seed.Value)
		}
	}
	if !reflect.DeepEqual(domains, []string{"extra.example"}) {
		t.Fatalf("domains = %#v", domains)
	}
	// Auto removes the standing verdict: the catalog seed returns.
	if err := publication.SetListValues(context.Background(), "example", []string{"example.com"}, DomainVerdictAuto); err != nil {
		t.Fatal(err)
	}
	definition, _ = publication.definition("example")
	domains = domains[:0]
	for _, seed := range definition.Seeds {
		if seed.Kind.IsDomain() {
			domains = append(domains, seed.Value)
		}
	}
	if !reflect.DeepEqual(domains, []string{"example.com", "extra.example"}) {
		t.Fatalf("domains after reset = %#v", domains)
	}
	// A refusal names the value the operator must fix: a batch is refused as a
	// whole, so "something was wrong" would leave them reading the file again.
	var invalid InvalidDestinationError
	if err := publication.SetListValues(context.Background(), "example", []string{"not a domain"}, DomainVerdictExclude); !errors.As(err, &invalid) || invalid.Value != "not a domain" {
		t.Fatalf("invalid destination err = %v", err)
	}
}

// A destination is not only a name. An address and a network are stated the
// same way and must plant seeds of their own kind, or an address-only target
// would publish nothing for either.
func TestIncludedAddressesAndNetworksPlantSeedsOfTheirKind(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	values := []string{"198.51.100.7", "203.0.113.5/26", "2001:DB8::1"}
	if err := publication.SetListValues(context.Background(), "example", values, DomainVerdictInclude); err != nil {
		t.Fatal(err)
	}
	definition, _ := publication.definition("example")
	planted := make(map[string]domain.RuleKind, len(definition.Seeds))
	for _, seed := range definition.Seeds {
		planted[seed.Value] = seed.Kind
	}
	// What is planted is canonical: the network is masked and the address is
	// lowercased, so one destination cannot enter twice under two spellings.
	for value, kind := range map[string]domain.RuleKind{
		"198.51.100.7":   domain.RuleIPv4,
		"203.0.113.0/26": domain.RulePrefix4,
		"2001:db8::1":    domain.RuleIPv6,
	} {
		if planted[value] != kind {
			t.Fatalf("seed %q kind = %q, want %q (seeds = %#v)", value, planted[value], kind, definition.Seeds)
		}
	}
	if _, verbatim := planted["203.0.113.5/26"]; verbatim {
		t.Fatalf("an unmasked network was planted: %#v", definition.Seeds)
	}

	contents, err := publication.ListContents(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	// Names lead, then addresses, then networks; each block alphabetical, so a
	// table of hundreds of rows reads in one order whatever it holds.
	want := []ListContentsRow{
		{Value: "example.com", Kind: "domain", Origin: "catalog", Enabled: true},
		{Value: "192.0.2.1", Kind: "ip", Origin: "catalog", Enabled: true},
		{Value: "198.51.100.7", Kind: "ip", Origin: "manual", Enabled: true},
		{Value: "198.51.100.8", Kind: "ip", Origin: "dns-main", Enabled: true},
		{Value: "2001:db8::1", Kind: "ip", Origin: "manual", Enabled: true},
		{Value: "203.0.113.0/26", Kind: "prefix", Origin: "manual", Enabled: true},
	}
	if !reflect.DeepEqual(contents.Rows, want) {
		t.Fatalf("rows = %#v", contents.Rows)
	}
}

// Excluding works on every kind, not only on names: the catalog's own address
// leaves the plan while the rest of the definition stands.
func TestExcludingACatalogAddressRemovesItFromTheSeeds(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	if err := publication.SetListValues(context.Background(), "example", []string{"192.0.2.1"}, DomainVerdictExclude); err != nil {
		t.Fatal(err)
	}
	definition, _ := publication.definition("example")
	if len(definition.Seeds) != 1 || definition.Seeds[0].Value != "example.com" {
		t.Fatalf("seeds = %#v", definition.Seeds)
	}
	contents, err := publication.ListContents(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range contents.Rows {
		if row.Value == "192.0.2.1" {
			found = true
			if row.Enabled || row.Kind != "ip" || row.Origin != "catalog" {
				t.Fatalf("excluded address row = %#v", row)
			}
		}
	}
	if !found {
		t.Fatalf("the excluded address left the table instead of switching off: %#v", contents.Rows)
	}
}

// A batch is one action. One value the product cannot store refuses all of it,
// and nothing of the batch reaches storage or the registry.
func TestOneInvalidValueRefusesTheWholeBatch(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	before := publication.listTuning("example")
	err := publication.SetListValues(context.Background(), "example",
		[]string{"good.example", "198.51.100.7", "not a destination"}, DomainVerdictInclude)
	var invalid InvalidDestinationError
	if !errors.As(err, &invalid) || invalid.Value != "not a destination" {
		t.Fatalf("batch err = %v", err)
	}
	if store.verdictWrites != 0 {
		t.Fatalf("a refused batch reached storage %d times", store.verdictWrites)
	}
	if after := publication.listTuning("example"); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused batch changed the registry: %#v -> %#v", before, after)
	}
	definition, _ := publication.definition("example")
	for _, seed := range definition.Seeds {
		if seed.Value == "good.example" || seed.Value == "198.51.100.7" {
			t.Fatalf("a refused batch planted a seed: %#v", definition.Seeds)
		}
	}
}

// The batch bound is what keeps one paste from becoming an unbounded write. It
// is a bound on the action, so the largest allowed batch still succeeds.
func TestABatchLargerThanTheBoundIsRefusedWhole(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	values := make([]string, 0, maxVerdictBatch+1)
	for i := 0; i <= maxVerdictBatch; i++ {
		values = append(values, fmt.Sprintf("host%d.example", i))
	}
	if err := publication.SetListValues(context.Background(), "example", values, DomainVerdictInclude); err == nil {
		t.Fatalf("a batch of %d destinations was accepted", len(values))
	}
	if err := publication.SetListValues(context.Background(), "example", nil, DomainVerdictInclude); err == nil {
		t.Fatal("an empty batch was accepted")
	}
	if store.verdictWrites != 0 {
		t.Fatalf("a refused batch reached storage %d times", store.verdictWrites)
	}
	if err := publication.SetListValues(context.Background(), "example", values[:maxVerdictBatch], DomainVerdictInclude); err != nil {
		t.Fatal(err)
	}
	if got := len(publication.listTuning("example").Includes); got != maxVerdictBatch {
		t.Fatalf("standing includes = %d, want %d", got, maxVerdictBatch)
	}
}

// Two spellings of one destination are one decision, so the batch collapses
// them instead of storing the same standing verdict twice.
func TestDuplicateSpellingsCollapseToOneVerdict(t *testing.T) {
	store := &publicationFakeStore{}
	publication := tuningTestService(t, store)
	if err := publication.SetListValues(context.Background(), "example",
		[]string{"Example.NET", "example.net.", "example.net"}, DomainVerdictInclude); err != nil {
		t.Fatal(err)
	}
	if got := publication.listTuning("example").Includes; !reflect.DeepEqual(got, []string{"example.net"}) {
		t.Fatalf("includes = %#v", got)
	}
	if got := store.tunings["example"].Includes; !reflect.DeepEqual(got, []string{"example.net"}) {
		t.Fatalf("stored includes = %#v", got)
	}
	definition, _ := publication.definition("example")
	planted := 0
	for _, seed := range definition.Seeds {
		if seed.Value == "example.net" {
			planted++
		}
	}
	if planted != 1 {
		t.Fatalf("one destination planted %d seeds: %#v", planted, definition.Seeds)
	}
}

func TestListContentsMergesStaticAndObservedRows(t *testing.T) {
	store := &publicationFakeStore{observedDomains: map[string][]string{
		"vendor":   {"cdn.example.net"},
		"dns-main": {"example.com"},
	}}
	publication := tuningTestService(t, store)
	if err := publication.SetListValues(context.Background(), "example", []string{"cdn.example.net", "gone.example"}, DomainVerdictExclude); err != nil {
		t.Fatal(err)
	}
	contents, err := publication.ListContents(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if !contents.Observed {
		t.Fatalf("contents = %#v", contents)
	}
	want := []ListContentsRow{
		{Value: "cdn.example.net", Kind: "domain", Origin: "vendor", Enabled: false},
		{Value: "example.com", Kind: "domain", Origin: "catalog", Enabled: true},
		{Value: "gone.example", Kind: "domain", Enabled: false, Missing: true},
		{Value: "192.0.2.1", Kind: "ip", Origin: "catalog", Enabled: true},
	}
	if !reflect.DeepEqual(contents.Rows, want) {
		t.Fatalf("rows = %#v", contents.Rows)
	}
	if len(contents.Sources) != 2 || contents.Sources[0].ID != "dns-main" || !contents.Sources[0].Enabled || contents.Sources[1].ID != "vendor" {
		t.Fatalf("sources = %#v", contents.Sources)
	}

	// Disabling the feed removes its observed rows from the table, exactly as
	// it removes them from the next plan; the source row itself stays visible
	// with its switch off.
	if err := publication.SetListSourceEnabled(context.Background(), "example", "vendor", false); err != nil {
		t.Fatal(err)
	}
	contents, err = publication.ListContents(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range contents.Rows {
		if row.Origin == "vendor" {
			t.Fatalf("a disabled source still contributes rows: %#v", contents.Rows)
		}
	}
	if contents.Sources[1].ID != "vendor" || contents.Sources[1].Enabled {
		t.Fatalf("sources = %#v", contents.Sources)
	}
}
