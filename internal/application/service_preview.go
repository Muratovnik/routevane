package application

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ServicePreview is a transient, read-only look at the automatic catalog
// sources for one service. It deliberately contains domain names but only
// counts address material: exact IP evidence belongs to build diagnostics,
// while this projection answers the composition question a list editor asks.
type ServicePreview struct {
	ServiceID    string                 `json:"list_id"`
	Sources      []ServiceSourcePreview `json:"sources"`
	Domains      []string               `json:"domains"`
	DomainCount  int                    `json:"domain_count"`
	AddressCount int                    `json:"address_count"`
	PrefixCount  int                    `json:"prefix_count"`
	SkippedCount int                    `json:"skipped_count"`
}

// ServiceSourcePreview keeps partial success visible. One unavailable source
// does not erase what the other sources returned, and failures use the same
// bounded codes as persisted refresh cycles rather than exposing raw errors.
type ServiceSourcePreview struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	ErrorCode    string   `json:"error_code,omitempty"`
	Domains      []string `json:"domains"`
	DomainCount  int      `json:"domain_count"`
	AddressCount int      `json:"address_count"`
	PrefixCount  int      `json:"prefix_count"`
	SkippedCount int      `json:"skipped_count"`
}

// PreviewService observes the configured automatic sources without recording
// a source cycle, changing a list, or publishing an artifact. The explicit UI
// action that calls it is therefore safe to use before a service is selected.
func (s *PublicationService) PreviewService(ctx context.Context, serviceID string) (ServicePreview, error) {
	preview := ServicePreview{ServiceID: serviceID, Sources: []ServiceSourcePreview{}, Domains: []string{}}
	if ctx == nil || s == nil || s.config.Clock == nil {
		return preview, fmt.Errorf("invalid service preview composition")
	}
	definition, ok := s.definition(serviceID)
	if !ok {
		return preview, ErrNotFound
	}
	observedAt := s.config.Clock.Now().UTC()
	if observedAt.IsZero() {
		return preview, fmt.Errorf("clock returned zero time")
	}

	sources := append([]domain.SourceDefinition(nil), definition.Sources...)
	slices.SortFunc(sources, func(a, b domain.SourceDefinition) int { return cmp.Compare(a.ID, b.ID) })
	allDomains := make(map[string]struct{})
	allAddresses := make(map[string]struct{})
	allPrefixes := make(map[string]struct{})
	for _, definitionSource := range sources {
		if err := ctx.Err(); err != nil {
			return preview, err
		}
		item := ServiceSourcePreview{ID: definitionSource.ID, Type: string(definitionSource.Type), Status: "failed", Domains: []string{}}
		source, registered := s.config.Sources[definitionSource.Type]
		if !registered || source == nil {
			item.ErrorCode = SourceErrorCode(definitionSource.Type)
			preview.Sources = append(preview.Sources, item)
			continue
		}
		sourceCtx, cancel := context.WithTimeout(ctx, SourceTimeout)
		result, err := source.Observe(sourceCtx, SourceRequest{
			ServiceID:      definition.ID,
			ComponentID:    definitionSource.ComponentID,
			SourceID:       definitionSource.ID,
			SourceRevision: definitionSource.Revision,
			Names:          append([]string(nil), definitionSource.Names...),
			URL:            definitionSource.URL,
			Format:         definitionSource.Format,
			SourceClass:    definitionSource.Class,
			ObservedAt:     observedAt,
		})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return preview, ctx.Err()
			}
			item.ErrorCode = SourceErrorCode(definitionSource.Type)
			preview.Sources = append(preview.Sources, item)
			continue
		}

		item.Status = "ready"
		item.SkippedCount = result.Skipped
		itemDomains := make(map[string]struct{})
		itemAddresses := make(map[string]struct{})
		itemPrefixes := make(map[string]struct{})
		for _, sighting := range result.Sightings {
			if !sighting.Resource.IsValid() {
				continue
			}
			value := sighting.Resource.CanonicalValue()
			switch sighting.Resource.Kind {
			case domain.ResourceDomain:
				itemDomains[value] = struct{}{}
				allDomains[value] = struct{}{}
			case domain.ResourceIP:
				itemAddresses[value] = struct{}{}
				allAddresses[value] = struct{}{}
			case domain.ResourcePrefix:
				itemPrefixes[value] = struct{}{}
				allPrefixes[value] = struct{}{}
			}
		}
		item.Domains = sortedKeys(itemDomains)
		item.DomainCount = len(itemDomains)
		item.AddressCount = len(itemAddresses)
		item.PrefixCount = len(itemPrefixes)
		preview.SkippedCount += result.Skipped
		preview.Sources = append(preview.Sources, item)
	}
	preview.Domains = sortedKeys(allDomains)
	preview.DomainCount = len(allDomains)
	preview.AddressCount = len(allAddresses)
	preview.PrefixCount = len(allPrefixes)
	return preview, nil
}

// sortedKeys answers a string set in one order. The result is an empty slice
// rather than a nil one because a screen reads it as a JSON array, and null is
// not a list it can iterate.
func sortedKeys(values map[string]struct{}) []string {
	result := slices.AppendSeq(make([]string, 0, len(values)), maps.Keys(values))
	slices.Sort(result)
	return result
}
