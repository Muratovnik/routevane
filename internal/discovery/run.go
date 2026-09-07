package discovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ErrConfirmationRequired reports that the caller has not yet confirmed the
// exact URL a browser would load. A discovery session never starts from an
// unconfirmed value: the user must see the canonical URL first.
var ErrConfirmationRequired = errors.New("discovery target requires explicit confirmation")

// ErrInvalidComposition reports a missing seam.
var ErrInvalidComposition = errors.New("invalid discovery composition")

// Observation is what the DNS cycle contributed. Only counts and relations
// cross this boundary: the addresses themselves stay in the observation store.
type Observation struct {
	Sightings int
	Relations []domain.Relation
}

// Deps are the seams one discovery run needs. Each is owned by its own layer:
// the browser is infrastructure, the DNS cycle is a source, and the writer is
// the catalog boundary.
type Deps struct {
	Browser BrowserOptions
	// Load performs the managed page load. Nil uses LoadPage.
	Load func(context.Context, Target, BrowserOptions) (PageLoad, error)
	// Observe runs the DNS cycle for the accepted hosts and persists it. It is
	// the only path by which an address is stored.
	Observe func(context.Context, domain.ListDefinition) (Observation, error)
	// Write persists the reviewed draft into the local catalog.
	Write func(context.Context, domain.ListDefinition) (string, error)
	Now   func() time.Time
}

// Request is one discovery invocation.
type Request struct {
	// URL is what the user typed. It may be a bare host.
	URL string
	// ListID overrides the identity derived from the registrable domain.
	ListID string
	Title  string
	// ManualSeeds are extra domains the user explicitly wants included.
	ManualSeeds []string
	// Confirm must be true for a browser to start. A run without it returns the
	// normalized target so the caller can show it and ask.
	Confirm bool
}

// Result is the outcome of one discovery run. A run without confirmation
// returns only Target and Confirmed=false.
type Result struct {
	Target    Target
	ListID    string
	Confirmed bool
	Page      PageLoad
	Draft     Draft
	Path      string
	Observed  Observation
	// ObservationError reports that the draft was written but its first DNS
	// cycle did not complete. The list exists; refresh owns retrying.
	ObservationError string
}

// Run normalizes the target, optionally performs one managed page load, derives
// a safe draft, persists observations, and writes the draft.
//
// The unconfirmed call is not a preview of a different computation: it returns
// exactly the URL the confirmed call will load.
func Run(ctx context.Context, request Request, deps Deps) (Result, error) {
	if ctx == nil {
		return Result{}, ErrInvalidComposition
	}
	target, err := NormalizeTarget(request.URL)
	if err != nil {
		return Result{}, err
	}
	listID := request.ListID
	if listID == "" {
		listID, err = ListIDFor(target.RegistrableDomain)
		if err != nil {
			return Result{}, err
		}
	}
	if domain.ValidateSlug(listID) != nil {
		return Result{}, fmt.Errorf("%w: list id %q", ErrInvalidTarget, listID)
	}
	if !request.Confirm {
		return Result{Target: target, ListID: listID}, ErrConfirmationRequired
	}
	if deps.Observe == nil || deps.Write == nil {
		return Result{Target: target, ListID: listID}, ErrInvalidComposition
	}
	load := deps.Load
	if load == nil {
		load = LoadPage
	}

	page, loadErr := load(ctx, target, deps.Browser)
	if loadErr != nil {
		return Result{Target: target, ListID: listID, Confirmed: true, Page: page}, loadErr
	}
	draft, err := BuildDraft(DraftRequest{
		Target:      target,
		ListID:      listID,
		Title:       request.Title,
		Page:        page,
		ManualSeeds: request.ManualSeeds,
	})
	if err != nil {
		return Result{Target: target, ListID: listID, Confirmed: true, Page: page}, err
	}
	// The draft is written first so the observation runs against the definition
	// the product actually loaded, with its real catalog and source revisions,
	// rather than against a synthetic identity that exists only here.
	path, err := deps.Write(ctx, draft.Definition)
	if err != nil {
		return Result{Target: target, ListID: listID, Confirmed: true, Page: page, Draft: draft}, err
	}
	result := Result{Target: target, ListID: listID, Confirmed: true, Page: page, Draft: draft, Path: path}
	observed, observeErr := deps.Observe(ctx, draft.Definition)
	result.Observed = observed
	if observeErr != nil {
		// The list now exists and is reviewable. A failed first observation
		// is reported, not fatal: refresh is the operation that owns retrying.
		result.ObservationError = observeErr.Error()
		return result, nil
	}
	result.Draft.Relations = observed.Relations
	return result, nil
}
