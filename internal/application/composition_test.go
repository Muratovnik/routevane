package application

import (
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// categoryTestService wires three services and two overlapping categories, the
// shape ADR 0016 exists for: one service reachable through more than one
// grouping.
func categoryTestService(t *testing.T) *PublicationService {
	t.Helper()
	definitions := map[string]domain.ServiceDefinition{}
	for _, id := range []string{"youtube", "discord", "roblox"} {
		definitions[id] = domain.ServiceDefinition{
			ID: id, CatalogRevision: strings.Repeat("c", 64),
			Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
			Seeds:      []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual}},
		}
	}
	categories := map[string]domain.CategoryDefinition{
		"communication": {ID: "communication", Title: "Общение", Services: []string{"discord"}},
		"games":         {ID: "games", Title: "Игры", Services: []string{"discord", "roblox"}},
	}
	target := domain.TargetProfile{ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	service, err := NewPublicationService(PublicationConfig{
		Definitions: definitions, Categories: categories,
		Targets: map[string]domain.TargetProfile{target.ID: target}, TargetRevision: strings.Repeat("t", 64),
		Store: &publicationFakeStore{}, Files: &publicationFakeFiles{},
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func listWith(services, categories, exclusions []string) List {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return List{ID: strings.Repeat("1", 32), Name: "list", Services: services, Categories: categories, Exclusions: exclusions, CreatedAt: now, UpdatedAt: now}
}

func joined(values []string) string { return strings.Join(values, ",") }

// Discord is in both categories. It must be planned once, or the target's rule
// budget is charged twice for one service.
func TestResolutionDeduplicatesAcrossCategories(t *testing.T) {
	service := categoryTestService(t)
	got := service.ResolvedServices(listWith(nil, []string{"communication", "games"}, nil))
	if joined(got) != "discord,roblox" {
		t.Fatalf("resolved = %v", got)
	}
}

// A service named directly and also carried by a category appears once.
func TestResolutionDeduplicatesNamedAgainstReferenced(t *testing.T) {
	service := categoryTestService(t)
	got := service.ResolvedServices(listWith([]string{"discord"}, []string{"games"}, nil))
	if joined(got) != "discord,roblox" {
		t.Fatalf("resolved = %v", got)
	}
}

// An exclusion removes a member of a referenced category and keeps the
// reference: the rest of the category still arrives.
func TestExclusionRemovesOneMemberAndKeepsTheReference(t *testing.T) {
	service := categoryTestService(t)
	got := service.ResolvedServices(listWith(nil, []string{"games"}, []string{"roblox"}))
	if joined(got) != "discord" {
		t.Fatalf("resolved = %v", got)
	}
}

// A category that gains a service reaches every list referencing it without an
// edit. That is what makes a reference different from a copy.
func TestAGrowingCategoryReachesTheListWithoutAnEdit(t *testing.T) {
	service := categoryTestService(t)
	list := listWith(nil, []string{"communication"}, nil)
	if joined(service.ResolvedServices(list)) != "discord" {
		t.Fatalf("resolved before = %v", service.ResolvedServices(list))
	}
	service.config.Categories["communication"] = domain.CategoryDefinition{ID: "communication", Title: "Общение", Services: []string{"discord", "youtube"}}
	if joined(service.ResolvedServices(list)) != "discord,youtube" {
		t.Fatalf("resolved after = %v", service.ResolvedServices(list))
	}
}

func TestPrioritySurvivesLiveCategoryChanges(t *testing.T) {
	service := categoryTestService(t)
	list := listWith(nil, []string{"games"}, nil)
	list.Priority = []string{"roblox", "discord"}
	if joined(service.ResolvedServices(list)) != "roblox,discord" {
		t.Fatalf("resolved before = %v", service.ResolvedServices(list))
	}
	service.config.Categories["games"] = domain.CategoryDefinition{
		ID: "games", Title: "Игры", Services: []string{"discord", "roblox", "youtube"},
	}
	if joined(service.ResolvedServices(list)) != "roblox,discord,youtube" {
		t.Fatalf("resolved after growth = %v", service.ResolvedServices(list))
	}
	service.config.Categories["games"] = domain.CategoryDefinition{
		ID: "games", Title: "Игры", Services: []string{"roblox", "youtube"},
	}
	if joined(service.ResolvedServices(list)) != "roblox,youtube" {
		t.Fatalf("resolved after removal = %v", service.ResolvedServices(list))
	}
}

// A category that left the catalog must not fail the read or silently shrink
// the list without saying so.
func TestAVanishedCategoryIsReportedRatherThanHidden(t *testing.T) {
	service := categoryTestService(t)
	list := listWith([]string{"youtube"}, []string{"games", "absent"}, nil)
	if joined(service.ResolvedServices(list)) != "discord,roblox,youtube" {
		t.Fatalf("resolved = %v", service.ResolvedServices(list))
	}
	if joined(service.MissingCategories(list)) != "absent" {
		t.Fatalf("missing = %v", service.MissingCategories(list))
	}
}

func TestCompositionValidation(t *testing.T) {
	service := categoryTestService(t)
	tests := []struct {
		name      string
		requested ListComposition
		valid     bool
	}{
		{"services only", ListComposition{Services: []string{"youtube"}}, true},
		{"categories only", ListComposition{Categories: []string{"games"}}, true},
		{"unknown service", ListComposition{Services: []string{"absent"}}, false},
		{"unknown category", ListComposition{Categories: []string{"absent"}}, false},
		{"nothing at all", ListComposition{}, false},
		{"named and excluded", ListComposition{Services: []string{"youtube"}, Exclusions: []string{"youtube"}}, false},
		{"everything excluded", ListComposition{Categories: []string{"communication"}, Exclusions: []string{"discord"}}, false},
		{"priority", ListComposition{Categories: []string{"games"}, Priority: []string{"roblox", "discord"}}, true},
		{"duplicate priority", ListComposition{Categories: []string{"games"}, Priority: []string{"roblox", "roblox"}}, false},
		{"foreign priority", ListComposition{Categories: []string{"games"}, Priority: []string{"youtube"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.validComposition(test.requested)
			if test.valid != (err == nil) {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}

// A category naming a service the composition does not carry would resolve to a
// silently smaller list at build time.
func TestCompositionRootRefusesACategoryWithAnUnknownService(t *testing.T) {
	target := domain.TargetProfile{ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	_, err := NewPublicationService(PublicationConfig{
		Definitions: map[string]domain.ServiceDefinition{"youtube": {ID: "youtube", CatalogRevision: strings.Repeat("c", 64)}},
		Categories:  map[string]domain.CategoryDefinition{"games": {ID: "games", Title: "Игры", Services: []string{"absent"}}},
		Targets:     map[string]domain.TargetProfile{target.ID: target}, TargetRevision: strings.Repeat("t", 64),
		Store: &publicationFakeStore{}, Files: &publicationFakeFiles{},
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err == nil {
		t.Fatal("expected the composition to be refused")
	}
}
