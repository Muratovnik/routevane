package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// A category is operator-owned over a catalog seed (ADR 0028). The shipped
// catalog still supplies categories with their members; what the operator
// changed lives in the store as an overlay — membership added to or removed
// from a catalog category, and whole categories they created. Every reader of
// category membership goes through the merged accessor below, so adding a
// list to a category reaches every profile that names it on the next build,
// which is the point of a category rather than a copy.
//
// A catalog category keeps its title; its existence, however, belongs to the
// operator, who owns the library and may delete what the catalog shipped. That
// deletion is a removal record in the overlay rather than an edit of the
// shipped file (ADR 0029, removals.go). Deleting a category a profile names is
// refused with the profiles named, because a profile that silently lost a category
// would build something its author did not choose.

// CustomCategoryIDPrefix is reserved for a category the operator created. The
// shipped catalog cannot legally use it, so a later catalog import can never
// collide with a stored category. The store enforces the same prefix, which is
// why the constant is exported rather than repeated there.
const CustomCategoryIDPrefix = "custom-"

const (
	// maxCategoryTitleRunes bounds an operator-supplied category title. It is
	// deliberately shorter than a list name: a category title is a column
	// heading, not a sentence.
	maxCategoryTitleRunes = 80
	// maxCustomCategories and maxCategoryMembers bound the operator-owned part
	// of the catalog the same way one composition part is bounded.
	maxCustomCategories = maxCompositionItems
	maxCategoryMembers  = maxCompositionItems
	// maxCatalogRemovals bounds the removal half of the overlay. Only a
	// catalog identity can be removed, so a healthy installation cannot exceed
	// the catalog's own size; the bound exists because a stored file could.
	maxCatalogRemovals = 1024
)

// ErrCatalogCategory refuses the one edit only an operator-created category
// accepts. A shipped category's title belongs to the catalog: renaming one
// here would make the installation disagree with the catalog it reads. Its
// existence does not — the operator owns the library and may delete what the
// catalog shipped, which the overlay records as a removal instead of editing
// the shipped file (ADR 0029).
var ErrCatalogCategory = errors.New("category is defined by the catalog")

// MembershipState is the operator's verdict on one list in one category:
// added on top of what the catalog carries, or removed from it. Absence of a
// verdict is the catalog's own answer, so the overlay stores only disagreement.
type MembershipState string

const (
	MembershipAdded   MembershipState = "added"
	MembershipRemoved MembershipState = "removed"
)

// CustomCategory is a category the operator created. It owns a title and an
// identity; its membership lives in the overlay with everything else's, so a
// catalog category and an operator category are read the same way.
type CustomCategory struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CategoryMembership is one stored verdict.
type CategoryMembership struct {
	CategoryID string
	ListID     string
	State      MembershipState
	UpdatedAt  time.Time
}

// CategoryOverlay is everything the store holds about the operator's library
// over the shipped catalog, read in one pass so the registry cannot be
// hydrated from two disagreeing moments. Removals travel with membership for
// exactly that reason: a merge that subtracted removals read a moment later
// than it read membership could promise a category a list it no longer has.
type CategoryOverlay struct {
	Categories  []CustomCategory
	Memberships []CategoryMembership
	Removals    []CatalogRemoval
}

// CategoryWrite is one category edit as the store applies it. Title is set
// only for an operator-created category, because a catalog category has no row
// to carry another one. Memberships is the complete overlay for the category
// and replaces what is stored, but only when ReplaceMemberships says the
// request restated it: a rename must not rewrite membership it never mentioned.
type CategoryWrite struct {
	CategoryID         string
	Title              string
	Memberships        []CategoryMembership
	ReplaceMemberships bool
	UpdatedAt          time.Time
}

// CategoryUpdate is a partial edit as a request states it. A nil field is one
// the request did not mention, which is not the same as an empty one: omitting
// lists leaves membership alone, while sending an empty set clears it.
// Lists, when present, is the complete desired membership — the difference
// against the catalog is computed here rather than sent by the caller, so two
// clients cannot disagree about what an overlay row means.
type CategoryUpdate struct {
	Title *string
	Lists *[]string
}

// ProfileReference names one profile that still holds a reference. It carries the
// profile's own words rather than its identity alone, so a refusal can be read
// without a second request.
type ProfileReference struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// CategoryInUseError refuses to delete a category a profile still names. It
// carries the profiles so the operator can act on the refusal instead of
// searching for its cause.
type CategoryInUseError struct {
	CategoryID string
	Profiles   []ProfileReference
}

func (e CategoryInUseError) Error() string {
	return fmt.Sprintf("category %q is named by %d profile(s)", e.CategoryID, len(e.Profiles))
}

// categoryRegistry is the in-process copy of the stored overlay. The process
// lock guarantees a single writer over the database, so a write-through
// registry cannot drift from the store while the process lives;
// LoadCategories re-reads it at startup.
type categoryRegistry struct {
	mu     sync.RWMutex
	custom map[string]CustomCategory
	// membership is keyed by category id and then by list id, which is the
	// shape every read wants and the primary key the store holds.
	membership map[string]map[string]MembershipState
	// removed is what the operator deleted from the shipped catalog, keyed by
	// kind and then by identity. An operator-created object never appears
	// here: deleting one deletes its own rows, so its absence is the deletion.
	removed map[RemovalKind]map[string]struct{}
}

// LoadCategories hydrates the registry from the store. It runs once at
// composition time; every later write goes through the store first and the
// registry second, so what the planner reads is never ahead of what a restart
// would read back.
func (s *PublicationService) LoadCategories(ctx context.Context) error {
	stored, err := s.config.Store.CategoryOverlay(ctx)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}
	if len(stored.Categories) > maxCustomCategories {
		return fmt.Errorf("load categories: stored categories exceed their bound")
	}
	custom := make(map[string]CustomCategory, len(stored.Categories))
	for _, category := range stored.Categories {
		normalized, err := normalizedCustomCategory(category)
		if err != nil {
			return fmt.Errorf("load categories: %w", err)
		}
		if _, taken := s.config.Categories[normalized.ID]; taken {
			return fmt.Errorf("load categories: stored category %q collides with the catalog", normalized.ID)
		}
		custom[normalized.ID] = normalized
	}
	membership := make(map[string]map[string]MembershipState, len(stored.Memberships))
	for _, entry := range stored.Memberships {
		if err := validStoredMembership(entry); err != nil {
			return fmt.Errorf("load categories: %w", err)
		}
		if membership[entry.CategoryID] == nil {
			membership[entry.CategoryID] = map[string]MembershipState{}
		}
		membership[entry.CategoryID][entry.ListID] = entry.State
	}
	if len(stored.Removals) > maxCatalogRemovals {
		return fmt.Errorf("load categories: stored removals exceed their bound")
	}
	removed := emptyRemovalIndex()
	for _, removal := range stored.Removals {
		if err := validStoredRemoval(removal); err != nil {
			return fmt.Errorf("load categories: %w", err)
		}
		removed[removal.Kind][removal.ID] = struct{}{}
	}
	s.overlay.mu.Lock()
	s.overlay.custom, s.overlay.membership, s.overlay.removed = custom, membership, removed
	s.overlay.mu.Unlock()
	return nil
}

// mergedCategory is the one accessor category membership is read through: the
// shipped catalog grouping with the operator's overlay applied, or a category
// the operator created outright. The picker, the forecast, composition
// validation and the planner's expansion of a profile's categories all resolve
// here, so a membership change reaches every one of them at once.
//
// A member whose list left the catalog is dropped rather than reported,
// exactly as composition resolution drops it: a list that cannot be planned
// cannot be published, and naming it here would promise otherwise. A category
// the operator removed is subtracted here, before the merge, which is what
// keeps the picker, the forecast, the planner and the library agreeing without
// a second filter at each of them (ADR 0029).
func (s *PublicationService) mergedCategory(id string) (CategoryDetail, bool) {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	base, shipped := s.config.Categories[id]
	s.overlay.mu.RLock()
	created, operatorOwned := s.overlay.custom[id]
	verdicts := maps.Clone(s.overlay.membership[id])
	_, gone := s.overlay.removed[RemovalCategory][id]
	s.overlay.mu.RUnlock()
	if gone || (!shipped && !operatorOwned) {
		return CategoryDetail{}, false
	}
	detail := CategoryDetail{ID: id, Title: base.Title, Custom: !shipped}
	if operatorOwned {
		detail.Title = created.Title
	}
	members := make([]string, 0, len(base.Lists)+len(verdicts))
	for _, listID := range base.Lists {
		if verdicts[listID] == MembershipRemoved {
			continue
		}
		members = append(members, listID)
	}
	for listID, state := range verdicts {
		if state == MembershipAdded {
			members = append(members, listID)
		}
	}
	known := make([]string, 0, len(members))
	for _, listID := range members {
		if s.knownList(listID) {
			known = append(known, listID)
		}
	}
	detail.Lists = domain.StableStrings(known)
	return detail, true
}

// mergedCategories lists every category — shipped, operator-created, or a
// shipped one the operator changed — in stable id order.
func (s *PublicationService) mergedCategories() []CategoryDetail {
	ids := make(map[string]struct{}, len(s.config.Categories))
	for id := range s.config.Categories {
		ids[id] = struct{}{}
	}
	s.overlay.mu.RLock()
	for id := range s.overlay.custom {
		ids[id] = struct{}{}
	}
	s.overlay.mu.RUnlock()
	details := make([]CategoryDetail, 0, len(ids))
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		if detail, ok := s.mergedCategory(id); ok {
			details = append(details, detail)
		}
	}
	return details
}

// knownList reports whether an id resolves to a definition at all. It is the
// existence half of the definition accessor without the tuning work, which a
// membership read never needs.
func (s *PublicationService) knownList(id string) bool {
	_, ok := s.baseDefinition(id)
	return ok
}

// CreateCategory stores one operator-created category and makes it selectable
// at once. The identity is generated, never operator-supplied: the title is
// presentation and may repeat, while the id must stay a stable slug.
func (s *PublicationService) CreateCategory(ctx context.Context, title string, lists []string) (CategoryDetail, error) {
	cleanTitle, ok := validCategoryTitle(title)
	if !ok {
		return CategoryDetail{}, fmt.Errorf("invalid category title")
	}
	members, err := s.validCategoryMembers(lists)
	if err != nil {
		return CategoryDetail{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CategoryDetail{}, fmt.Errorf("clock returned zero time")
	}
	s.overlay.mu.RLock()
	total := len(s.overlay.custom)
	s.overlay.mu.RUnlock()
	if total >= maxCustomCategories {
		return CategoryDetail{}, fmt.Errorf("custom category limit reached")
	}
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := randomHex(s.config.Entropy, 8)
		if err != nil {
			return CategoryDetail{}, fmt.Errorf("generate category identity: %w", err)
		}
		category := CustomCategory{ID: CustomCategoryIDPrefix + suffix, Title: cleanTitle, CreatedAt: now, UpdatedAt: now}
		// The shipped catalog cannot use the reserved prefix, but the check
		// costs nothing and turns a violated assumption into a retry.
		if _, taken := s.config.Categories[category.ID]; taken {
			continue
		}
		memberships := membershipRows(category.ID, members, nil, now)
		if err := s.config.Store.CreateCustomCategory(ctx, category, memberships); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return CategoryDetail{}, err
		}
		s.overlay.mu.Lock()
		s.overlay.custom[category.ID] = category
		s.overlay.membership[category.ID] = membershipIndex(memberships)
		s.overlay.mu.Unlock()
		detail, _ := s.mergedCategory(category.ID)
		return detail, nil
	}
	return CategoryDetail{}, ErrIdentityCollision
}

// UpdateCategory applies one partial edit. Membership travels as the complete
// desired set and the overlay difference against the catalog is computed here:
// a client that computed it would have to hold a copy of the catalog, and a
// stale copy would store a verdict the operator never gave.
func (s *PublicationService) UpdateCategory(ctx context.Context, id string, update CategoryUpdate) (CategoryDetail, error) {
	current, ok := s.mergedCategory(id)
	if !ok {
		return CategoryDetail{}, ErrNotFound
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CategoryDetail{}, fmt.Errorf("clock returned zero time")
	}
	write := CategoryWrite{CategoryID: id, UpdatedAt: now}
	title := current.Title
	if update.Title != nil {
		if !current.Custom {
			return CategoryDetail{}, ErrCatalogCategory
		}
		clean, valid := validCategoryTitle(*update.Title)
		if !valid {
			return CategoryDetail{}, fmt.Errorf("invalid category title")
		}
		title = clean
	}
	if current.Custom {
		write.Title = title
	}
	if update.Lists != nil {
		desired, err := s.validCategoryMembers(*update.Lists)
		if err != nil {
			return CategoryDetail{}, err
		}
		write.Memberships = membershipRows(id, desired, s.config.Categories[id].Lists, now)
		write.ReplaceMemberships = true
	}
	if err := s.config.Store.UpdateCategory(ctx, write); err != nil {
		return CategoryDetail{}, err
	}
	s.overlay.mu.Lock()
	if stored, operatorOwned := s.overlay.custom[id]; operatorOwned {
		stored.Title, stored.UpdatedAt = title, now
		s.overlay.custom[id] = stored
	}
	if write.ReplaceMemberships {
		s.overlay.membership[id] = membershipIndex(write.Memberships)
	}
	s.overlay.mu.Unlock()
	detail, _ := s.mergedCategory(id)
	return detail, nil
}

// membershipRows is the difference between what the operator wants the category
// to contain and what the catalog puts in it: a list the catalog does not
// carry becomes 'added', a catalog member the request omits becomes 'removed',
// and everything the two agree on is stored as nothing at all. A category the
// operator created has no catalog members, so it can only ever produce 'added'.
func membershipRows(categoryID string, desired, catalog []string, now time.Time) []CategoryMembership {
	carried := make(map[string]struct{}, len(catalog))
	for _, listID := range catalog {
		carried[listID] = struct{}{}
	}
	wanted := make(map[string]struct{}, len(desired))
	for _, listID := range desired {
		wanted[listID] = struct{}{}
	}
	rows := make([]CategoryMembership, 0, len(desired))
	for _, listID := range desired {
		if _, known := carried[listID]; known {
			continue
		}
		rows = append(rows, CategoryMembership{CategoryID: categoryID, ListID: listID, State: MembershipAdded, UpdatedAt: now})
	}
	for _, listID := range catalog {
		if _, kept := wanted[listID]; kept {
			continue
		}
		rows = append(rows, CategoryMembership{CategoryID: categoryID, ListID: listID, State: MembershipRemoved, UpdatedAt: now})
	}
	slices.SortFunc(rows, func(a, b CategoryMembership) int { return cmp.Compare(a.ListID, b.ListID) })
	return rows
}

func membershipIndex(rows []CategoryMembership) map[string]MembershipState {
	index := make(map[string]MembershipState, len(rows))
	for _, row := range rows {
		index[row.ListID] = row.State
	}
	return index
}

// validCategoryMembers normalizes the requested membership: deduplicated,
// stably ordered, bounded, and naming only lists that exist — shipped or
// operator-defined. A category naming a list nothing can plan would promise
// a profile content it cannot publish.
func (s *PublicationService) validCategoryMembers(lists []string) ([]string, error) {
	members := domain.StableStrings(lists)
	if len(members) > maxCategoryMembers {
		return nil, fmt.Errorf("invalid category lists")
	}
	for _, listID := range members {
		if domain.ValidateSlug(listID) != nil || !s.knownList(listID) {
			return nil, fmt.Errorf("invalid category lists")
		}
	}
	return members, nil
}

// validCategoryTitle applies the grammar every operator-supplied name passes,
// with the category's own bound.
func validCategoryTitle(title string) (string, bool) {
	return validName(title, maxCategoryTitleRunes)
}

// normalizedCustomCategory checks one stored row against the same grammar a
// request passes, so a store edited by hand cannot smuggle an unusable category
// into the registry.
func normalizedCustomCategory(category CustomCategory) (CustomCategory, error) {
	if !strings.HasPrefix(category.ID, CustomCategoryIDPrefix) || domain.ValidateSlug(category.ID) != nil {
		return CustomCategory{}, fmt.Errorf("invalid custom category identity %q", category.ID)
	}
	if category.CreatedAt.IsZero() || category.UpdatedAt.IsZero() {
		return CustomCategory{}, fmt.Errorf("invalid custom category moments for %q", category.ID)
	}
	title, ok := validCategoryTitle(category.Title)
	if !ok {
		return CustomCategory{}, fmt.Errorf("invalid stored custom category %q", category.ID)
	}
	category.Title = title
	return category, nil
}

// validStoredMembership refuses a row whose shape the schema should already
// have refused. A 'removed' verdict on an operator-created category is the one
// combination that means nothing: such a category has no catalog membership.
func validStoredMembership(entry CategoryMembership) error {
	if domain.ValidateSlug(entry.CategoryID) != nil || domain.ValidateSlug(entry.ListID) != nil {
		return fmt.Errorf("invalid stored category membership")
	}
	switch entry.State {
	case MembershipAdded:
		return nil
	case MembershipRemoved:
		if strings.HasPrefix(entry.CategoryID, CustomCategoryIDPrefix) {
			return fmt.Errorf("invalid stored category membership for %q", entry.CategoryID)
		}
		return nil
	default:
		return fmt.Errorf("invalid stored category membership state %q", entry.State)
	}
}
