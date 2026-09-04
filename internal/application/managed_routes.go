package application

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

// MaxManagedRouteDescriptionBytes is deliberately product-owned. Keenetic
// publishes no limit for this RCI field, so Routevane keeps comments compact
// and verifies the exact value after every owned write.
const MaxManagedRouteDescriptionBytes = 96

// ManagedRouteScope is the stable identity of one physical route surface. It
// excludes credentials and a registered-device ID so manual and scheduled
// delivery to the same endpoint share ownership.
type ManagedRouteScope struct {
	Endpoint  string
	TargetID  string
	Interface string
}

// ManagedRouteSpec is one observed or desired physical route. Labels contain
// the complete provenance set only for desired Routevane routes; Description is
// the compact device value.
type ManagedRouteSpec struct {
	Prefix      netip.Prefix
	Description string
	Labels      []string
}

// ManagedRoute records whether Routevane created the physical route. A route
// that was already present can be claimed, but never becomes removable or
// editable merely because an output later stops needing it.
type ManagedRoute struct {
	Prefix             netip.Prefix
	Description        string
	CreatedByRoutevane bool
}

// ManagedRouteClaim ties one output to one prefix and its complete provenance
// on one exact route surface. Description is persisted as the compact value so
// the ledger records what that output asked the router to show.
type ManagedRouteClaim struct {
	OutputID    string
	Prefix      netip.Prefix
	Description string
	Labels      []string
}

type ManagedRouteOwnership struct {
	Scope  ManagedRouteScope
	Routes []ManagedRoute
	Claims []ManagedRouteClaim
}

type ManagedRouteRepository interface {
	ManagedRouteOwnership(context.Context, ManagedRouteScope) (ManagedRouteOwnership, error)
	ReplaceManagedRouteOwnership(context.Context, ManagedRouteOwnership) error
	RetireManagedRouteOwnership(context.Context, ManagedRouteScope) error
}

// ManagedRouteMutation can upsert an owned route to change its description,
// while removals remain identified solely by prefix.
type ManagedRouteMutation struct {
	Upsert []ManagedRouteSpec
	Remove []netip.Prefix
}

type ManagedRouteDeployer interface {
	ManagedRouteScope(domain.TargetProfile, Connection) (ManagedRouteScope, error)
	DesiredManagedRoutes(DeployArtifact) ([]ManagedRouteSpec, error)
	CurrentManagedRoutes(context.Context, DeviceInfo, Connection) ([]ManagedRouteSpec, error)
	ApplyManagedRoutes(context.Context, DeviceInfo, Connection, ManagedRouteMutation) error
}

func validManagedRouteScope(scope ManagedRouteScope) bool {
	for _, value := range []string{scope.Endpoint, scope.TargetID, scope.Interface} {
		if value == "" || len(value) > 512 || !utf8.ValidString(value) {
			return false
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return len(scope.TargetID) <= 64 && utf8.RuneCountInString(scope.Interface) <= 120
}

func comparePrefix(a, b netip.Prefix) int { return cmp.Compare(a.String(), b.String()) }

func validManagedLabel(label string) bool {
	if !utf8.ValidString(label) || len(label) < 3 || len(label) > 1024 || label[0] != '(' || label[len(label)-1] != ')' {
		return false
	}
	inner := label[1 : len(label)-1]
	segments := strings.Split(inner, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return false
	}
	for _, segment := range segments {
		if segment != strings.Join(strings.Fields(segment), " ") || strings.ContainsAny(segment, "\\()+") {
			return false
		}
	}
	for _, r := range label {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validManagedDescription(description string) bool {
	if !utf8.ValidString(description) || len(description) > MaxManagedRouteDescriptionBytes {
		return false
	}
	for _, r := range description {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func canonicalManagedLabels(labels []string) ([]string, bool) {
	stable := domain.StableStrings(labels)
	for _, label := range stable {
		if !validManagedLabel(label) {
			return nil, false
		}
	}
	return stable, true
}

// CompactManagedRouteDescription turns a complete provenance set into one
// bounded device comment. A long first label is truncated only at a UTF-8 rune
// boundary and keeps the surrounding parentheses.
func CompactManagedRouteDescription(labels []string) string {
	labels, ok := canonicalManagedLabels(labels)
	if !ok || len(labels) == 0 {
		return ""
	}
	suffix := ""
	if len(labels) > 1 {
		suffix = " +" + strconv.Itoa(len(labels)-1)
	}
	return truncateManagedLabel(labels[0], MaxManagedRouteDescriptionBytes-len(suffix)) + suffix
}

func truncateManagedLabel(label string, maximum int) string {
	if len(label) <= maximum {
		return label
	}
	const wrapperBytes = 2
	const ellipsis = "…"
	inner := strings.TrimSuffix(strings.TrimPrefix(label, "("), ")")
	budget := maximum - wrapperBytes - len(ellipsis)
	if budget < 0 {
		return "()"
	}
	for len(inner) > budget {
		_, size := utf8.DecodeLastRuneInString(inner)
		inner = inner[:len(inner)-size]
	}
	return "(" + inner + ellipsis + ")"
}

func canonicalDesiredSpecs(specs []ManagedRouteSpec) ([]ManagedRouteSpec, bool) {
	byPrefix := make(map[netip.Prefix][]string, len(specs))
	for _, spec := range specs {
		if !spec.Prefix.IsValid() || !spec.Prefix.Addr().Is4() || spec.Prefix != spec.Prefix.Masked() {
			return nil, false
		}
		labels, ok := canonicalManagedLabels(spec.Labels)
		if !ok || !slices.Equal(labels, spec.Labels) || spec.Description != CompactManagedRouteDescription(labels) {
			return nil, false
		}
		byPrefix[spec.Prefix] = append(byPrefix[spec.Prefix], labels...)
	}
	result := make([]ManagedRouteSpec, 0, len(byPrefix))
	for prefix, labels := range byPrefix {
		labels, _ = canonicalManagedLabels(labels)
		if len(labels) == 0 {
			labels = nil
		}
		result = append(result, ManagedRouteSpec{Prefix: prefix, Description: CompactManagedRouteDescription(labels), Labels: labels})
	}
	slices.SortFunc(result, func(a, b ManagedRouteSpec) int { return comparePrefix(a.Prefix, b.Prefix) })
	return result, true
}

func currentSpecMap(specs []ManagedRouteSpec) (map[netip.Prefix]ManagedRouteSpec, bool) {
	result := make(map[netip.Prefix]ManagedRouteSpec, len(specs))
	for _, spec := range specs {
		if !spec.Prefix.IsValid() || !spec.Prefix.Addr().Is4() || spec.Prefix != spec.Prefix.Masked() || !utf8.ValidString(spec.Description) {
			return nil, false
		}
		if _, duplicate := result[spec.Prefix]; duplicate {
			return nil, false
		}
		result[spec.Prefix] = spec
	}
	return result, true
}

func validateManagedRouteOwnership(state ManagedRouteOwnership) error {
	if !validManagedRouteScope(state.Scope) {
		return fmt.Errorf("invalid managed route scope")
	}
	routes := make(map[netip.Prefix]ManagedRoute, len(state.Routes))
	for _, route := range state.Routes {
		if !route.Prefix.IsValid() || !route.Prefix.Addr().Is4() || route.Prefix != route.Prefix.Masked() || !validManagedDescription(route.Description) {
			return fmt.Errorf("invalid managed route")
		}
		if _, duplicate := routes[route.Prefix]; duplicate {
			return fmt.Errorf("duplicate managed route prefix")
		}
		routes[route.Prefix] = route
	}
	claims := make(map[string]struct{}, len(state.Claims))
	claimedRoutes := make(map[netip.Prefix]struct{}, len(state.Claims))
	labelsByPrefix := make(map[netip.Prefix][]string, len(state.Claims))
	for _, claim := range state.Claims {
		labels, ok := canonicalManagedLabels(claim.Labels)
		if !isHexID(claim.OutputID) || !claim.Prefix.IsValid() || !claim.Prefix.Addr().Is4() || claim.Prefix != claim.Prefix.Masked() || !ok || !slices.Equal(labels, claim.Labels) || claim.Description != CompactManagedRouteDescription(labels) {
			return fmt.Errorf("invalid managed route claim")
		}
		if _, present := routes[claim.Prefix]; !present {
			return fmt.Errorf("managed route claim has no route")
		}
		key := claim.OutputID + "\x00" + claim.Prefix.String()
		if _, duplicate := claims[key]; duplicate {
			return fmt.Errorf("duplicate managed route claim")
		}
		claims[key] = struct{}{}
		claimedRoutes[claim.Prefix] = struct{}{}
		labelsByPrefix[claim.Prefix] = append(labelsByPrefix[claim.Prefix], labels...)
	}
	if len(claimedRoutes) != len(routes) {
		return fmt.Errorf("managed route has no claim")
	}
	for prefix, route := range routes {
		if !route.CreatedByRoutevane && route.Description != "" {
			return fmt.Errorf("foreign managed route carries an owned description")
		}
		expected := CompactManagedRouteDescription(labelsByPrefix[prefix])
		if route.CreatedByRoutevane && expected != "" && route.Description != expected {
			return fmt.Errorf("managed route description does not match its claims")
		}
	}
	return nil
}

func (state ManagedRouteOwnership) Validate() error { return validateManagedRouteOwnership(state) }

func reconcileManagedRoutes(prior ManagedRouteOwnership, outputID string, desired, current []ManagedRouteSpec) (ManagedRouteMutation, ManagedRouteOwnership, error) {
	if !isHexID(outputID) {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid managed route output")
	}
	if err := validateManagedRouteOwnership(prior); err != nil {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, err
	}
	desired, ok := canonicalDesiredSpecs(desired)
	if !ok {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid desired managed routes")
	}
	currentSet, ok := currentSpecMap(current)
	if !ok {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid current managed routes")
	}
	priorRoutes := make(map[netip.Prefix]ManagedRoute, len(prior.Routes))
	for _, route := range prior.Routes {
		priorRoutes[route.Prefix] = route
	}

	claims := make([]ManagedRouteClaim, 0, len(prior.Claims)+len(desired))
	for _, claim := range prior.Claims {
		if claim.OutputID != outputID {
			claims = append(claims, claim)
		}
	}
	for _, spec := range desired {
		claims = append(claims, ManagedRouteClaim{OutputID: outputID, Prefix: spec.Prefix, Description: spec.Description, Labels: append([]string(nil), spec.Labels...)})
	}
	labelsByPrefix := make(map[netip.Prefix][]string)
	for _, claim := range claims {
		labelsByPrefix[claim.Prefix] = append(labelsByPrefix[claim.Prefix], claim.Labels...)
	}

	mutation := ManagedRouteMutation{}
	nextRoutes := make([]ManagedRoute, 0, len(labelsByPrefix))
	for prefix, labels := range labelsByPrefix {
		labels, _ = canonicalManagedLabels(labels)
		if len(labels) == 0 {
			labels = nil
		}
		description := CompactManagedRouteDescription(labels)
		priorRoute, known := priorRoutes[prefix]
		if description == "" && known && priorRoute.CreatedByRoutevane && priorRoute.Description != "" {
			// A v9 claim has no provenance. It must not erase the last known
			// description when it becomes the only remaining claimant.
			description = priorRoute.Description
		}
		observed, exists := currentSet[prefix]
		created := known && priorRoute.CreatedByRoutevane
		if !exists {
			created = true
			mutation.Upsert = append(mutation.Upsert, ManagedRouteSpec{Prefix: prefix, Description: description, Labels: labels})
		} else if !known {
			created = false
		} else if created && observed.Description != description {
			mutation.Upsert = append(mutation.Upsert, ManagedRouteSpec{Prefix: prefix, Description: description, Labels: labels})
		}
		storedDescription := ""
		if created {
			storedDescription = description
		}
		nextRoutes = append(nextRoutes, ManagedRoute{Prefix: prefix, Description: storedDescription, CreatedByRoutevane: created})
	}
	for prefix, route := range priorRoutes {
		if !route.CreatedByRoutevane {
			continue
		}
		if _, stillClaimed := labelsByPrefix[prefix]; stillClaimed {
			continue
		}
		if _, exists := currentSet[prefix]; exists {
			mutation.Remove = append(mutation.Remove, prefix)
		}
	}
	slices.SortFunc(mutation.Upsert, func(a, b ManagedRouteSpec) int { return comparePrefix(a.Prefix, b.Prefix) })
	slices.SortFunc(mutation.Remove, comparePrefix)
	slices.SortFunc(nextRoutes, func(a, b ManagedRoute) int { return comparePrefix(a.Prefix, b.Prefix) })
	slices.SortFunc(claims, func(a, b ManagedRouteClaim) int {
		if byOutput := strings.Compare(a.OutputID, b.OutputID); byOutput != 0 {
			return byOutput
		}
		return comparePrefix(a.Prefix, b.Prefix)
	})
	next := ManagedRouteOwnership{Scope: prior.Scope, Routes: nextRoutes, Claims: claims}
	if err := validateManagedRouteOwnership(next); err != nil {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, err
	}
	return mutation, next, nil
}

func verifyManagedRouteMutation(next ManagedRouteOwnership, mutation ManagedRouteMutation, current []ManagedRouteSpec) error {
	present, ok := currentSpecMap(current)
	if !ok {
		return fmt.Errorf("invalid current managed routes")
	}
	missing := make([]string, 0)
	mismatched := make([]string, 0)
	for _, route := range next.Routes {
		observed, exists := present[route.Prefix]
		if !exists {
			missing = append(missing, route.Prefix.String())
			continue
		}
		if route.CreatedByRoutevane && observed.Description != route.Description {
			mismatched = append(mismatched, route.Prefix.String())
		}
	}
	remaining := make([]string, 0)
	for _, prefix := range mutation.Remove {
		if _, exists := present[prefix]; exists {
			remaining = append(remaining, prefix.String())
		}
	}
	if len(missing) == 0 && len(mismatched) == 0 && len(remaining) == 0 {
		return nil
	}
	return fmt.Errorf("managed routes do not match: %d missing, %d descriptions mismatched, %d removals still present (missing=%s mismatched=%s present=%s)", len(missing), len(mismatched), len(remaining), strings.Join(missing, ","), strings.Join(mismatched, ","), strings.Join(remaining, ","))
}
