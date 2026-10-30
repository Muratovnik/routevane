package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ListContents is the one table the list card renders: every
// destination the list currently stands for — domains, IP addresses and
// networks — each with where it came from and whether the operator keeps it
// on. It merges the static definition with the stored observations — the same
// material the next build reads — so the card and the artifact cannot tell
// two different stories.
type ListContents struct {
	ListID  string               `json:"list_id"`
	Rows    []ListContentsRow    `json:"rows"`
	Sources []ListContentsSource `json:"sources"`
	// Observed means every enabled source has a successful current read. A
	// format row is written before source work and cannot establish this fact.
	Observed bool `json:"observed"`
	// Freshness is the list's latest read and what it changed, the same
	// summary the library listing carries.
	Freshness ListFreshness `json:"freshness"`
}

// ListContentsRow is one destination of the list. Kind is "domain",
// "ip" or "prefix". Origin names where the value comes from: "catalog" for a
// shipped seed, "manual" for an operator entry, otherwise the id of the
// automatic source that observed it. Missing marks a standing verdict whose
// value no current material offers.
//
// Definition marks a value of the list's own static definition: a shipped
// seed, or a domain the operator wrote into a list they created. Origin cannot
// say this for an operator's list, where the definition and an include verdict
// are both "manual". It decides the switch: a definition value is switched off
// by an exclude verdict and back on by auto, while a value that stands on an
// include verdict alone is withdrawn by auto.
type ListContentsRow struct {
	Value      string `json:"value"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin"`
	Enabled    bool   `json:"enabled"`
	Missing    bool   `json:"missing,omitempty"`
	Definition bool   `json:"definition,omitempty"`
}

// ListContentsSource is one automatic source with the operator's switch.
// The URL is disclosed only for the operator's own feeds: it is their data,
// while a catalog feed's configuration stays behind the catalog.
//
// LastSuccessAt and LastFailureAt are the source's latest successful and
// failed cycles at its current revision, nil when there was none; LastError is
// the stable code of that failure. A switched-off source keeps reporting its
// history, so switching it back on shows how old its material is.
type ListContentsSource struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	Custom        bool       `json:"custom"`
	Enabled       bool       `json:"enabled"`
	URL           string     `json:"url,omitempty"`
	State         string     `json:"state"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastFailureAt *time.Time `json:"last_failure_at"`
	LastError     string     `json:"last_error,omitempty"`
	revision      string
}

func (s *PublicationService) ListContents(ctx context.Context, listID string) (ListContents, error) {
	base, ok := s.baseDefinition(listID)
	if !ok {
		return ListContents{}, ErrNotFound
	}
	tuning := s.listTuning(listID)
	disabled := make(map[string]struct{}, len(tuning.DisabledSources))
	for _, id := range tuning.DisabledSources {
		disabled[id] = struct{}{}
	}
	excluded := make(map[string]struct{}, len(tuning.Excludes))
	for _, value := range tuning.Excludes {
		excluded[value] = struct{}{}
	}

	contents := ListContents{ListID: listID, Rows: []ListContentsRow{}, Sources: []ListContentsSource{}}
	for _, source := range base.Sources {
		_, off := disabled[source.ID]
		contents.Sources = append(contents.Sources, ListContentsSource{
			ID: source.ID, Type: string(source.Type), Enabled: !off, revision: source.Revision,
		})
	}
	for _, feed := range tuning.CustomSources {
		_, off := disabled[feed.ID]
		contents.Sources = append(contents.Sources, ListContentsSource{
			ID: feed.ID, Type: string(domain.SourceHTTP), Custom: true, Enabled: !off, URL: feed.URL,
			revision: customSourceRevision(feed),
		})
	}
	slices.SortFunc(contents.Sources, func(a, b ListContentsSource) int { return cmp.Compare(a.ID, b.ID) })

	// The static truth: shipped seeds are catalog rows for a shipped list
	// and the operator's own rows for a custom list; verdict includes are
	// always the operator's.
	staticOrigin := "catalog"
	if _, shipped := s.config.Definitions[listID]; !shipped {
		staticOrigin = "manual"
	}
	rows := make(map[string]*ListContentsRow)
	addRow := func(value, kind, origin string) *ListContentsRow {
		if existing, seen := rows[value]; seen {
			// A value several origins offer keeps the strongest claim: the
			// operator's own word over the catalog, the catalog over a feed.
			if originRank(origin) < originRank(existing.Origin) {
				existing.Origin = origin
			}
			return existing
		}
		_, off := excluded[value]
		row := &ListContentsRow{Value: value, Kind: kind, Origin: origin, Enabled: !off}
		rows[value] = row
		return row
	}
	// The untuned seeds are the definition, so an excluded one stays a row.
	for _, seed := range base.Seeds {
		addRow(seed.Value, rowKind(seed.Kind), staticOrigin).Definition = true
	}
	for _, value := range tuning.Includes {
		if canonical, kind, err := domain.NormalizeRuleValue(value); err == nil {
			addRow(canonical, rowKind(kind), "manual")
		}
	}

	// The observed truth: the same stored sightings the next build reads,
	// filtered by the effective source revisions, so a disabled source's
	// domains leave this table exactly when they leave the plans.
	effective := s.tunedDefinition(base)
	cutoff := s.config.Clock.Now().UTC()
	if cutoff.IsZero() {
		return ListContents{}, fmt.Errorf("clock returned zero time")
	}
	rawFormat := domain.RawJSONTargetDefinition()
	snapshot, err := s.config.Store.ReadPlanningSnapshot(ctx, listID, sourceRevisions(effective), rawFormat.FormatKey, cutoff)
	switch {
	case errors.Is(err, ErrNotFound):
		// Never refreshed: the static rows are the whole story so far.
	case err != nil:
		return ListContents{}, fmt.Errorf("read list observations: %w", err)
	default:
		if snapshot.Format.ListID != listID || snapshot.Format.FormatKey != rawFormat.FormatKey || snapshot.Format.TargetID != rawFormat.ID || snapshot.Format.RendererID != rawFormat.RendererID {
			return ListContents{}, ErrFormatMismatch
		}
		for _, sighting := range snapshot.Sightings {
			if sighting.Validity != domain.ValidityValid {
				continue
			}
			addRow(sighting.Resource.CanonicalValue(), string(sighting.Resource.Kind), sighting.SourceID)
		}
	}
	observations := snapshotObservations(snapshot)
	contents.Observed = true
	for i := range contents.Sources {
		source := &contents.Sources[i]
		source.State = "disabled"
		if source.Enabled {
			source.State = enabledSourceState(observations, effective, source.ID, cutoff)
		}
		if source.Enabled && source.State != "ready" {
			contents.Observed = false
		}
		if state, ok := sourceHealth(observations, source.ID, source.revision); ok {
			source.LastSuccessAt = optionalTime(state.LastSuccessAt)
			source.LastFailureAt = optionalTime(state.LastFailureAt)
			if source.LastFailureAt != nil {
				source.LastError = state.LastErrorCode
			}
		}
	}
	contents.Freshness = listFreshness(observations, effective, cutoff)

	// A standing verdict whose value no material offers anymore is still the
	// operator's decision; it stays visible instead of silently evaporating.
	for _, value := range tuning.Excludes {
		if _, seen := rows[value]; !seen {
			kind := string(domain.ResourceDomain)
			if _, ruleKind, err := domain.NormalizeRuleValue(value); err == nil {
				kind = rowKind(ruleKind)
			}
			rows[value] = &ListContentsRow{Value: value, Kind: kind, Enabled: false, Missing: true}
		}
	}

	for _, row := range rows {
		contents.Rows = append(contents.Rows, *row)
	}
	// Domains first, then addresses, then networks; each block alphabetical, so
	// the readable names lead and the numeric material follows.
	slices.SortFunc(contents.Rows, func(a, b ListContentsRow) int {
		return cmp.Or(cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)), cmp.Compare(a.Value, b.Value))
	})
	return contents, nil
}

// optionalTime answers a stored instant as an RFC 3339 UTC value, and a
// moment that never happened as null.
func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

// rowKind folds the six rule kinds into the three the card names.
func rowKind(kind domain.RuleKind) string {
	switch {
	case kind.IsIP():
		return string(domain.ResourceIP)
	case kind.IsPrefix():
		return string(domain.ResourcePrefix)
	default:
		return string(domain.ResourceDomain)
	}
}

func kindRank(kind string) int {
	switch kind {
	case string(domain.ResourceDomain):
		return 0
	case string(domain.ResourceIP):
		return 1
	default:
		return 2
	}
}

// originRank orders competing claims about one domain: the operator's own word
// first, the shipped catalog second, an automatic source last.
func originRank(origin string) int {
	switch origin {
	case "manual":
		return 0
	case "catalog":
		return 1
	default:
		return 2
	}
}

// RefreshListByID re-observes one list's automatic sources and persists
// the result, so the contents table and the next build read the same fresh
// material. It is the list card's refresh: no list is touched and nothing
// is published.
func (s *PublicationService) RefreshListByID(ctx context.Context, listID string) (ListRefresh, error) {
	definition, ok := s.definition(listID)
	if !ok {
		return ListRefresh{}, ErrNotFound
	}
	rawFormat := domain.RawJSONTargetDefinition()
	summary, err := RefreshList(ctx, definition, rawFormat, s.config.Sources, s.config.Store, s.config.Clock)
	refresh := ListRefresh{RefreshSummary: summary}
	if err != nil && !errors.Is(err, ErrSourceDegraded) {
		return refresh, err
	}
	// What this refresh changed is read back from what it stored, through the
	// same function as the card and the library, so the three cannot differ.
	snapshot, err := s.config.Store.ReadPlanningSnapshot(ctx, listID, sourceRevisions(definition), rawFormat.FormatKey, summary.ObservedAt)
	if err != nil {
		return refresh, fmt.Errorf("read refreshed observations: %w", err)
	}
	freshness := listFreshness(snapshotObservations(snapshot), definition, summary.ObservedAt)
	if freshness.LastReadAt != nil && freshness.LastReadAt.Equal(summary.ObservedAt) {
		refresh.Added, refresh.Removed = freshness.Added, freshness.Removed
	}
	return refresh, nil
}

// ListRefresh is the list card's refresh answer: the cycle counts and the
// change this refresh brought. Added and Removed mean what they mean in
// ListFreshness, for this read; both are zero when no source was read.
type ListRefresh struct {
	RefreshSummary
	Added   int `json:"added"`
	Removed int `json:"removed"`
}
