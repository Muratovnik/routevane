package application

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ManagedRouteScope is the stable identity of one physical route surface. It
// excludes credentials and a registered-device ID so manual and scheduled
// delivery to the same endpoint share ownership.
type ManagedRouteScope struct {
	Endpoint  string
	TargetID  string
	Interface string
}

// ManagedRoute records whether Routevane created the physical route. A route
// that was already present can be claimed, but never becomes removable merely
// because an output later stops needing it.
type ManagedRoute struct {
	Prefix             netip.Prefix
	CreatedByRoutevane bool
}

// ManagedRouteClaim ties one output to one prefix on one exact route surface.
type ManagedRouteClaim struct {
	OutputID string
	Prefix   netip.Prefix
}

// ManagedRouteOwnership is one complete active ledger snapshot.
type ManagedRouteOwnership struct {
	Scope  ManagedRouteScope
	Routes []ManagedRoute
	Claims []ManagedRouteClaim
}

// ManagedRouteRepository persists ownership independently from a deployer.
// Replace must be atomic: a failed write leaves the previous snapshot intact.
type ManagedRouteRepository interface {
	ManagedRouteOwnership(context.Context, ManagedRouteScope) (ManagedRouteOwnership, error)
	ReplaceManagedRouteOwnership(context.Context, ManagedRouteOwnership) error
	RetireManagedRouteOwnership(context.Context, ManagedRouteScope) error
}

// ManagedRouteMutation is the exact bounded change an ownership-aware deployer
// may make. It cannot express interface-wide ownership.
type ManagedRouteMutation struct {
	Add    []netip.Prefix
	Remove []netip.Prefix
}

// ManagedRouteDeployer is the optional exact-route capability. The application
// owns claims and deletion authority; the deployer owns artifact parsing and the
// physical route protocol. The generic Deployer boundary remains unchanged.
type ManagedRouteDeployer interface {
	ManagedRouteScope(domain.TargetProfile, Connection) (ManagedRouteScope, error)
	DesiredManagedRoutes(DeployArtifact) ([]netip.Prefix, error)
	CurrentManagedRoutes(context.Context, DeviceInfo, Connection) ([]netip.Prefix, error)
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

func canonicalManagedPrefixes(prefixes []netip.Prefix) ([]netip.Prefix, bool) {
	unique := make(map[netip.Prefix]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return nil, false
		}
		unique[prefix] = struct{}{}
	}
	result := make([]netip.Prefix, 0, len(unique))
	for prefix := range unique {
		result = append(result, prefix)
	}
	slices.SortFunc(result, comparePrefix)
	return result, true
}

func comparePrefix(a, b netip.Prefix) int { return cmp.Compare(a.String(), b.String()) }

func validateManagedRouteOwnership(state ManagedRouteOwnership) error {
	if !validManagedRouteScope(state.Scope) {
		return fmt.Errorf("invalid managed route scope")
	}
	routes := make(map[netip.Prefix]bool, len(state.Routes))
	for _, route := range state.Routes {
		if !route.Prefix.IsValid() || !route.Prefix.Addr().Is4() || route.Prefix != route.Prefix.Masked() {
			return fmt.Errorf("invalid managed route prefix")
		}
		if _, duplicate := routes[route.Prefix]; duplicate {
			return fmt.Errorf("duplicate managed route prefix")
		}
		routes[route.Prefix] = route.CreatedByRoutevane
	}
	claims := make(map[string]struct{}, len(state.Claims))
	claimedRoutes := make(map[netip.Prefix]struct{}, len(state.Claims))
	for _, claim := range state.Claims {
		if !isHexID(claim.OutputID) || !claim.Prefix.IsValid() || !claim.Prefix.Addr().Is4() || claim.Prefix != claim.Prefix.Masked() {
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
	}
	if len(claimedRoutes) != len(routes) {
		return fmt.Errorf("managed route has no claim")
	}
	return nil
}

// Validate checks persisted ownership before it can authorize a deletion.
func (state ManagedRouteOwnership) Validate() error { return validateManagedRouteOwnership(state) }

func reconcileManagedRoutes(prior ManagedRouteOwnership, outputID string, desired, current []netip.Prefix) (ManagedRouteMutation, ManagedRouteOwnership, error) {
	if !isHexID(outputID) {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid managed route output")
	}
	if err := validateManagedRouteOwnership(prior); err != nil {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, err
	}
	desired, ok := canonicalManagedPrefixes(desired)
	if !ok {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid desired managed routes")
	}
	current, ok = canonicalManagedPrefixes(current)
	if !ok {
		return ManagedRouteMutation{}, ManagedRouteOwnership{}, fmt.Errorf("invalid current managed routes")
	}

	currentSet := make(map[netip.Prefix]struct{}, len(current))
	for _, prefix := range current {
		currentSet[prefix] = struct{}{}
	}
	priorRoutes := make(map[netip.Prefix]bool, len(prior.Routes))
	for _, route := range prior.Routes {
		priorRoutes[route.Prefix] = route.CreatedByRoutevane
	}

	claims := make([]ManagedRouteClaim, 0, len(prior.Claims)+len(desired))
	claimed := make(map[netip.Prefix]struct{}, len(prior.Claims)+len(desired))
	for _, claim := range prior.Claims {
		if claim.OutputID == outputID {
			continue
		}
		claims = append(claims, claim)
		claimed[claim.Prefix] = struct{}{}
	}
	for _, prefix := range desired {
		claims = append(claims, ManagedRouteClaim{OutputID: outputID, Prefix: prefix})
		claimed[prefix] = struct{}{}
	}

	mutation := ManagedRouteMutation{}
	nextRoutes := make([]ManagedRoute, 0, len(claimed))
	for prefix := range claimed {
		_, exists := currentSet[prefix]
		created := priorRoutes[prefix]
		if !exists {
			mutation.Add = append(mutation.Add, prefix)
			created = true
		} else if _, known := priorRoutes[prefix]; !known {
			created = false
		}
		nextRoutes = append(nextRoutes, ManagedRoute{Prefix: prefix, CreatedByRoutevane: created})
	}
	for prefix, created := range priorRoutes {
		if !created {
			continue
		}
		if _, stillClaimed := claimed[prefix]; stillClaimed {
			continue
		}
		if _, exists := currentSet[prefix]; exists {
			mutation.Remove = append(mutation.Remove, prefix)
		}
	}
	slices.SortFunc(mutation.Add, comparePrefix)
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

func verifyManagedRouteMutation(next ManagedRouteOwnership, mutation ManagedRouteMutation, current []netip.Prefix) error {
	current, ok := canonicalManagedPrefixes(current)
	if !ok {
		return fmt.Errorf("invalid current managed routes")
	}
	present := make(map[netip.Prefix]struct{}, len(current))
	for _, prefix := range current {
		present[prefix] = struct{}{}
	}
	missing := make([]string, 0)
	for _, route := range next.Routes {
		if _, ok := present[route.Prefix]; !ok {
			missing = append(missing, route.Prefix.String())
		}
	}
	remaining := make([]string, 0)
	for _, prefix := range mutation.Remove {
		if _, ok := present[prefix]; ok {
			remaining = append(remaining, prefix.String())
		}
	}
	if len(missing) == 0 && len(remaining) == 0 {
		return nil
	}
	return fmt.Errorf("managed routes do not match: %d missing, %d removals still present (missing=%s present=%s)", len(missing), len(remaining), strings.Join(missing, ","), strings.Join(remaining, ","))
}
