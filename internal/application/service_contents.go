package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ServiceContents is the one table the service card renders: every
// destination the service currently stands for — domains, IP addresses and
// networks — each with where it came from and whether the operator keeps it
// on. It merges the static definition with the stored observations — the same
// material the next build reads — so the card and the artifact cannot tell
// two different stories.
type ServiceContents struct {
	ServiceID string                  `json:"service_id"`
	Rows      []ServiceContentsRow    `json:"rows"`
	Sources   []ServiceContentsSource `json:"sources"`
	// Observed reports whether stored observations exist for this service. A
	// service that was never refreshed shows only its static rows, and says so
	// instead of pretending the automatic material is empty.
	Observed bool `json:"observed"`
}

// ServiceContentsRow is one destination of the service. Kind is "domain",
// "ip" or "prefix". Origin names where the value comes from: "catalog" for a
// shipped seed, "manual" for an operator entry, otherwise the id of the
// automatic source that observed it. Missing marks a standing verdict whose
// value no current material offers.
type ServiceContentsRow struct {
	Value   string `json:"value"`
	Kind    string `json:"kind"`
	Origin  string `json:"origin"`
	Enabled bool   `json:"enabled"`
	Missing bool   `json:"missing,omitempty"`
}

// ServiceContentsSource is one automatic source with the operator's switch.
// The URL is disclosed only for the operator's own feeds: it is their data,
// while a catalog feed's configuration stays behind the catalog.
type ServiceContentsSource struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Custom  bool   `json:"custom"`
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
}

func (s *PublicationService) ServiceContents(ctx context.Context, serviceID string) (ServiceContents, error) {
	base, ok := s.baseDefinition(serviceID)
	if !ok {
		return ServiceContents{}, ErrNotFound
	}
	tuning := s.serviceTuning(serviceID)
	disabled := make(map[string]struct{}, len(tuning.DisabledSources))
	for _, id := range tuning.DisabledSources {
		disabled[id] = struct{}{}
	}
	excluded := make(map[string]struct{}, len(tuning.Excludes))
	for _, value := range tuning.Excludes {
		excluded[value] = struct{}{}
	}

	contents := ServiceContents{ServiceID: serviceID, Rows: []ServiceContentsRow{}, Sources: []ServiceContentsSource{}}
	for _, source := range base.Sources {
		_, off := disabled[source.ID]
		contents.Sources = append(contents.Sources, ServiceContentsSource{
			ID: source.ID, Type: string(source.Type), Enabled: !off,
		})
	}
	for _, feed := range tuning.CustomSources {
		_, off := disabled[feed.ID]
		contents.Sources = append(contents.Sources, ServiceContentsSource{
			ID: feed.ID, Type: string(domain.SourceHTTP), Custom: true, Enabled: !off, URL: feed.URL,
		})
	}
	slices.SortFunc(contents.Sources, func(a, b ServiceContentsSource) int { return cmp.Compare(a.ID, b.ID) })

	// The static truth: shipped seeds are catalog rows for a shipped service
	// and the operator's own rows for a custom service; verdict includes are
	// always the operator's.
	staticOrigin := "catalog"
	if _, shipped := s.config.Definitions[serviceID]; !shipped {
		staticOrigin = "manual"
	}
	rows := make(map[string]*ServiceContentsRow)
	addRow := func(value, kind, origin string) {
		if existing, seen := rows[value]; seen {
			// A value several origins offer keeps the strongest claim: the
			// operator's own word over the catalog, the catalog over a feed.
			if originRank(origin) < originRank(existing.Origin) {
				existing.Origin = origin
			}
			return
		}
		_, off := excluded[value]
		rows[value] = &ServiceContentsRow{Value: value, Kind: kind, Origin: origin, Enabled: !off}
	}
	for _, seed := range base.Seeds {
		addRow(seed.Value, rowKind(seed.Kind), staticOrigin)
	}
	for _, value := range tuning.Includes {
		if canonical, kind, err := domain.NormalizeRuleValue(value); err == nil {
			addRow(canonical, rowKind(kind), "manual")
		}
	}

	// The observed truth: the same stored sightings the next build reads,
	// filtered by the effective source revisions, so a disabled source's
	// domains leave this table exactly when they leave the plans.
	effective := s.tunedDefinition(base)
	cutoff := s.config.Clock.Now().UTC()
	if cutoff.IsZero() {
		return ServiceContents{}, fmt.Errorf("clock returned zero time")
	}
	rawProfile := domain.RawJSONTargetProfile()
	snapshot, err := s.config.Store.ReadPlanningSnapshot(ctx, serviceID, sourceRevisions(effective), rawProfile.ProfileKey, cutoff)
	switch {
	case errors.Is(err, ErrNotFound):
		// Never refreshed: the static rows are the whole story so far.
	case err != nil:
		return ServiceContents{}, fmt.Errorf("read service observations: %w", err)
	default:
		contents.Observed = true
		for _, sighting := range snapshot.Sightings {
			if sighting.Validity != domain.ValidityValid {
				continue
			}
			addRow(sighting.Resource.CanonicalValue(), string(sighting.Resource.Kind), sighting.SourceID)
		}
	}

	// A standing verdict whose value no material offers anymore is still the
	// operator's decision; it stays visible instead of silently evaporating.
	for _, value := range tuning.Excludes {
		if _, seen := rows[value]; !seen {
			kind := string(domain.ResourceDomain)
			if _, ruleKind, err := domain.NormalizeRuleValue(value); err == nil {
				kind = rowKind(ruleKind)
			}
			rows[value] = &ServiceContentsRow{Value: value, Kind: kind, Enabled: false, Missing: true}
		}
	}

	for _, row := range rows {
		contents.Rows = append(contents.Rows, *row)
	}
	// Domains first, then addresses, then networks; each block alphabetical, so
	// the readable names lead and the numeric material follows.
	slices.SortFunc(contents.Rows, func(a, b ServiceContentsRow) int {
		return cmp.Or(cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)), cmp.Compare(a.Value, b.Value))
	})
	return contents, nil
}

// rowKind folds the six rule kinds into the three the card names.
func rowKind(kind domain.RuleKind) string {
	switch {
	case kind.IsIP():
		return string(domain.ResourceIP)
	case kind.IsPrefix():
		return string(domain.ResourcePrefix)
	default:
		return string(domain.ResourceDomain)
	}
}

func kindRank(kind string) int {
	switch kind {
	case string(domain.ResourceDomain):
		return 0
	case string(domain.ResourceIP):
		return 1
	default:
		return 2
	}
}

// originRank orders competing claims about one domain: the operator's own word
// first, the shipped catalog second, an automatic source last.
func originRank(origin string) int {
	switch origin {
	case "manual":
		return 0
	case "catalog":
		return 1
	default:
		return 2
	}
}

// RefreshServiceByID re-observes one service's automatic sources and persists
// the result, so the contents table and the next build read the same fresh
// material. It is the service card's refresh: no list is touched and nothing
// is published.
func (s *PublicationService) RefreshServiceByID(ctx context.Context, serviceID string) (RefreshSummary, error) {
	definition, ok := s.definition(serviceID)
	if !ok {
		return RefreshSummary{}, ErrNotFound
	}
	summary, err := RefreshService(ctx, definition, domain.RawJSONTargetProfile(), s.config.Sources, s.config.Store, s.config.Clock)
	if err != nil && !errors.Is(err, ErrSourceDegraded) {
		return summary, err
	}
	return summary, nil
}
