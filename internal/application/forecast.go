package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

// CompositionForecast is what one target would receive from a composition that
// does not exist yet: the rule budget the device declares, the count the
// renderer would project into the file, and whether the two fit. The product
// knows both numbers before anything is created, so it states them instead of
// letting the first build discover the refusal.
type CompositionForecast struct {
	TargetID string `json:"target_id"`
	// MaximumRules is the target's own bound. Zero means the target declares
	// none, and the pair then always fits.
	MaximumRules   int  `json:"maximum_rules"`
	ProjectedRules int  `json:"projected_rules"`
	Fits           bool `json:"fits"`
	// PerService attributes the planned rules to the services that produced
	// them, so an overflowing composition is reduced by name rather than by
	// guesswork. Its sum may be larger than ProjectedRules: a renderer collapses
	// and deduplicates format-identical rules, and that saving belongs to the
	// file rather than to any one service.
	PerService []ServiceRuleForecast `json:"per_service"`
	Overlaps   CompositionOverlaps   `json:"overlaps"`
	// Missing services are excluded from this partial calculation, never counted
	// as zero. Fits is false until the entire composition can be measured.
	IncompleteServices []string `json:"incomplete_services,omitempty"`
}

type OverlapValue struct {
	RuleKind domain.RuleKind `json:"rule_kind"`
	Value    string          `json:"value"`
	Services []string        `json:"services"`
}

type CompositionOverlap struct {
	Kind     string        `json:"kind"`
	Entry    OverlapValue  `json:"entry"`
	Covering *OverlapValue `json:"covering,omitempty"`
}

// CompositionOverlaps is a bounded explanation, not a count of saved rules.
// Truncation applies to details; only ProjectedRules describes the whole file.
type CompositionOverlaps struct {
	Items     []CompositionOverlap `json:"items"`
	Truncated bool                 `json:"truncated"`
	// Summary is the complete undirected service adjacency graph. Unlike Items,
	// it is not capped: a forecast with many diagnostic relations still names
	// every service pair that overlaps.
	Summary []CompositionOverlapSummary `json:"summary,omitempty"`
}

// CompositionOverlapSummary is one stable row of the overlap adjacency graph.
// Overlaps never contains ServiceID itself.
type CompositionOverlapSummary struct {
	ServiceID string   `json:"service_id"`
	Overlaps  []string `json:"overlaps"`
}

func forecastOverlaps(plan domain.RoutingPlan) CompositionOverlaps {
	const detailLimit = 100
	analysis := planner.AnalyzeRuleOverlaps(plan.Rules, detailLimit)
	result := CompositionOverlaps{Items: make([]CompositionOverlap, 0, len(analysis.Items)), Truncated: analysis.Truncated, Summary: make([]CompositionOverlapSummary, 0, len(plan.Services))}
	value := func(entry planner.OverlapValue) OverlapValue {
		return OverlapValue{RuleKind: entry.RuleKind, Value: entry.Value, Services: entry.Services}
	}
	for _, item := range analysis.Items {
		mapped := CompositionOverlap{Kind: item.Kind, Entry: value(item.Entry)}
		if item.Covering != nil {
			covering := value(*item.Covering)
			mapped.Covering = &covering
		}
		result.Items = append(result.Items, mapped)
	}
	// The planner summary contains only services that participated in a
	// relation. Forecasts must state one row for every selected service,
	// including a service that contributed zero rules, so construct rows in the
	// plan's canonical service order and fill absent adjacency with an empty set.
	byService := make(map[string][]string, len(analysis.Summary))
	for _, row := range analysis.Summary {
		byService[row.ServiceID] = append([]string(nil), row.Overlaps...)
	}
	for _, serviceID := range plan.Services {
		overlaps := byService[serviceID]
		if overlaps == nil {
			overlaps = []string{}
		}
		result.Summary = append(result.Summary, CompositionOverlapSummary{ServiceID: serviceID, Overlaps: overlaps})
	}
	return result
}

// ServiceRuleForecast is one service's share of a forecast plan.
type ServiceRuleForecast struct {
	ServiceID string `json:"service_id"`
	Rules     int    `json:"rules"`
}

// ForecastComposition answers what a composition would cost on each requested
// target, before a list, an output, an attempt, or an artifact exists. It is a
// read: the composition travels as an argument, the list it builds is a value
// that never reaches the store, and nothing on this path writes.
//
// An empty target set means every selectable target, which is what a screen
// offering the choice needs; a named set is answered in sorted order, so the
// same request always produces the same document.
func (s *PublicationService) ForecastComposition(ctx context.Context, requested ListComposition, targetIDs []string) ([]CompositionForecast, error) {
	// The composition passes exactly the checks list creation applies, so a
	// forecast can never describe a composition the product would then refuse to
	// store, and a composition resolving to nothing is refused here by name.
	composition, err := s.validCompositionWithDefaultPriority(ctx, requested)
	if err != nil {
		return nil, err
	}
	targets, err := s.forecastTargets(targetIDs)
	if err != nil {
		return nil, err
	}
	// An ephemeral list carries no identity because there is no list: it is the
	// argument shape prepareList already accepts, not a row waiting to be
	// written.
	list := List{
		Services: composition.Services, Categories: composition.Categories,
		Exclusions: composition.Exclusions, ServiceDomains: composition.ServiceDomains,
		Priority: composition.Priority,
	}
	cutoff := s.config.Clock.Now().UTC()
	forecasts := make([]CompositionForecast, 0, len(targets))
	for _, targetID := range targets {
		forecast, err := s.forecastTarget(ctx, list, targetID, cutoff)
		if err != nil {
			return nil, err
		}
		forecasts = append(forecasts, forecast)
	}
	return forecasts, nil
}

// forecastTargets normalizes the requested target set against the catalog. An
// unknown id is refused rather than dropped: a caller that asked about a device
// and received nothing about it would read the silence as a fit.
func (s *PublicationService) forecastTargets(requested []string) ([]string, error) {
	if len(requested) == 0 {
		ids := make([]string, 0, len(s.config.Targets))
		for id := range s.config.Targets {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return ids, nil
	}
	if len(requested) > maxCompositionItems {
		return nil, fmt.Errorf("invalid forecast targets")
	}
	ids := domain.StableStrings(requested)
	for _, id := range ids {
		if _, known := s.config.Targets[id]; !known {
			return nil, fmt.Errorf("invalid forecast target")
		}
	}
	return ids, nil
}

// forecastTarget plans the composition for one target with that target's rule
// bound lifted, then measures the finished plan against the real bound.
//
// The bound is not an input to planning. PrepareServices already plans against
// a copy with MaxRules cleared, and the limit exists only as the last gate in
// PreflightPlan, which computes the projection and then refuses. So lifting it
// changes nothing about which rules the plan holds and only decides whether
// that refusal fires — and predicting that refusal is the whole point, which it
// cannot do from behind it.
func (s *PublicationService) forecastTarget(ctx context.Context, list List, targetID string, cutoff time.Time) (CompositionForecast, error) {
	target, renderer, err := s.target(targetID)
	if err != nil {
		return CompositionForecast{}, fmt.Errorf("invalid forecast target")
	}
	unbounded := target
	unbounded.Constraints.MaxRules = 0
	prepared, _, err := s.prepareListAt(ctx, list, unbounded, renderer, cutoff)
	var incomplete []string
	if unavailableForecastCoverage(err) {
		// Publication is still all-or-nothing. Inspection can retain the complete
		// services, using the same strict preparation and the same observation cut.
		var available []string
		for _, id := range s.ResolvedServices(list) {
			one := List{Services: []string{id}, ServiceDomains: list.ServiceDomains}
			_, _, serviceErr := s.prepareListAt(ctx, one, unbounded, renderer, cutoff)
			if unavailableForecastCoverage(serviceErr) {
				incomplete = append(incomplete, id)
			} else if serviceErr != nil {
				return CompositionForecast{}, serviceErr
			} else {
				available = append(available, id)
			}
		}
		if len(available) == 0 {
			return CompositionForecast{TargetID: target.ID, MaximumRules: target.Constraints.MaxRules,
				IncompleteServices: incomplete, PerService: []ServiceRuleForecast{},
				Overlaps: CompositionOverlaps{Items: []CompositionOverlap{}}}, nil
		}
		subset := List{Services: available, Priority: available, ServiceDomains: list.ServiceDomains}
		prepared, _, err = s.prepareListAt(ctx, subset, unbounded, renderer, cutoff)
	}
	if err != nil {
		return CompositionForecast{}, err
	}
	projected, err := renderer.ProjectedRuleCount(cloneRoutingPlan(prepared.Plan))
	if err != nil || projected < 0 {
		return CompositionForecast{}, fmt.Errorf("%w: invalid renderer projection", ErrPreflight)
	}
	// A zero contribution is useful inside a larger plan. An entirely empty
	// artifact is refused by every file renderer and must not be offered as a fit.
	if projected == 0 {
		return CompositionForecast{TargetID: target.ID, MaximumRules: target.Constraints.MaxRules,
			IncompleteServices: s.ResolvedServices(list), PerService: []ServiceRuleForecast{},
			Overlaps: CompositionOverlaps{Items: []CompositionOverlap{}}}, nil
	}
	maximum := target.Constraints.MaxRules
	return CompositionForecast{
		TargetID: target.ID, MaximumRules: maximum, ProjectedRules: projected,
		Fits:               len(incomplete) == 0 && (maximum == 0 || projected <= maximum),
		IncompleteServices: incomplete,
		PerService:         rulesPerService(prepared.Plan),
		Overlaps:           prepared.compositionOverlaps,
	}, nil
}

// rulesPerService groups a plan's rules by the service each rule is attributed
// to. Every planned service appears, including one that contributed nothing: a
// zero is a fact an operator deciding what to drop needs, and an absent row
// would read as an unanswered question rather than as an empty share.
func rulesPerService(plan domain.RoutingPlan) []ServiceRuleForecast {
	counts := make(map[string]int, len(plan.Services))
	for _, rule := range plan.Rules {
		counts[rule.ServiceID]++
	}
	// plan.Services is canonical — sorted, unique, and a superset of every
	// rule's attribution, both checked by preflight — so it supplies the order
	// and the completeness rule without a second sort.
	perService := make([]ServiceRuleForecast, 0, len(plan.Services))
	for _, serviceID := range plan.Services {
		perService = append(perService, ServiceRuleForecast{ServiceID: serviceID, Rules: counts[serviceID]})
	}
	return perService
}

func unavailableForecastCoverage(err error) bool {
	return errors.Is(err, ErrPartialCoverage) || errors.Is(err, ErrNotFound)
}
