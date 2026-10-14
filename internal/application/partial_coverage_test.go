package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// A build refused for incomplete coverage names the lists behind the refusal,
// each by its identity and the title its definition carries, whichever order
// the lists were given in. Both the error chain and the classified failure say
// so, and the sentinel keeps answering for every caller that never asked.
func TestRefusedBuildNamesTheUncoveredListsByTitle(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bare := func(id, title string) domain.ListDefinition {
		definition := m2Definition(id)
		definition.Title = title
		return definition
	}
	covered := bare("alpha", "Альфа")
	covered.Seeds = []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:ip", SourceClass: domain.SourceManual}}
	definitions := []domain.ListDefinition{bare("charlie", "Чарли"), covered, bare("bravo", "Браво")}
	store := &m2Store{snapshots: map[string]PlanningSnapshot{}}
	revisions := map[string]map[string]string{}
	for _, definition := range definitions {
		store.snapshots[definition.ID] = m2Snapshot(definition, nil)
		revisions[definition.ID] = map[string]string{}
	}
	renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}

	_, err := BuildLists(context.Background(), definitions, revisions, m2Target(), store, renderer, &m2Output{}, ClockFunc(func() time.Time { return now }))
	var partial *PartialCoverageError
	if !errors.Is(err, ErrPartialCoverage) || !errors.As(err, &partial) {
		t.Fatalf("err = %v", err)
	}
	want := []UncoveredList{{ID: "bravo", Title: "Браво"}, {ID: "charlie", Title: "Чарли"}}
	if !reflect.DeepEqual(partial.Lists, want) {
		t.Fatalf("uncovered lists = %#v, want %#v and not the covered alpha", partial.Lists, want)
	}
	// Wrapping the refusal, as callers do, changes nothing about what it says.
	details := ClassifyBuildFailure(fmt.Errorf("build: %w", err))
	if details.Code != BuildFailurePartialCoverage || !reflect.DeepEqual(details.UncoveredLists, want) {
		t.Fatalf("details = %#v", details)
	}
	// The same refusal prepared without a render names them too.
	_, err = PrepareLists(context.Background(), definitions, revisions, m2Target(), store, renderer, now)
	if !errors.As(err, &partial) || !reflect.DeepEqual(partial.Lists, want) {
		t.Fatalf("prepare err = %v", err)
	}
}

// A partial coverage that carries no lists is still that failure, and says so
// without inventing any.
func TestPartialCoverageWithoutListsStaysClassified(t *testing.T) {
	details := ClassifyBuildFailure(fmt.Errorf("%w: incomplete", ErrPartialCoverage))
	if details.Code != BuildFailurePartialCoverage || len(details.UncoveredLists) != 0 {
		t.Fatalf("details = %#v", details)
	}
	other := ClassifyBuildFailure(ErrRuleLimit)
	if other.Code != BuildFailureRuleLimit || other.UncoveredLists != nil {
		t.Fatalf("a rule limit named lists: %#v", other)
	}
}

// A title is what the operator knows the list by, so an untitled list is named
// by its identity, and a hostile catalog cannot make the attempt unbounded.
func TestUncoveredListsAreBounded(t *testing.T) {
	titles := map[string]string{"plain": "  Plain  ", "long": strings.Repeat("я", maxUncoveredTitle+50)}
	got := uncoveredLists([]string{"blank", "long", "plain"}, titles)
	if got[0] != (UncoveredList{ID: "blank", Title: "blank"}) || got[2] != (UncoveredList{ID: "plain", Title: "Plain"}) {
		t.Fatalf("lists = %#v", got)
	}
	if runes := []rune(got[1].Title); len(runes) != maxUncoveredTitle || !strings.HasSuffix(got[1].Title, uncoveredTitleDots) {
		t.Fatalf("long title has %d runes: %q", len(runes), got[1].Title)
	}
	ids := make([]string, maxUncoveredLists+10)
	for i := range ids {
		ids[i] = fmt.Sprintf("list-%04d", i)
	}
	if got := uncoveredLists(ids, nil); len(got) != maxUncoveredLists || got[0].ID != "list-0000" {
		t.Fatalf("kept %d lists starting at %q", len(got), got[0].ID)
	}
}

// The attempt that a refused build leaves behind carries the lists, so the
// profile page names them after a reload and not only in the response.
func TestFailedBuildAttemptKeepsTheUncoveredLists(t *testing.T) {
	// An empty, non-nil set of observed domains is a store that has seen nothing,
	// so nothing but the definition's own seeds can cover the list.
	store := &publicationFakeStore{observedDomains: map[string][]string{}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x23}, 256)))
	definition := publication.config.Definitions["example"]
	definition.Title = "Пример"
	definition.Seeds = nil
	publication.config.Definitions["example"] = definition
	profile, output := testProfileAndOutput()
	store.profile, store.output = profile, output

	_, err := publication.Build(context.Background(), output.ID)
	if !errors.Is(err, ErrPartialCoverage) {
		t.Fatalf("err = %v", err)
	}
	if len(store.attempts) != 1 {
		t.Fatalf("attempts = %#v", store.attempts)
	}
	attempt := store.attempts[0]
	if attempt.Status != "failed" || attempt.Code != BuildFailurePartialCoverage || !reflect.DeepEqual(attempt.UncoveredLists, []UncoveredList{{ID: "example", Title: "Пример"}}) {
		t.Fatalf("attempt = %#v", attempt)
	}
	if len(store.published) != 0 {
		t.Fatal("a refused build published")
	}
}
