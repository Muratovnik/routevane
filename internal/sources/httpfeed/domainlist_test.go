package httpfeed

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func observeAs(t *testing.T, fixture *feedFixture, url string, format domain.FeedFormat, class domain.SourceClass) (Result, error) {
	t.Helper()
	return fixture.observer.Observe(context.Background(), Query{
		ListID: "example", ComponentID: "web", SourceID: "feed", SourceRevision: "rev",
		URL: url, Format: format, SourceClass: class,
	}, time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
}

// A third-party list is community curation. Recording it as official would tell
// the planner and the diagnostics that the vendor published it about itself.
func TestFeedRecordsTheDeclaredSourceClass(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("example.com\n"), Options{})
	result, err := observeAs(t, fixture, "https://example.com/feed.txt", domain.FeedFormatText, domain.SourceCommunity)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 1 || result.Sightings[0].SourceClass != domain.SourceCommunity {
		t.Fatalf("sightings = %#v", result.Sightings)
	}
}

// An undeclared class keeps the meaning every feed had before the class
// existed: the vendor's own publication.
func TestFeedWithoutADeclaredClassStaysOfficial(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("example.com\n"), Options{})
	result, err := observeAs(t, fixture, "https://example.com/feed.txt", domain.FeedFormatText, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 1 || result.Sightings[0].SourceClass != domain.SourceOfficial {
		t.Fatalf("sightings = %#v", result.Sightings)
	}
}

func TestFeedRefusesAnUnsupportedSourceClass(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("example.com\n"), Options{})
	if _, err := observeAs(t, fixture, "https://example.com/feed.txt", domain.FeedFormatText, domain.SourceObserved); err == nil {
		t.Fatal("expected an observed feed class to be refused")
	}
}

// The v2fly dialect is read strictly and partially: names and "full:" are
// taken, patterns and includes are refused, and every refusal is counted.
func TestDomainListDialectIsReadStrictlyAndPartially(t *testing.T) {
	body := strings.Join([]string{
		"# youtube",
		"youtube.com",
		"full:www.youtube.com",
		"domain:googlevideo.com",
		"keyword:youtube",
		`regexp:.*\.ytimg\.com$`,
		"include:google",
		"ytimg.com @cn",
		"ggpht.com@ads",
		"",
		"   ",
	}, "\n")
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(body), Options{})
	result, err := observeAs(t, fixture, "https://example.com/youtube", domain.FeedFormatDomainList, domain.SourceCommunity)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"youtube.com", "www.youtube.com", "googlevideo.com", "ytimg.com", "ggpht.com"}
	if len(result.Sightings) != len(want) {
		t.Fatalf("recorded %d, want %d: %#v", len(result.Sightings), len(want), result.Sightings)
	}
	for index, expected := range want {
		if got := result.Sightings[index].Resource.CanonicalValue(); got != expected {
			t.Fatalf("entry %d = %q, want %q", index, got, expected)
		}
	}
	// keyword:, regexp: and include: — three refusals, counted rather than
	// silently dropped.
	if result.Skipped != 3 {
		t.Fatalf("skipped = %d, want 3", result.Skipped)
	}
}

// A domain-list feed of nothing but patterns contributes nothing, and saying so
// is better than publishing an empty rule set.
func TestDomainListOfPatternsOnlyIsRefused(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("keyword:ads\nregexp:.*\n"), Options{})
	_, err := observeAs(t, fixture, "https://example.com/list", domain.FeedFormatDomainList, domain.SourceCommunity)
	if err == nil {
		t.Fatal("expected a pattern-only list to be refused")
	}
}
