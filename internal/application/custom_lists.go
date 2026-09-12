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

// A custom list is an operator-defined catalog entry: a name and the domain
// suffixes it stands for. It has no automatic sources — what the operator wrote
// is the whole definition — and it is global: any list may name it, exactly
// like a shipped catalog list. Its identity carries a reserved prefix so it
// can never collide with a list the shipped catalog gains later.
const customListIDPrefix = "custom-"

// maxCustomLists bounds the operator-defined part of the catalog the same
// way one composition part is bounded. The shipped catalog is bounded by its
// directory; this registry has no directory, so the bound lives here.
const maxCustomLists = maxCompositionItems

type CustomList struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Domains   []string  `json:"domains"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// customListRegistry is the in-process copy of the stored custom lists.
// The process lock guarantees a single writer over the database, so a
// write-through registry cannot drift from the store while the process lives;
// LoadCustomLists re-reads it at startup.
type customListRegistry struct {
	mu    sync.RWMutex
	lists map[string]CustomList
}

// LoadCustomLists hydrates the registry from the store. It runs once at
// composition time; every later write goes through the store first and the
// registry second, so what the planner reads is never ahead of what a restart
// would read back.
func (s *PublicationService) LoadCustomLists(ctx context.Context) error {
	stored, err := s.config.Store.CustomLists(ctx)
	if err != nil {
		return fmt.Errorf("load custom lists: %w", err)
	}
	lists := make(map[string]CustomList, len(stored))
	for _, list := range stored {
		normalized, err := normalizedCustomList(list)
		if err != nil {
			return fmt.Errorf("load custom lists: %w", err)
		}
		lists[normalized.ID] = normalized
	}
	s.custom.mu.Lock()
	s.custom.lists = lists
	s.custom.mu.Unlock()
	return nil
}

// CreateCustomList stores one operator-defined list and makes it
// selectable at once. The identity is generated, never operator-supplied: the
// title is presentation and may repeat, while the id must stay a stable slug.
func (s *PublicationService) CreateCustomList(ctx context.Context, title string, domains []string) (CustomList, error) {
	cleanTitle, cleanDomains, err := validCustomListInput(title, domains)
	if err != nil {
		return CustomList{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CustomList{}, fmt.Errorf("clock returned zero time")
	}
	s.custom.mu.RLock()
	total := len(s.custom.lists)
	s.custom.mu.RUnlock()
	if total >= maxCustomLists {
		return CustomList{}, fmt.Errorf("custom list limit reached")
	}
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := randomHex(s.config.Entropy, 8)
		if err != nil {
			return CustomList{}, fmt.Errorf("generate custom list identity: %w", err)
		}
		list := CustomList{ID: customListIDPrefix + suffix, Title: cleanTitle, Domains: cleanDomains, CreatedAt: now, UpdatedAt: now}
		// The shipped catalog cannot use the reserved prefix, but the check
		// costs nothing and turns a violated assumption into a retry.
		if _, taken := s.config.Definitions[list.ID]; taken {
			continue
		}
		s.publicationMu.Lock()
		if err := s.config.Store.CreateCustomList(ctx, list); err != nil {
			s.publicationMu.Unlock()
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return CustomList{}, err
		}
		s.custom.mu.Lock()
		s.custom.lists[list.ID] = list
		s.custom.mu.Unlock()
		s.publicationGeneration++
		s.publicationMu.Unlock()
		return list, nil
	}
	return CustomList{}, ErrIdentityCollision
}

// UpdateCustomList replaces the mutable part of one custom list: its
// title and its domains. Identity and creation time are immutable, and the
// change reaches a published file only through the next refresh-and-rebuild,
// exactly as a shipped catalog edit would.
func (s *PublicationService) UpdateCustomList(ctx context.Context, id, title string, domains []string) (CustomList, error) {
	cleanTitle, cleanDomains, err := validCustomListInput(title, domains)
	if err != nil {
		return CustomList{}, err
	}
	s.custom.mu.RLock()
	current, ok := s.custom.lists[id]
	s.custom.mu.RUnlock()
	if !ok {
		return CustomList{}, ErrNotFound
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CustomList{}, fmt.Errorf("clock returned zero time")
	}
	updated := CustomList{ID: current.ID, Title: cleanTitle, Domains: cleanDomains, CreatedAt: current.CreatedAt, UpdatedAt: now}
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	if err := s.config.Store.UpdateCustomList(ctx, updated); err != nil {
		return CustomList{}, err
	}
	s.custom.mu.Lock()
	s.custom.lists[updated.ID] = updated
	s.custom.mu.Unlock()
	s.publicationGeneration++
	return updated, nil
}

// customLists lists the registry in stable id order.
func (s *PublicationService) customLists() []CustomList {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	s.custom.mu.RLock()
	lists := make([]CustomList, 0, len(s.custom.lists))
	for _, list := range s.custom.lists {
		lists = append(lists, list)
	}
	s.custom.mu.RUnlock()
	slices.SortFunc(lists, func(a, b CustomList) int { return cmp.Compare(a.ID, b.ID) })
	return lists
}

// definition resolves one list id against the shipped catalog first and the
// custom registry second, then applies the operator's tuning. Every
// composition, refresh, preview, and build lookup goes through here, so an
// operator-defined list and an operator correction behave the same
// everywhere or nowhere.
func (s *PublicationService) definition(id string) (domain.ListDefinition, bool) {
	base, ok := s.baseDefinition(id)
	if !ok {
		return domain.ListDefinition{}, false
	}
	return s.tunedDefinition(base), true
}

func (s *PublicationService) hasDefinition(id string) bool {
	_, ok := s.definition(id)
	return ok
}

// customListDefinition projects a stored custom list into the planner's
// shape: one required component whose seeds are the operator's domain
// suffixes. It carries the shipped catalog's revision, because the planner
// accepts exactly one revision per plan and a custom list composes with the
// catalog it extends. Freshness does not depend on the revision: the domains
// enter every plan straight from this definition, and a change reaches the
// artifact through the plan's semantic hash.
func customListDefinition(list CustomList, catalogRevision string) domain.ListDefinition {
	seeds := make([]domain.Seed, 0, len(list.Domains))
	for _, value := range list.Domains {
		seeds = append(seeds, domain.Seed{
			Kind: domain.RuleDomainSuffix, Value: value, ComponentID: "web",
			SourceID: "manual:custom:" + list.ID, SourceClass: domain.SourceManual,
		})
	}
	return domain.ListDefinition{
		ID: list.ID, Title: list.Title,
		Components:      []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:           seeds,
		CatalogRevision: catalogRevision,
	}
}

// validCustomListInput normalizes the operator's title and domains with the
// same rules the rest of the product applies: the list-name grammar for the
// title, and the bounded list-local domain grammar for the domain set. At
// least one domain is required — a list that matches nothing would publish
// a promise with no rule behind it.
func validCustomListInput(title string, domains []string) (string, []string, error) {
	cleanTitle, ok := validObjectName(title)
	if !ok {
		return "", nil, fmt.Errorf("invalid custom list title")
	}
	if len(domains) == 0 || len(domains) > maxDomainsPerList {
		return "", nil, fmt.Errorf("invalid custom list domains")
	}
	normalized := make([]string, 0, len(domains))
	for _, value := range domains {
		cleanDomain, err := domain.NormalizeDomain(value)
		if err != nil {
			return "", nil, fmt.Errorf("invalid custom list domains")
		}
		normalized = append(normalized, cleanDomain)
	}
	return cleanTitle, domain.StableStrings(normalized), nil
}

// normalizedCustomList checks one stored row against the same grammar a
// request passes, so a store edited by hand cannot smuggle an unplannable
// definition into the registry.
func normalizedCustomList(list CustomList) (CustomList, error) {
	if !strings.HasPrefix(list.ID, customListIDPrefix) || domain.ValidateSlug(list.ID) != nil {
		return CustomList{}, fmt.Errorf("invalid custom list identity %q", list.ID)
	}
	if list.CreatedAt.IsZero() || list.UpdatedAt.IsZero() {
		return CustomList{}, fmt.Errorf("invalid custom list moments for %q", list.ID)
	}
	title, domains, err := validCustomListInput(list.Title, list.Domains)
	if err != nil {
		return CustomList{}, fmt.Errorf("invalid stored custom list %q", list.ID)
	}
	list.Title, list.Domains = title, domains
	return list, nil
}
