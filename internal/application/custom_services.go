package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// A custom service is an operator-defined catalog entry: a name and the domain
// suffixes it stands for. It has no automatic sources — what the operator wrote
// is the whole definition — and it is global: any list may name it, exactly
// like a shipped catalog service. Its identity carries a reserved prefix so it
// can never collide with a service the shipped catalog gains later.
const customServiceIDPrefix = "custom-"

// maxCustomServices bounds the operator-defined part of the catalog the same
// way one composition part is bounded. The shipped catalog is bounded by its
// directory; this registry has no directory, so the bound lives here.
const maxCustomServices = maxCompositionItems

type CustomService struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Domains   []string  `json:"domains"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// customServiceRegistry is the in-process copy of the stored custom services.
// The process lock guarantees a single writer over the database, so a
// write-through registry cannot drift from the store while the process lives;
// LoadCustomServices re-reads it at startup.
type customServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]CustomService
}

// LoadCustomServices hydrates the registry from the store. It runs once at
// composition time; every later write goes through the store first and the
// registry second, so what the planner reads is never ahead of what a restart
// would read back.
func (s *PublicationService) LoadCustomServices(ctx context.Context) error {
	stored, err := s.config.Store.CustomServices(ctx)
	if err != nil {
		return fmt.Errorf("load custom services: %w", err)
	}
	services := make(map[string]CustomService, len(stored))
	for _, service := range stored {
		normalized, err := normalizedCustomService(service)
		if err != nil {
			return fmt.Errorf("load custom services: %w", err)
		}
		services[normalized.ID] = normalized
	}
	s.custom.mu.Lock()
	s.custom.services = services
	s.custom.mu.Unlock()
	return nil
}

// CreateCustomService stores one operator-defined service and makes it
// selectable at once. The identity is generated, never operator-supplied: the
// title is presentation and may repeat, while the id must stay a stable slug.
func (s *PublicationService) CreateCustomService(ctx context.Context, title string, domains []string) (CustomService, error) {
	cleanTitle, cleanDomains, err := validCustomServiceInput(title, domains)
	if err != nil {
		return CustomService{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CustomService{}, fmt.Errorf("clock returned zero time")
	}
	s.custom.mu.RLock()
	total := len(s.custom.services)
	s.custom.mu.RUnlock()
	if total >= maxCustomServices {
		return CustomService{}, fmt.Errorf("custom service limit reached")
	}
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := randomHex(s.config.Entropy, 8)
		if err != nil {
			return CustomService{}, fmt.Errorf("generate custom service identity: %w", err)
		}
		service := CustomService{ID: customServiceIDPrefix + suffix, Title: cleanTitle, Domains: cleanDomains, CreatedAt: now, UpdatedAt: now}
		// The shipped catalog cannot use the reserved prefix, but the check
		// costs nothing and turns a violated assumption into a retry.
		if _, taken := s.config.Definitions[service.ID]; taken {
			continue
		}
		if err := s.config.Store.CreateCustomService(ctx, service); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return CustomService{}, err
		}
		s.custom.mu.Lock()
		s.custom.services[service.ID] = service
		s.custom.mu.Unlock()
		return service, nil
	}
	return CustomService{}, ErrIdentityCollision
}

// UpdateCustomService replaces the mutable part of one custom service: its
// title and its domains. Identity and creation time are immutable, and the
// change reaches a published file only through the next refresh-and-rebuild,
// exactly as a shipped catalog edit would.
func (s *PublicationService) UpdateCustomService(ctx context.Context, id, title string, domains []string) (CustomService, error) {
	cleanTitle, cleanDomains, err := validCustomServiceInput(title, domains)
	if err != nil {
		return CustomService{}, err
	}
	s.custom.mu.RLock()
	current, ok := s.custom.services[id]
	s.custom.mu.RUnlock()
	if !ok {
		return CustomService{}, ErrNotFound
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CustomService{}, fmt.Errorf("clock returned zero time")
	}
	updated := CustomService{ID: current.ID, Title: cleanTitle, Domains: cleanDomains, CreatedAt: current.CreatedAt, UpdatedAt: now}
	if err := s.config.Store.UpdateCustomService(ctx, updated); err != nil {
		return CustomService{}, err
	}
	s.custom.mu.Lock()
	s.custom.services[updated.ID] = updated
	s.custom.mu.Unlock()
	return updated, nil
}

// customServices lists the registry in stable id order.
func (s *PublicationService) customServices() []CustomService {
	s.custom.mu.RLock()
	services := make([]CustomService, 0, len(s.custom.services))
	for _, service := range s.custom.services {
		services = append(services, service)
	}
	s.custom.mu.RUnlock()
	slices.SortFunc(services, func(a, b CustomService) int { return cmp.Compare(a.ID, b.ID) })
	return services
}

// definition resolves one service id against the shipped catalog first and the
// custom registry second, then applies the operator's tuning. Every
// composition, refresh, preview, and build lookup goes through here, so an
// operator-defined service and an operator correction behave the same
// everywhere or nowhere.
func (s *PublicationService) definition(id string) (domain.ServiceDefinition, bool) {
	base, ok := s.baseDefinition(id)
	if !ok {
		return domain.ServiceDefinition{}, false
	}
	return s.tunedDefinition(base), true
}

func (s *PublicationService) hasDefinition(id string) bool {
	_, ok := s.definition(id)
	return ok
}

// customServiceDefinition projects a stored custom service into the planner's
// shape: one required component whose seeds are the operator's domain
// suffixes. It carries the shipped catalog's revision, because the planner
// accepts exactly one revision per plan and a custom service composes with the
// catalog it extends. Freshness does not depend on the revision: the domains
// enter every plan straight from this definition, and a change reaches the
// artifact through the plan's semantic hash.
func customServiceDefinition(service CustomService, catalogRevision string) domain.ServiceDefinition {
	seeds := make([]domain.Seed, 0, len(service.Domains))
	for _, value := range service.Domains {
		seeds = append(seeds, domain.Seed{
			Kind: domain.RuleDomainSuffix, Value: value, ComponentID: "web",
			SourceID: "manual:custom:" + service.ID, SourceClass: domain.SourceManual,
		})
	}
	return domain.ServiceDefinition{
		ID: service.ID, Title: service.Title,
		Components:      []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:           seeds,
		CatalogRevision: catalogRevision,
	}
}

// validCustomServiceInput normalizes the operator's title and domains with the
// same rules the rest of the product applies: the list-name grammar for the
// title, and the bounded list-local domain grammar for the domain set. At
// least one domain is required — a service that matches nothing would publish
// a promise with no rule behind it.
func validCustomServiceInput(title string, domains []string) (string, []string, error) {
	cleanTitle, ok := validListName(title)
	if !ok {
		return "", nil, fmt.Errorf("invalid custom service title")
	}
	if len(domains) == 0 || len(domains) > maxDomainsPerService {
		return "", nil, fmt.Errorf("invalid custom service domains")
	}
	normalized := make([]string, 0, len(domains))
	for _, value := range domains {
		cleanDomain, err := domain.NormalizeDomain(value)
		if err != nil {
			return "", nil, fmt.Errorf("invalid custom service domains")
		}
		normalized = append(normalized, cleanDomain)
	}
	return cleanTitle, domain.StableStrings(normalized), nil
}

// normalizedCustomService checks one stored row against the same grammar a
// request passes, so a store edited by hand cannot smuggle an unplannable
// definition into the registry.
func normalizedCustomService(service CustomService) (CustomService, error) {
	if !strings.HasPrefix(service.ID, customServiceIDPrefix) || domain.ValidateSlug(service.ID) != nil {
		return CustomService{}, fmt.Errorf("invalid custom service identity %q", service.ID)
	}
	if service.CreatedAt.IsZero() || service.UpdatedAt.IsZero() {
		return CustomService{}, fmt.Errorf("invalid custom service moments for %q", service.ID)
	}
	title, domains, err := validCustomServiceInput(service.Title, service.Domains)
	if err != nil {
		return CustomService{}, fmt.Errorf("invalid stored custom service %q", service.ID)
	}
	service.Title, service.Domains = title, domains
	return service, nil
}
