package catalogyaml

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secondServiceYAML = `id: other
title: Other
components:
  web:
    required: true
seeds:
  - kind: domain_suffix
    value: other.example
    component: web
    source: manual
sources:
  - id: dns-main
    type: dns
    component: web
    config:
      names: [other.example]
`

func writeCategoryFile(t *testing.T, root, name string, payload []byte) {
	t.Helper()
	dir := filepath.Join(root, "categories")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A service belongs to as many categories as fit it. The grouping is the reason
// categories exist as their own object rather than as a field on the service.
func TestCategoryMembershipIsManyToMany(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
	if err := os.WriteFile(filepath.Join(root, "builtin", "other.yaml"), []byte(secondServiceYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCategoryFile(t, root, "video.yaml", []byte("id: video\ntitle: Видео\nservices:\n  - example\n  - other\n"))
	writeCategoryFile(t, root, "games.yaml", []byte("id: games\ntitle: Игры\nservices:\n  - example\n"))

	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	video, found := catalog.Category("video")
	if !found || video.Title != "Видео" || strings.Join(video.Services, ",") != "example,other" {
		t.Fatalf("video = %#v", video)
	}
	games, found := catalog.Category("games")
	if !found || strings.Join(games.Services, ",") != "example" {
		t.Fatalf("games = %#v", games)
	}
}

// A category naming a service the catalog does not carry would resolve to a
// silently smaller list, so the whole catalog is refused instead.
func TestCategoryNamingAnUnknownServiceIsRefused(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
	writeCategoryFile(t, root, "video.yaml", []byte("id: video\ntitle: Видео\nservices:\n  - absent\n"))
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("expected the catalog to refuse a category with an unknown service")
	}
}

func TestInvalidCategoryDocumentsAreRefused(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"no services", "id: video\ntitle: Видео\nservices: []\n"},
		{"no title", "id: video\nservices:\n  - example\n"},
		{"unknown field", "id: video\ntitle: Видео\nservices:\n  - example\nunknown: true\n"},
		{"duplicate service", "id: video\ntitle: Видео\nservices:\n  - example\n  - example\n"},
		{"traversal id", "id: ../video\ntitle: Видео\nservices:\n  - example\n"},
		{"second document", "id: video\ntitle: Видео\nservices:\n  - example\n---\nid: other\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
			writeCategoryFile(t, root, "video.yaml", []byte(test.payload))
			if _, err := Load(context.Background(), root); err == nil {
				t.Fatalf("expected %s to be refused", test.name)
			}
		})
	}
}

// A list resolves its composition through categories, so a category that gains
// a service changes what the next plan is built from. A revision that ignored
// that would claim the plan came from a catalog it did not.
func TestCatalogRevisionCoversCategories(t *testing.T) {
	first := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
	if err := os.WriteFile(filepath.Join(first, "builtin", "other.yaml"), []byte(secondServiceYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCategoryFile(t, first, "video.yaml", []byte("id: video\ntitle: Видео\nservices:\n  - example\n"))
	before, err := Load(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}

	second := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
	if err := os.WriteFile(filepath.Join(second, "builtin", "other.yaml"), []byte(secondServiceYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCategoryFile(t, second, "video.yaml", []byte("id: video\ntitle: Видео\nservices:\n  - example\n  - other\n"))
	after, err := Load(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	if before.Revision == after.Revision {
		t.Fatalf("revision did not change when a category gained a service: %s", before.Revision)
	}
}

// A catalog with no categories is legitimate: a list may name services itself.
func TestCatalogWithoutCategoriesLoads(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Categories) != 0 {
		t.Fatalf("categories = %#v", catalog.Categories)
	}
}

const communityFeedYAML = `id: example
title: Example
components:
  web:
    required: true
seeds:
  - kind: domain_suffix
    value: example.com
    component: web
    source: manual
sources:
  - id: feed-main
    type: http
    component: web
    config:
      url: https://lists.example.org/example.lst
      format: domain-list
      class: community
`

// A feed says what it is. Only a vendor's own publication about itself is
// official; a third-party list is community curation.
func TestFeedDeclaresItsSourceClass(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "service.yaml", []byte(communityFeedYAML))
	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	service, found := catalog.Service("example")
	if !found || len(service.Sources) != 1 {
		t.Fatalf("service = %#v", service)
	}
	if service.Sources[0].Class != "community" || service.Sources[0].Format != "domain-list" {
		t.Fatalf("source = %#v", service.Sources[0])
	}
}

// Filing the same bytes under a different class is a different observation
// semantic, so the stored observations of the old class must not be reused.
func TestSourceRevisionCoversTheClass(t *testing.T) {
	community := writeCatalogFile(t, "builtin", "service.yaml", []byte(communityFeedYAML))
	official := writeCatalogFile(t, "builtin", "service.yaml",
		[]byte(strings.Replace(communityFeedYAML, "class: community", "class: official", 1)))
	left, err := Load(context.Background(), community)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Load(context.Background(), official)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := left.Service("example")
	b, _ := right.Service("example")
	if a.Sources[0].Revision == b.Sources[0].Revision {
		t.Fatalf("revision ignored the class: %s", a.Sources[0].Revision)
	}
}

func TestInvalidFeedClassIsRefused(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "service.yaml",
		[]byte(strings.Replace(communityFeedYAML, "class: community", "class: observed", 1)))
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("expected an observed feed class to be refused")
	}
}

// A DNS source has no class of its own: what it records is what this machine
// saw, and saying otherwise would file an observation as a publication.
func TestDNSSourceRefusesAClass(t *testing.T) {
	payload := strings.Replace(validCatalogYAML, "      names: [example.com]", "      names: [example.com]\n      class: community", 1)
	root := writeCatalogFile(t, "builtin", "service.yaml", []byte(payload))
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("expected a class on a DNS source to be refused")
	}
}

// The retired key is read for one minor version so an operator's edited
// catalog survives the upgrade (ADR 0039). Both keys mean the same thing, so a
// file naming both is refused rather than reconciled by guesswork.
func TestCategoryReadsTheRetiredMembershipKeyButRefusesBoth(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"current key", "id: video\ntitle: Видео\nlists:\n  - example\n"},
		{"retired key", "id: video\ntitle: Видео\nservices:\n  - example\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
			writeCategoryFile(t, root, "video.yaml", []byte(test.payload))
			catalog, err := Load(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			video, found := catalog.Category("video")
			if !found || strings.Join(video.Services, ",") != "example" {
				t.Fatalf("video = %#v", video)
			}
		})
	}

	t.Run("both keys", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "service.yaml", []byte(validCatalogYAML))
		writeCategoryFile(t, root, "video.yaml", []byte("id: video\ntitle: Видео\nlists:\n  - example\nservices:\n  - example\n"))
		_, err := Load(context.Background(), root)
		if err == nil {
			t.Fatal("a category naming both keys must be refused")
		}
		if !strings.Contains(err.Error(), "retired services key") {
			t.Fatalf("error=%v, want it to name the retired key", err)
		}
	})
}
