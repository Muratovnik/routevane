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
	definitions := map[string]domain.ListDefinition{}
	for _, id := range []string{"youtube", "discord", "roblox"} {
		definitions[id] = domain.ListDefinition{
			ID: id, CatalogRevision: strings.Repeat("c", 64),
			Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
			Seeds:      []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual}},
		}
	}
	categories := map[string]domain.CategoryDefinition{
		"communication": {ID: "communication", Title: "Общение", Lists: []string{"discord"}},
		"games":         {ID: "games", Title: "Игры", Lists: []string{"discord", "roblox"}},
	}
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	publication, err := NewPublicationService(PublicationConfig{
		Definitions: definitions, Categories: categories,
		Targets: map[string]domain.TargetDefinition{target.ID: target}, TargetRevision: strings.Repeat("t", 64),
		Store: &publicationFakeStore{}, Files: &publicationFakeFiles{},
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return publication
}

func profileWith(lists, categories, exclusions []string) Profile {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return Profile{ID: strings.Repeat("1", 32), Name: "list", Lists: lists, Categories: categories, Exclusions: exclusions, CreatedAt: now, UpdatedAt: now}
}

func joined(values []string) string { return strings.Join(values, ",") }

// Discord is in both categories. It must be planned once, or the target's rule
// budget is charged twice for one service.
func TestResolutionDeduplicatesAcrossCategories(t *testing.T) {
	publication := categoryTestService(t)
	got := publication.ResolvedLists(profileWith(nil, []string{"communication", "games"}, nil))
	if joined(got) != "discord,roblox" {
		t.Fatalf("resolved = %v", got)
	}
}

// A service named directly and also carried by a category appears once.
func TestResolutionDeduplicatesNamedAgainstReferenced(t *testing.T) {
	publication := categoryTestService(t)
	got := publication.ResolvedLists(profileWith([]string{"discord"}, []string{"games"}, nil))
	if joined(got) != "discord,roblox" {
		t.Fatalf("resolved = %v", got)
	}
}

// An exclusion removes a member of a referenced category and keeps the
// reference: the rest of the category still arrives.
func TestExclusionRemovesOneMemberAndKeepsTheReference(t *testing.T) {
	publication := categoryTestService(t)
	got := publication.ResolvedLists(profileWith(nil, []string{"games"}, []string{"roblox"}))
	if joined(got) != "discord" {
		t.Fatalf("resolved = %v", got)
	}
}

// A category that gains a service reaches every list referencing it without an
// edit. That is what makes a reference different from a copy.
func TestAGrowingCategoryReachesTheProfileWithoutAnEdit(t *testing.T) {
	publication := categoryTestService(t)
	profile := profileWith(nil, []string{"communication"}, nil)
	if joined(publication.ResolvedLists(profile)) != "discord" {
		t.Fatalf("resolved before = %v", publication.ResolvedLists(profile))
	}
	publication.config.Categories["communication"] = domain.CategoryDefinition{ID: "communication", Title: "Общение", Lists: []string{"discord", "youtube"}}
	if joined(publication.ResolvedLists(profile)) != "discord,youtube" {
		t.Fatalf("resolved after = %v", publication.ResolvedLists(profile))
	}
}

func TestPrioritySurvivesLiveCategoryChanges(t *testing.T) {
	publication := categoryTestService(t)
	profile := profileWith(nil, []string{"games"}, nil)
	profile.Priority = []string{"roblox", "discord"}
	if joined(publication.ResolvedLists(profile)) != "roblox,discord" {
		t.Fatalf("resolved before = %v", publication.ResolvedLists(profile))
	}
	publication.config.Categories["games"] = domain.CategoryDefinition{
		ID: "games", Title: "Игры", Lists: []string{"discord", "roblox", "youtube"},
	}
	if joined(publication.ResolvedLists(profile)) != "roblox,discord,youtube" {
		t.Fatalf("resolved after growth = %v", publication.ResolvedLists(profile))
	}
	publication.config.Categories["games"] = domain.CategoryDefinition{
		ID: "games", Title: "Игры", Lists: []string{"roblox", "youtube"},
	}
	if joined(publication.ResolvedLists(profile)) != "roblox,youtube" {
		t.Fatalf("resolved after removal = %v", publication.ResolvedLists(profile))
	}
}

// A category that left the catalog must not fail the read or silently shrink
// the list without saying so.
func TestAVanishedCategoryIsReportedRatherThanHidden(t *testing.T) {
	publication := categoryTestService(t)
	profile := profileWith([]string{"youtube"}, []string{"games", "absent"}, nil)
	if joined(publication.ResolvedLists(profile)) != "discord,roblox,youtube" {
		t.Fatalf("resolved = %v", publication.ResolvedLists(profile))
	}
	if joined(publication.MissingCategories(profile)) != "absent" {
		t.Fatalf("missing = %v", publication.MissingCategories(profile))
	}
}

func TestCompositionValidation(t *testing.T) {
	publication := categoryTestService(t)
	tests := []struct {
		name      string
		requested ProfileComposition
		valid     bool
	}{
		{"services only", ProfileComposition{Lists: []string{"youtube"}}, true},
		{"categories only", ProfileComposition{Categories: []string{"games"}}, true},
		{"unknown service", ProfileComposition{Lists: []string{"absent"}}, false},
		{"unknown category", ProfileComposition{Categories: []string{"absent"}}, false},
		{"nothing at all", ProfileComposition{}, false},
		{"named and excluded", ProfileComposition{Lists: []string{"youtube"}, Exclusions: []string{"youtube"}}, false},
		{"everything excluded", ProfileComposition{Categories: []string{"communication"}, Exclusions: []string{"discord"}}, false},
		{"priority", ProfileComposition{Categories: []string{"games"}, Priority: []string{"roblox", "discord"}}, true},
		{"duplicate priority", ProfileComposition{Categories: []string{"games"}, Priority: []string{"roblox", "roblox"}}, false},
		{"foreign priority", ProfileComposition{Categories: []string{"games"}, Priority: []string{"youtube"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := publication.validComposition(test.requested)
			if test.valid != (err == nil) {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}

// A category naming a service the composition does not carry would resolve to a
// silently smaller list at build time.
func TestCompositionRootRefusesACategoryWithAnUnknownList(t *testing.T) {
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	_, err := NewPublicationService(PublicationConfig{
		Definitions: map[string]domain.ListDefinition{"youtube": {ID: "youtube", CatalogRevision: strings.Repeat("c", 64)}},
		Categories:  map[string]domain.CategoryDefinition{"games": {ID: "games", Title: "Игры", Lists: []string{"absent"}}},
		Targets:     map[string]domain.TargetDefinition{target.ID: target}, TargetRevision: strings.Repeat("t", 64),
		Store: &publicationFakeStore{}, Files: &publicationFakeFiles{},
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err == nil {
		t.Fatal("expected the composition to be refused")
	}
}
