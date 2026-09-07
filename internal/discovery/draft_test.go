package discovery

import (
	"errors"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func testTarget(t *testing.T, raw string) Target {
	t.Helper()
	target, err := NormalizeTarget(raw)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestBuildDraftAcceptsSameSiteHostsAndKeepsThirdPartiesAsCandidates(t *testing.T) {
	target := testTarget(t, "https://www.example.co.uk/watch")
	page := PageLoad{
		FinalURL: target.URL,
		Hosts: []string{
			"www.example.co.uk",
			"static.example.co.uk",
			"api.a.example.co.uk",
			// Third parties of every shape discovery names: a shared CDN, an
			// authentication platform, an analytics endpoint, and an unrelated
			// registrant under the same public suffix.
			"cdn.thirdparty.test",
			"auth.identityvendor.test",
			"metrics.analytics.test",
			"other.co.uk",
		},
	}
	draft, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page})
	if err != nil {
		t.Fatal(err)
	}
	wantAccepted := []string{"api.a.example.co.uk", "static.example.co.uk", "www.example.co.uk"}
	if strings.Join(draft.AcceptedHosts, ",") != strings.Join(wantAccepted, ",") {
		t.Fatalf("accepted = %v, want %v", draft.AcceptedHosts, wantAccepted)
	}
	// The registrable domain is a routing seed even though the page was served
	// from a subdomain and the apex was never reached.
	if strings.Join(draft.SeedDomains, ",") != "example.co.uk" {
		t.Fatalf("seed domains = %v", draft.SeedDomains)
	}
	candidateHosts := make([]string, 0, len(draft.Candidates))
	for _, candidate := range draft.Candidates {
		candidateHosts = append(candidateHosts, candidate.Host)
		if len(candidate.Reasons) == 0 || candidate.Reasons[0] != CandidateThirdPartyDomain {
			t.Fatalf("candidate %#v must be recorded as a third party", candidate)
		}
	}
	wantCandidates := []string{"auth.identityvendor.test", "cdn.thirdparty.test", "metrics.analytics.test", "other.co.uk"}
	if strings.Join(candidateHosts, ",") != strings.Join(wantCandidates, ",") {
		t.Fatalf("candidates = %v, want %v", candidateHosts, wantCandidates)
	}

	// Exactly one seed, and it is the registrable domain. A third party never
	// becomes a suffix seed, which is what would widen routing to a shared
	// vendor.
	if len(draft.Definition.Seeds) != 1 {
		t.Fatalf("seeds = %#v", draft.Definition.Seeds)
	}
	seed := draft.Definition.Seeds[0]
	if seed.Kind != domain.RuleDomainSuffix || seed.Value != "example.co.uk" || seed.SourceClass != domain.SourceManual {
		t.Fatalf("seed = %#v", seed)
	}
	for _, candidate := range wantCandidates {
		for _, name := range draft.Definition.DNSNames {
			if name == candidate {
				t.Fatalf("third party %q reached the draft configuration", candidate)
			}
		}
	}
}

func TestBuildDraftNeverProducesAnAddressOrNetwork(t *testing.T) {
	target := testTarget(t, "https://example.com")
	page := PageLoad{Hosts: []string{"example.com", "cdn.example.com", "203.0.113.10", "2001:db8::1"}}
	draft, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page})
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range draft.Definition.Seeds {
		if !seed.Kind.IsDomain() {
			t.Fatalf("a browser session produced a non-domain seed: %#v", seed)
		}
	}
	for _, name := range draft.Definition.DNSNames {
		if _, err := domain.NormalizeDomain(name); err != nil {
			t.Fatalf("configured name %q is not a domain", name)
		}
		if strings.Contains(name, "/") || strings.Contains(name, ":") {
			t.Fatalf("configured name %q looks like a network", name)
		}
	}
	// An address literal observed by the browser is neither accepted nor
	// silently dropped: it cannot be a domain, so it stays a visible candidate.
	for _, name := range draft.Definition.DNSNames {
		if name == "203.0.113.10" || name == "2001:db8::1" {
			t.Fatalf("an observed address reached the draft: %q", name)
		}
	}
	if !contains(candidateHostsOf(draft), "203.0.113.10") {
		t.Fatalf("an observed address must stay a recorded candidate: %#v", draft.Candidates)
	}
	if _, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page, ManualSeeds: []string{"co.uk"}}); !errors.Is(err, ErrUnsafeSeed) {
		t.Fatalf("a public-suffix seed must be refused: %v", err)
	}
	if _, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page, ManualSeeds: []string{"203.0.113.0/24"}}); !errors.Is(err, ErrUnsafeSeed) {
		t.Fatalf("a network seed must be refused: %v", err)
	}
	if _, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page, ManualSeeds: []string{"203.0.113.10"}}); !errors.Is(err, ErrUnsafeSeed) {
		t.Fatalf("an address seed must be refused: %v", err)
	}
}

func TestBuildDraftRecordsManualSeedsAndRefusedHosts(t *testing.T) {
	target := testTarget(t, "https://example.com")
	page := PageLoad{
		Hosts:   []string{"example.com"},
		Blocked: map[string]string{"internal.thirdparty.test": RefusedLocalDestination},
	}
	draft, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: page, ManualSeeds: []string{"Example-Cdn.test", "example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, 0, len(draft.Definition.Seeds))
	for _, seed := range draft.Definition.Seeds {
		values = append(values, seed.Value)
	}
	// The duplicate of the registrable domain is collapsed; the explicit extra
	// domain is kept because the user asked for it.
	if strings.Join(values, ",") != "example.com,example-cdn.test" {
		t.Fatalf("seeds = %v", values)
	}
	if reasons := draft.Decisions["example-cdn.test"]; len(reasons) != 1 || reasons[0] != AcceptedManualSeed {
		t.Fatalf("manual seed decision = %#v", draft.Decisions)
	}
	if len(draft.Candidates) != 1 || draft.Candidates[0].Host != "internal.thirdparty.test" {
		t.Fatalf("candidates = %#v", draft.Candidates)
	}
	if !contains(draft.Candidates[0].Reasons, CandidateRefusedByPolicy) || !contains(draft.Candidates[0].Reasons, RefusedLocalDestination) {
		t.Fatalf("refused candidate reasons = %#v", draft.Candidates[0].Reasons)
	}
}

func TestBuildDraftRefusesAnInvalidIdentityOrTooManyHosts(t *testing.T) {
	target := testTarget(t, "https://example.com")
	if _, err := BuildDraft(DraftRequest{Target: target, ListID: "Bad Id", Page: PageLoad{Hosts: []string{"example.com"}}}); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal("an invalid list identity must be refused")
	}
	if _, err := BuildDraft(DraftRequest{Target: Target{}, ListID: "example"}); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal("an empty target must be refused")
	}
	hosts := make([]string, 0, MaxDraftNames+2)
	for index := 0; index <= MaxDraftNames+1; index++ {
		hosts = append(hosts, "h"+string(rune('a'+index%26))+string(rune('a'+index/26))+".example.com")
	}
	if _, err := BuildDraft(DraftRequest{Target: target, ListID: "example", Page: PageLoad{Hosts: hosts}}); !errors.Is(err, ErrNoUsableEvidence) {
		t.Fatal("exceeding the host bound must be reported, not truncated")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func candidateHostsOf(draft Draft) []string {
	hosts := make([]string, 0, len(draft.Candidates))
	for _, candidate := range draft.Candidates {
		hosts = append(hosts, candidate.Host)
	}
	return hosts
}
