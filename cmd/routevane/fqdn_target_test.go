package main

import (
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

func TestFQDNTargetAliasMustMatchEnforcedGroupBounds(t *testing.T) {
	for _, test := range []struct {
		name            string
		entries, groups int
		accepted        bool
	}{
		{"same format under another target", 300, 128, true},
		{"smaller groups would require different sharding", 100, 128, false},
		{"larger groups exceed the validator", 301, 128, false},
		{"smaller device budget is not enforced", 300, 127, false},
		{"larger device budget exceeds the deployer", 300, 129, false},
		{"missing group budgets", 0, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := domain.TargetDefinition{
				ID: "home-router", RendererID: keeneticdns.ID, FormatKey: keeneticdns.Version,
				Constraints: domain.TargetConstraints{SupportsDomainSuffix: true, SupportsDynamicDNSSet: true,
					SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true,
					MaxRules: 1000, MaxArtifactSize: 64000, MaxEntriesPerList: test.entries, MaxLists: test.groups},
			}
			catalog := catalogyaml.Catalog{Targets: map[string]domain.TargetDefinition{target.ID: target}}
			targets := catalogTargets(catalog, application.RendererRegistry{keeneticdns.ID: keeneticdns.Renderer{}})
			_, accepted := targets[target.ID]
			if accepted != test.accepted {
				t.Fatalf("target offered=%v want=%v for entries=%d groups=%d", accepted, test.accepted, test.entries, test.groups)
			}
		})
	}
}
