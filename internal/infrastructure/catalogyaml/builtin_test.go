package catalogyaml

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if len(catalog.Services) < 20 || len(catalog.Categories) < 5 {
		t.Fatalf("catalog holds %d services and %d categories", len(catalog.Services), len(catalog.Categories))
	}
	for id, service := range catalog.Services {
		if service.Title == "" || len(service.Sources) == 0 || len(service.Seeds) == 0 {
			t.Fatalf("service %q = %#v", id, service)
		}
		for _, seed := range service.Seeds {
			if seed.Kind != domain.RuleDomainSuffix || seed.SourceClass != domain.SourceManual {
				t.Fatalf("service %q seed widened automatic evidence: %#v", id, seed)
			}
		}
		for _, source := range service.Sources {
			// Every shipped source is a third party's curation, and saying
			// official would tell the planner the vendor published it itself.
			if source.Type != domain.SourceHTTP || source.Class != domain.SourceCommunity {
				t.Fatalf("service %q source = %#v", id, source)
			}
			if !strings.HasPrefix(source.URL, "https://") {
				t.Fatalf("service %q source is not fetched over HTTPS: %#v", id, source)
			}
			if source.Format != domain.FeedFormatText && source.Format != domain.FeedFormatDomainList {
				t.Fatalf("service %q source format = %q", id, source.Format)
			}
		}
	}

	// A service in no category is reachable only by searching for it by name,
	// which is not how anyone finds something they have not heard of.
	grouped := make(map[string]struct{}, len(catalog.Services))
	for _, category := range catalog.Categories {
		for _, member := range category.Services {
			grouped[member] = struct{}{}
		}
	}
	for id := range catalog.Services {
		if _, found := grouped[id]; !found {
			t.Fatalf("service %q belongs to no category", id)
		}
	}
}

// A service carried by two categories is the case categories exist for, and
// losing it would quietly turn the shipped catalog back into one label each.
func TestShippedCatalogKeepsAServiceInMoreThanOneCategory(t *testing.T) {
	catalog, err := Load(context.Background(), filepath.Join(repositoryRoot(t), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	count := make(map[string]int, len(catalog.Services))
	for _, category := range catalog.Categories {
		for _, member := range category.Services {
			count[member]++
		}
	}
	for _, total := range count {
		if total > 1 {
			return
		}
	}
	t.Fatal("no shipped service belongs to more than one category")
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
	service, found := fixture.Service("example")
	if !found || len(service.Sources) != 1 {
		t.Fatalf("diagnostic fixture = %#v", service)
	}
	if service.Sources[0].Type != domain.SourceDNS || len(service.Sources[0].Names) != 1 {
		t.Fatalf("diagnostic fixture source = %#v", service.Sources[0])
	}
}
