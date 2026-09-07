// Package dns is the bounded stdlib DNS source behind the refresh cycle.
package dns

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	MaxUniqueAddresses   = 256
	ConservativeValidity = 2 * time.Hour
	SourceRevision       = "stdlib-dns-v1"
)

// Resolver is deliberately local to this consumer.  LookupHost returns both
// A and AAAA answers.  A resolver that also implements CNAMEResolver contributes
// one normalized query-to-terminal relation per name.
type Resolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

type CNAMEResolver interface {
	LookupCNAME(context.Context, string) (string, error)
}

type Query struct {
	ListID      string
	ComponentID string
	SourceID    string
	Names       []string
}

type Result struct {
	Sightings []domain.Sighting
	Relations []domain.Relation
}

type Observer struct {
	Resolver       Resolver
	Validity       time.Duration
	SourceRevision string
}

func NewObserver(resolver Resolver) *Observer {
	return &Observer{Resolver: resolver, Validity: ConservativeValidity, SourceRevision: SourceRevision}
}

// NetResolver adapts net.Resolver without exposing it to planner or domain.
func NetResolver() Resolver { return net.DefaultResolver }

func (o *Observer) Observe(ctx context.Context, query Query, observedAt time.Time) (Result, error) {
	if o == nil || o.Resolver == nil {
		return Result{}, fmt.Errorf("dns resolver is nil")
	}
	if ctx == nil {
		return Result{}, fmt.Errorf("dns context is nil")
	}
	if query.ListID == "" {
		return Result{}, fmt.Errorf("dns query has no service id")
	}
	if query.ComponentID == "" {
		query.ComponentID = "web"
	}
	if query.SourceID == "" {
		query.SourceID = "dns"
	}
	if observedAt.IsZero() {
		return Result{}, fmt.Errorf("dns observation time is zero")
	}
	observedAt = observedAt.UTC()
	validity := o.Validity
	if validity <= 0 {
		validity = ConservativeValidity
	}
	revision := o.SourceRevision
	if revision == "" {
		revision = SourceRevision
	}

	names := make([]string, 0, len(query.Names))
	seenNames := make(map[string]struct{}, len(query.Names))
	for _, raw := range query.Names {
		name, err := domain.NormalizeDomain(raw)
		if err != nil {
			return Result{}, fmt.Errorf("normalize DNS name %q: %w", raw, err)
		}
		if _, exists := seenNames[name]; exists {
			continue
		}
		seenNames[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return Result{}, fmt.Errorf("dns query has no names")
	}
	slices.Sort(names)

	addresses := make(map[string]netip.Addr)
	sightings := make(map[string]domain.Sighting)
	relations := make(map[string]domain.Relation)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("DNS observation canceled: %w", err)
		}
		rawAddresses, err := o.Resolver.LookupHost(ctx, name)
		if err != nil {
			return Result{}, fmt.Errorf("lookup A/AAAA %s: %w", name, err)
		}
		for _, rawAddress := range rawAddresses {
			a, err := domain.ParseAddr(rawAddress)
			if err != nil {
				return Result{}, fmt.Errorf("lookup %s returned invalid address %q: %w", name, rawAddress, err)
			}
			key := a.String()
			if _, exists := addresses[key]; !exists {
				if len(addresses) >= MaxUniqueAddresses {
					return Result{}, fmt.Errorf("DNS result exceeds %d unique addresses", MaxUniqueAddresses)
				}
				addresses[key] = a
			}
			resource, _ := domain.NewAddrResource(a)
			// net.Resolver does not expose an observed DNS TTL. The conservative
			// validity window is policy, so it must not be mislabeled as TTL data.
			sighting := domain.Sighting{ListID: query.ListID, ComponentID: query.ComponentID, Resource: resource, SourceID: query.SourceID, SourceClass: domain.SourceObserved, SourceRevision: revision, FirstSeen: observedAt, LastSeen: observedAt, ValidUntil: observedAt.Add(validity), TTLSeconds: 0, TTLKnown: false, ObservationCount: 1, Validity: domain.ValidityValid}
			key = strings.Join([]string{query.ListID, query.ComponentID, resource.CanonicalValue(), query.SourceID, revision}, "\x00")
			if _, exists := sightings[key]; !exists {
				// A source observation cycle is one fact regardless of duplicate
				// RR rows or multiple query names yielding the same resource.
				sightings[key] = sighting
			}
		}

		if cnameResolver, ok := o.Resolver.(CNAMEResolver); ok {
			if err := ctx.Err(); err != nil {
				return Result{}, fmt.Errorf("DNS observation canceled: %w", err)
			}
			terminalRaw, err := cnameResolver.LookupCNAME(ctx, name)
			if err != nil {
				// A/AAAA answers are the routing evidence. CNAME is supplemental
				// provenance enrichment, so a resolver that cannot provide it does
				// not discard otherwise valid address observations. A cancellation
				// still invalidates the whole cycle rather than publishing a partial
				// result after the caller has withdrawn it.
				if canceled := ctx.Err(); canceled != nil {
					return Result{}, fmt.Errorf("DNS observation canceled: %w", canceled)
				}
				continue
			}
			if terminalRaw == "" {
				continue
			}
			terminal, err := domain.NormalizeDomain(terminalRaw)
			if err != nil {
				return Result{}, fmt.Errorf("lookup CNAME %s returned invalid name %q: %w", name, terminalRaw, err)
			}
			if terminal != name {
				source, _ := domain.NewDomainResource(name)
				target, _ := domain.NewDomainResource(terminal)
				relation := domain.Relation{SourceResource: source, RelationType: domain.RelationCNAMETo, TargetResource: target, ListID: query.ListID, ComponentID: query.ComponentID, FirstSeen: observedAt, LastSeen: observedAt, ValidUntil: observedAt.Add(validity), SourceID: query.SourceID, SourceRevision: revision, Validity: domain.ValidityValid}
				relations[relation.Fingerprint()] = relation
			}
		}
	}

	out := Result{Sightings: make([]domain.Sighting, 0, len(sightings)), Relations: make([]domain.Relation, 0, len(relations))}
	for _, sighting := range sightings {
		sighting.ID = sighting.Fingerprint()
		out.Sightings = append(out.Sightings, sighting)
	}
	for _, relation := range relations {
		out.Relations = append(out.Relations, relation)
	}
	slices.SortFunc(out.Sightings, func(a, b domain.Sighting) int {
		ak := strings.Join([]string{a.ListID, a.ComponentID, a.Resource.CanonicalValue(), a.SourceID, a.SourceRevision}, "\x00")
		bk := strings.Join([]string{b.ListID, b.ComponentID, b.Resource.CanonicalValue(), b.SourceID, b.SourceRevision}, "\x00")
		return cmp.Compare(ak, bk)
	})
	slices.SortFunc(out.Relations, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return out, nil
}

// ObserveNames is a compact convenience for callers with a single source
// definition.  Observe remains the primitive used by tests and the command.
func (o *Observer) ObserveNames(ctx context.Context, listID, componentID, sourceID string, names []string, observedAt time.Time) (Result, error) {
	return o.Observe(ctx, Query{ListID: listID, ComponentID: componentID, SourceID: sourceID, Names: names}, observedAt)
}
