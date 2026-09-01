package application

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// Deleting from the library (ADR 0029). The operator owns what the library
// holds, including what the shipped catalog put there: a category or a list
// can be deleted whoever created it. For a catalog object the deletion is a
// removal record in the overlay — the shipped file is never edited, and a
// catalog update does not resurrect what the operator removed. For an object
// the operator created it is the deletion of that object's own rows, because
// there is nothing to subtract a record from.
//
// Deletion never rewrites a stored route. Removing a category or a list a
// route names directly is refused with those routes named. Removing a list
// from a category is not a deletion and is allowed to change what a route
// carries: that is what naming a category means (ADR 0028).

// RemovalKind separates the two things the library holds. It is stored, so
// these two words are the ones the schema checks a row against.
type RemovalKind string

const (
	RemovalCategory RemovalKind = "category"
	RemovalService  RemovalKind = "service"
)

// CatalogRemoval is one shipped object the operator deleted. It carries the
// moment because a removal is an operator action with a history, not a flag.
type CatalogRemoval struct {
	Kind      RemovalKind
	ID        string
	RemovedAt time.Time
}

// LibraryRemoval is one deletion as the store applies it: the object itself
// and, when a category takes its lists with it, those lists. It is one
// argument because it is one transaction — a category that vanished while the
// lists it held survived half-deleted is the state this must not reach.
type LibraryRemoval struct {
	Kind RemovalKind
	ID   string
	// Services are the lists deleted along with a category. It is empty for
	// every other removal, including a category whose lists are detached.
	Services  []string
	RemovedAt time.Time
}

// CategoryListDisposition answers the one question deleting a category asks:
// what becomes of the lists it held.
type CategoryListDisposition string

const (
	// CategoryListsDetach leaves them in the library, belonging to no category
	// unless another one holds them.
	CategoryListsDetach CategoryListDisposition = "detach"
	// CategoryListsDelete removes them with the category, by the same rules as
	// removing a list on its own.
	CategoryListsDelete CategoryListDisposition = "delete"
)

// ServiceInUseError refuses to delete a list a route names directly. It
// carries the routes for the same reason CategoryInUseError does: a refusal
// the operator can act on beats one they have to investigate.
type ServiceInUseError struct {
	ServiceID string
	Lists     []ListReference
}

func (e ServiceInUseError) Error() string {
	return fmt.Sprintf("list %q is named by %d route(s)", e.ServiceID, len(e.Lists))
}

// RemoveCategory deletes one category from the library, catalog-shipped or
// operator-created alike, and disposes of the lists it held as the operator
// asked. The two halves land together or not at all: a detached list belongs
// to no category the moment the category stops existing, and a deleted one
// leaves with it.
//
// The refusal is one shape for both references it checks. A route naming the
// category directly is refused because it would build something its author did
// not choose; under delete, a route naming one of the held lists directly is
// refused for exactly the same reason.
func (s *PublicationService) RemoveCategory(ctx context.Context, id string, lists CategoryListDisposition) error {
	// The disposition is part of the request's shape rather than of what the
	// store holds, so it is answered before the library is consulted at all.
	if lists != CategoryListsDetach && lists != CategoryListsDelete {
		return fmt.Errorf("invalid category list disposition")
	}
	current, ok := s.mergedCategory(id)
	if !ok {
		return ErrNotFound
	}
	var held []string
	if lists == CategoryListsDelete {
		held = current.Services
	}
	references, err := s.routesNaming(ctx, []string{id}, held)
	if err != nil {
		return err
	}
	if len(references) > 0 {
		return CategoryInUseError{CategoryID: id, Lists: references}
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	removal := LibraryRemoval{Kind: RemovalCategory, ID: id, Services: held, RemovedAt: now}
	if err := s.config.Store.RemoveFromLibrary(ctx, removal); err != nil {
		return err
	}
	s.forgetCategory(id)
	for _, serviceID := range held {
		s.forgetService(serviceID)
	}
	return nil
}

// RemoveService deletes one list from the library. A route naming it directly
// is refused, because that route would publish less than it says. A route that
// reaches the list only through a category it names is not: removing the list
// changes what that category expands to, which is what naming a category means
// (ADR 0028, ADR 0029).
//
// The route the refusal spared keeps its stored `service_domains` override for
// a list that is now gone. Nothing rewrites it: a stored route is never edited
// by a deletion, and the override is read only for services the composition
// still resolves to, so it is dead weight rather than a wrong answer.
func (s *PublicationService) RemoveService(ctx context.Context, id string) error {
	if !s.knownService(id) {
		return ErrNotFound
	}
	references, err := s.routesNaming(ctx, nil, []string{id})
	if err != nil {
		return err
	}
	if len(references) > 0 {
		return ServiceInUseError{ServiceID: id, Lists: references}
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	if err := s.config.Store.RemoveFromLibrary(ctx, LibraryRemoval{Kind: RemovalService, ID: id, RemovedAt: now}); err != nil {
		return err
	}
	s.forgetService(id)
	return nil
}

// routesNaming finds every stored route that names one of these categories or
// one of these lists directly, archived ones included: an archived route can be
// restored, and restoring one whose category or list vanished meanwhile is
// exactly the silent change the refusal exists to prevent.
func (s *PublicationService) routesNaming(ctx context.Context, categoryIDs, serviceIDs []string) ([]ListReference, error) {
	if len(categoryIDs) == 0 && len(serviceIDs) == 0 {
		return nil, nil
	}
	stored, err := s.config.Store.Lists(ctx)
	if err != nil {
		return nil, err
	}
	categories := make(map[string]struct{}, len(categoryIDs))
	for _, categoryID := range categoryIDs {
		categories[categoryID] = struct{}{}
	}
	services := make(map[string]struct{}, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		services[serviceID] = struct{}{}
	}
	references := make([]ListReference, 0)
	for _, route := range stored {
		if namesAny(route.Categories, categories) || namesAny(route.Services, services) {
			references = append(references, ListReference{ID: route.ID, Title: route.Name})
		}
	}
	slices.SortFunc(references, func(a, b ListReference) int { return cmp.Compare(a.ID, b.ID) })
	return references, nil
}

func namesAny(named []string, wanted map[string]struct{}) bool {
	for _, id := range named {
		if _, found := wanted[id]; found {
			return true
		}
	}
	return false
}

// forgetCategory brings the registry to what the store now holds: an
// operator-created category loses its row, a shipped one gains a removal, and
// either way the membership it owned is gone.
func (s *PublicationService) forgetCategory(id string) {
	s.overlay.mu.Lock()
	defer s.overlay.mu.Unlock()
	if _, operatorOwned := s.overlay.custom[id]; operatorOwned {
		delete(s.overlay.custom, id)
	} else {
		s.overlay.removed[RemovalCategory][id] = struct{}{}
	}
	delete(s.overlay.membership, id)
}

// forgetService does the same for a list, including the state that only made
// sense while the list existed: the verdicts and sources tuning it, and every
// category membership that named it.
//
// Ownership is decided by the reserved prefix, which is the rule the store
// applies. Deciding it by what the registry happens to hold would let the two
// disagree about whether a record was written.
func (s *PublicationService) forgetService(id string) {
	operatorOwned := strings.HasPrefix(id, CustomCategoryIDPrefix)
	s.custom.mu.Lock()
	delete(s.custom.services, id)
	s.custom.mu.Unlock()

	s.tuning.mu.Lock()
	delete(s.tuning.byID, id)
	s.tuning.mu.Unlock()

	s.overlay.mu.Lock()
	if !operatorOwned {
		s.overlay.removed[RemovalService][id] = struct{}{}
	}
	for categoryID, verdicts := range s.overlay.membership {
		delete(verdicts, id)
		if len(verdicts) == 0 {
			delete(s.overlay.membership, categoryID)
		}
	}
	s.overlay.mu.Unlock()
}

// removedFromLibrary reports whether the operator deleted this shipped object.
// It takes and releases the overlay lock on its own, so a caller may hold no
// other registry lock across it.
func (s *PublicationService) removedFromLibrary(kind RemovalKind, id string) bool {
	s.overlay.mu.RLock()
	defer s.overlay.mu.RUnlock()
	_, gone := s.overlay.removed[kind][id]
	return gone
}

// emptyRemovalIndex is the shape the registry holds removals in, with both
// kinds present so a read never has to check for a missing bucket.
func emptyRemovalIndex() map[RemovalKind]map[string]struct{} {
	return map[RemovalKind]map[string]struct{}{
		RemovalCategory: {},
		RemovalService:  {},
	}
}

// validStoredRemoval refuses a row the schema should already have refused. An
// identity carrying the reserved operator prefix — one string for both kinds
// the operator can create — is the one that means nothing: deleting an
// operator-created object deletes its rows, so a removal record for one would
// subtract something that is already gone and would then outlive whatever
// later took the identity.
func validStoredRemoval(removal CatalogRemoval) error {
	switch removal.Kind {
	case RemovalCategory, RemovalService:
	default:
		return fmt.Errorf("invalid stored removal kind %q", removal.Kind)
	}
	if domain.ValidateSlug(removal.ID) != nil || strings.HasPrefix(removal.ID, CustomCategoryIDPrefix) {
		return fmt.Errorf("invalid stored removal identity %q", removal.ID)
	}
	if removal.RemovedAt.IsZero() {
		return fmt.Errorf("invalid stored removal moment for %q", removal.ID)
	}
	return nil
}
