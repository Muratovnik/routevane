package application

import (
	"context"
	"fmt"
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
// Deletion never rewrites a stored profile. Removing a category or a list a
// profile names directly is refused with those profiles named. Removing a list
// from a category is not a deletion and is allowed to change what a profile
// carries: that is what naming a category means (ADR 0028).

// RemovalKind separates the two things the library holds. It is stored, so
// these two words are the ones the schema checks a row against.
type RemovalKind string

const (
	RemovalCategory RemovalKind = "category"
	RemovalList     RemovalKind = "list"
	// retiredRemovalList is the word this value carried before ADR 0039. The
	// schema migration rewrote every stored row, so it survives only as a
	// value that a configuration exported by a retired version still carries
	// into an import.
	retiredRemovalList RemovalKind = "service"
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
	// Lists are the lists deleted along with a category. It is empty for
	// every other removal, including a category whose lists are detached.
	Lists     []string
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

// ListInUseError refuses to delete a list a profile names directly. It
// carries the profiles for the same reason CategoryInUseError does: a refusal
// the operator can act on beats one they have to investigate.
type ListInUseError struct {
	ListID   string
	Profiles []ProfileReference
}

func (e ListInUseError) Error() string {
	return fmt.Sprintf("list %q is named by %d profile(s)", e.ListID, len(e.Profiles))
}

// RemoveCategory deletes one category from the library, catalog-shipped or
// operator-created alike, and disposes of the lists it held as the operator
// asked. The two halves land together or not at all: a detached list belongs
// to no category the moment the category stops existing, and a deleted one
// leaves with it.
//
// The refusal is one shape for both references it checks. A profile naming the
// category directly is refused because it would build something its author did
// not choose; under delete, a profile naming one of the held lists directly is
// refused for exactly the same reason.
func (s *PublicationService) RemoveCategory(ctx context.Context, id string, profiles CategoryListDisposition) error {
	// The disposition is part of the request's shape rather than of what the
	// store holds, so it is answered before the library is consulted at all.
	if profiles != CategoryListsDetach && profiles != CategoryListsDelete {
		return fmt.Errorf("invalid category list disposition")
	}
	current, ok := s.mergedCategory(id)
	if !ok {
		return ErrNotFound
	}
	var held []string
	if profiles == CategoryListsDelete {
		held = current.Lists
	}
	references, err := s.profilesNaming(ctx, []string{id}, held)
	if err != nil {
		return err
	}
	if len(references) > 0 {
		return CategoryInUseError{CategoryID: id, Profiles: references}
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	removal := LibraryRemoval{Kind: RemovalCategory, ID: id, Lists: held, RemovedAt: now}
	if err := s.config.Store.RemoveFromLibrary(ctx, removal); err != nil {
		return err
	}
	s.forgetCategory(id)
	for _, listID := range held {
		s.forgetList(listID)
	}
	return nil
}

// RemoveList deletes one list from the library. A profile naming it directly
// is refused, because that profile would publish less than it says. A profile that
// reaches the list only through a category it names is not: removing the list
// changes what that category expands to, which is what naming a category means
// (ADR 0028, ADR 0029).
//
// The profile the refusal spared keeps its stored `service_domains` override for
// a list that is now gone. Nothing rewrites it: a stored profile is never edited
// by a deletion, and the override is read only for lists the composition
// still resolves to, so it is dead weight rather than a wrong answer.
func (s *PublicationService) RemoveList(ctx context.Context, id string) error {
	if !s.knownList(id) {
		return ErrNotFound
	}
	references, err := s.profilesNaming(ctx, nil, []string{id})
	if err != nil {
		return err
	}
	if len(references) > 0 {
		return ListInUseError{ListID: id, Profiles: references}
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	if err := s.config.Store.RemoveFromLibrary(ctx, LibraryRemoval{Kind: RemovalList, ID: id, RemovedAt: now}); err != nil {
		return err
	}
	s.forgetList(id)
	return nil
}

// profilesNaming finds every stored profile that names one of these categories or
// one of these lists directly, archived ones included: an archived profile can be
// restored, and restoring one whose category or list vanished meanwhile is
// exactly the silent change the refusal exists to prevent.
func (s *PublicationService) profilesNaming(ctx context.Context, categoryIDs, listIDs []string) ([]ProfileReference, error) {
	if len(categoryIDs) == 0 && len(listIDs) == 0 {
		return nil, nil
	}
	return s.config.Store.ProfileReferences(ctx, categoryIDs, listIDs)
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

// forgetList does the same for a list, including the state that only made
// sense while the list existed: the verdicts and sources tuning it, and every
// category membership that named it.
//
// Ownership is decided by the reserved prefix, which is the rule the store
// applies. Deciding it by what the registry happens to hold would let the two
// disagree about whether a record was written.
func (s *PublicationService) forgetList(id string) {
	operatorOwned := strings.HasPrefix(id, CustomCategoryIDPrefix)
	s.custom.mu.Lock()
	delete(s.custom.lists, id)
	s.custom.mu.Unlock()

	s.tuning.mu.Lock()
	delete(s.tuning.byID, id)
	s.tuning.mu.Unlock()

	s.overlay.mu.Lock()
	if !operatorOwned {
		s.overlay.removed[RemovalList][id] = struct{}{}
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
		RemovalList:     {},
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
	case RemovalCategory, RemovalList:
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
