package catalogyaml

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository root")
	}
	return filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
}

// The shipped catalog is offered verbatim on the local control surface, so
// every claim it makes must hold before anything reads it.
func TestShippedCatalogIsTypedAndDeclared(t *testing.T) {
	catalog, err := Load(context.Background(), filepath.Join(repositoryRoot(t), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Lists) < 20 || len(catalog.Categories) < 5 {
		t.Fatalf("catalog holds %d lists and %d categories", len(catalog.Lists), len(catalog.Categories))
	}
	for id, list := range catalog.Lists {
		if list.Title == "" || len(list.Sources) == 0 || len(list.Seeds) == 0 {
			t.Fatalf("list %q = %#v", id, list)
		}
		for _, seed := range list.Seeds {
			if seed.Kind != domain.RuleDomainSuffix || seed.SourceClass != domain.SourceManual {
				t.Fatalf("service %q seed widened automatic evidence: %#v", id, seed)
			}
		}
		for _, source := range list.Sources {
			// Every shipped source is a third party's curation, and saying
			// official would tell the planner the vendor published it itself.
			if source.Type != domain.SourceHTTP || source.Class != domain.SourceCommunity {
				t.Fatalf("list %q source = %#v", id, source)
			}
			if !strings.HasPrefix(source.URL, "https://") {
				t.Fatalf("service %q source is not fetched over HTTPS: %#v", id, source)
			}
			if source.Format != domain.FeedFormatText && source.Format != domain.FeedFormatDomainList {
				t.Fatalf("list %q source format = %q", id, source.Format)
			}
		}
	}

	// A list in no category is reachable only by searching for it by name,
	// which is not how anyone finds something they have not heard of.
	grouped := make(map[string]struct{}, len(catalog.Lists))
	for _, category := range catalog.Categories {
		for _, member := range category.Lists {
			grouped[member] = struct{}{}
		}
	}
	for id := range catalog.Lists {
		if _, found := grouped[id]; !found {
			t.Fatalf("list %q belongs to no category", id)
		}
	}
}

// A list carried by two categories is the case categories exist for, and
// losing it would quietly turn the shipped catalog back into one label each.
func TestShippedCatalogKeepsAListInMoreThanOneCategory(t *testing.T) {
	catalog, err := Load(context.Background(), filepath.Join(repositoryRoot(t), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	count := make(map[string]int, len(catalog.Lists))
	for _, category := range catalog.Categories {
		for _, member := range category.Lists {
			count[member]++
		}
	}
	for _, total := range count {
		if total > 1 {
			return
		}
	}
	t.Fatal("no shipped list belongs to more than one category")
}

func TestDiagnosticExampleFixtureStaysOutOfTheProductCatalog(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "catalog", "builtin", "example.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnostic example definition returned to the product catalog: %v", err)
	}
	fixture, err := Load(context.Background(), filepath.Join(root, "testdata", "pipeline", "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	list, found := fixture.List("example")
	if !found || len(list.Sources) != 1 {
		t.Fatalf("diagnostic fixture = %#v", list)
	}
	if list.Sources[0].Type != domain.SourceDNS || len(list.Sources[0].Names) != 1 {
		t.Fatalf("diagnostic fixture source = %#v", list.Sources[0])
	}
}

func TestShippedCursorStaysBoundedToItsOwnDomains(t *testing.T) {
	catalog, err := Load(context.Background(), filepath.Join(repositoryRoot(t), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	list, found := catalog.List("cursor")
	if !found || list.Title != "Cursor" {
		t.Fatalf("Cursor is missing from the shipped library: %#v", list)
	}
	if len(list.Components) != 1 || list.Components[0] != (domain.ComponentDefinition{ID: "web", Required: true}) {
		t.Fatalf("Cursor components = %#v", list.Components)
	}
	if len(list.Seeds) != 1 || list.Seeds[0].Kind != domain.RuleDomainSuffix || list.Seeds[0].Value != "cursor.com" || list.Seeds[0].SourceClass != domain.SourceManual {
		t.Fatalf("Cursor seeds = %#v", list.Seeds)
	}
	if len(list.Sources) != 1 {
		t.Fatalf("Cursor must not pull infrastructure or process sources: %#v", list.Sources)
	}
	source := list.Sources[0]
	if source.ID != "v2fly" || source.Type != domain.SourceHTTP || source.Class != domain.SourceCommunity || source.ComponentID != "web" || source.Format != domain.FeedFormatDomainList || source.URL != "https://raw.githubusercontent.com/v2fly/domain-list-community/master/data/cursor" {
		t.Fatalf("Cursor source = %#v", source)
	}
	if !slices.Contains(catalog.Categories["development"].Lists, "cursor") {
		t.Fatal("Cursor must be discoverable in Development")
	}
}

func TestShippedAdditionalDomainListsHaveOnlyTheirOwnSource(t *testing.T) {
	catalog, err := Load(context.Background(), filepath.Join(repositoryRoot(t), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct{ id, title, seed, category string }{
		{"github-copilot", "GitHub Copilot", "githubcopilot.com", "development"},
		{"twitch", "Twitch", "twitch.tv", "video"},
		{"kinopub", "Kinopub", "kino.pub", "video"},
	} {
		t.Run(candidate.id, func(t *testing.T) {
			list, found := catalog.List(candidate.id)
			if !found || list.Title != candidate.title {
				t.Fatalf("missing list %s: %#v", candidate.id, list)
			}
			if len(list.Seeds) != 1 || list.Seeds[0].Value != candidate.seed || list.Seeds[0].Kind != domain.RuleDomainSuffix || list.Seeds[0].SourceClass != domain.SourceManual {
				t.Fatalf("unexpected seeds: %#v", list.Seeds)
			}
			if len(list.Sources) != 1 {
				t.Fatalf("unexpected sources: %#v", list.Sources)
			}
			source := list.Sources[0]
			if source.ID != "v2fly" || source.Type != domain.SourceHTTP || source.Class != domain.SourceCommunity || source.Format != domain.FeedFormatDomainList || source.URL != "https://raw.githubusercontent.com/v2fly/domain-list-community/master/data/"+candidate.id {
				t.Fatalf("source = %#v", source)
			}
			if !slices.Contains(catalog.Categories[candidate.category].Lists, candidate.id) {
				t.Fatalf("missing %s membership", candidate.category)
			}
		})
	}
}
