package discovery

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrNoUsableEvidence = errors.New("discovery produced no usable evidence")
	ErrUnsafeSeed       = errors.New("a discovery seed must be a domain, never an address or network")
)

// Draft decision codes. They are stable identities reported in diagnostics.
const (
	// AcceptedRegistrableDomain is the entered site's own registrable domain.
	AcceptedRegistrableDomain = "registrable_domain"
	// AcceptedSameSiteHost is a host under that registrable domain which the
	// page actually used.
	AcceptedSameSiteHost = "same_site_host_used_by_page"
	// AcceptedManualSeed is a domain the user typed themselves.
	AcceptedManualSeed = "manual_seed"
	// CandidateThirdPartyDomain is any host outside the registrable domain. It
	// is recorded as a dependency and never activated, which is what keeps a
	// shared CDN, an authentication platform, and an analytics endpoint out of
	// the draft without maintaining a vendor list.
	CandidateThirdPartyDomain = "third_party_domain"
	// CandidateRefusedByPolicy is a host the destination policy refused.
	CandidateRefusedByPolicy = "refused_by_policy"
)

// DefaultComponentID is the single component a first draft carries. The wider
// component taxonomy belongs with the code that observes those behaviours.
const DefaultComponentID = "core"

// MaxDraftNames bounds how many DNS names one draft may configure.
const MaxDraftNames = 32

// Candidate is an observed dependency that is deliberately not activated.
type Candidate struct {
	Host              string   `json:"host"`
	RegistrableDomain string   `json:"registrable_domain,omitempty"`
	Reasons           []string `json:"reasons"`
	Requests          int      `json:"requests,omitempty"`
}

// Draft is the reviewable outcome of one discovery session.
type Draft struct {
	Definition domain.ListDefinition
	// AcceptedHosts are the same-site hosts the draft configures for observation.
	// They were actually reached, so a DNS cycle over them is expected to work.
	AcceptedHosts []string
	// SeedDomains are the domain suffixes the draft routes. The registrable
	// domain is always one of them, even when it does not resolve itself.
	SeedDomains []string
	// Candidates are dependencies recorded without activation.
	Candidates []Candidate
	// Relations are the CNAME targets observed for the accepted hosts.
	Relations []domain.Relation
	// Decisions maps each accepted host to why it was accepted.
	Decisions map[string][]string
}

// DraftRequest is everything BuildDraft needs. It takes hostnames and relations
// only: observed addresses stay in the observation store, so a browser request
// can never become a routing prefix.
type DraftRequest struct {
	Target      Target
	ListID      string
	Title       string
	Page        PageLoad
	ManualSeeds []string
	Relations   []domain.Relation
}

// BuildDraft turns one page load into a safe local list definition.
//
// It converts the page load into the same evidence shape a guided session
// produces and runs the same activation policy, so a single-page draft and a
// learned draft cannot disagree about what may be activated. One page load
// exercises the core component and nothing else.
func BuildDraft(request DraftRequest) (Draft, error) {
	if request.Target.RegistrableDomain == "" {
		return Draft{}, ErrInvalidTarget
	}
	return BuildLearnedDraft(LearnedDraftRequest{
		Target:      request.Target,
		ListID:      request.ListID,
		Title:       request.Title,
		Evidence:    evidenceFromPageLoad(request.Target, request.Page),
		Exercised:   []string{domain.ComponentCore},
		ManualSeeds: request.ManualSeeds,
		Relations:   request.Relations,
	})
}

// evidenceFromPageLoad attributes every same-site host of a single page load to
// the core component: one load is one action, and that action is opening the
// site.
func evidenceFromPageLoad(target Target, page PageLoad) SessionEvidence {
	hosts := make([]HostEvidence, 0, len(page.Hosts)+len(page.Blocked)+1)
	seen := map[string]struct{}{}
	add := func(evidence HostEvidence) {
		if _, known := seen[evidence.Host]; known {
			return
		}
		seen[evidence.Host] = struct{}{}
		hosts = append(hosts, evidence)
	}
	add(HostEvidence{Host: target.Host, Component: domain.ComponentCore, Requests: 1})
	for _, host := range page.Hosts {
		normalized, err := domain.NormalizeDomain(strings.ToLower(host))
		if err != nil {
			continue
		}
		component := ""
		if SameSite(target.RegistrableDomain, normalized) {
			component = domain.ComponentCore
		}
		add(HostEvidence{Host: normalized, Component: component, Requests: 1, LoadedBy: loaderOf(target.Host, normalized)})
	}
	for host, reason := range page.Blocked {
		normalized, err := domain.NormalizeDomain(strings.ToLower(host))
		if err != nil {
			continue
		}
		add(HostEvidence{Host: normalized, Refused: reason})
	}
	slices.SortFunc(hosts, func(a, b HostEvidence) int { return cmp.Compare(a.Host, b.Host) })
	return SessionEvidence{Target: target, Hosts: hosts, Requests: page.Requests, Bytes: page.Bytes}
}

func loaderOf(documentHost, host string) []string {
	if documentHost == "" || documentHost == host {
		return nil
	}
	return []string{documentHost}
}

// LearnedDraftRequest is the component-aware draft input. Evidence may come from
// a guided browser session, an imported archive, or both merged.
type LearnedDraftRequest struct {
	Target      Target
	ListID      string
	Title       string
	Evidence    SessionEvidence
	Exercised   []string
	ManualSeeds []string
	Relations   []domain.Relation
}

// BuildLearnedDraft applies the activation policy and turns accepted evidence
// into a definition with one component per exercised area.
//
// A dependency is recorded, never activated, and never widened: a third-party
// host does not become a suffix seed, an address never becomes a name, and an
// unexercised component contributes nothing.
func BuildLearnedDraft(request LearnedDraftRequest) (Draft, error) {
	if request.Target.RegistrableDomain == "" {
		return Draft{}, ErrInvalidTarget
	}
	if domain.ValidateSlug(request.ListID) != nil {
		return Draft{}, fmt.Errorf("%w: list id %q", ErrInvalidTarget, request.ListID)
	}
	registrable := request.Target.RegistrableDomain
	evidence := request.Evidence
	evidence.Target = request.Target
	decisions := Classify(evidence, request.Exercised)

	byComponent := map[string]map[string]struct{}{}
	candidates := make([]Candidate, 0, len(decisions))
	reported := map[string][]string{registrable: {AcceptedRegistrableDomain}}
	for _, decision := range decisions {
		if _, addrErr := netip.ParseAddr(decision.Host); addrErr == nil {
			// An address literal can never be a configurable name and is never
			// turned into a network. It stays a visible dependency.
			candidates = append(candidates, Candidate{Host: decision.Host, Reasons: decision.Reasons})
			continue
		}
		if decision.Outcome != ActivationAccepted {
			observedRegistrable, _ := RegistrableDomainOf(decision.Host)
			candidates = append(candidates, Candidate{Host: decision.Host, RegistrableDomain: observedRegistrable, Reasons: decision.Reasons})
			continue
		}
		component := decision.Component
		if !domain.KnownComponent(component) {
			component = domain.ComponentCore
		}
		if _, known := byComponent[component]; !known {
			byComponent[component] = map[string]struct{}{}
		}
		byComponent[component][decision.Host] = struct{}{}
		reported[decision.Host] = decision.Reasons
	}
	candidates = mergeCandidates(candidates)

	seedValues, err := draftSeedValues(registrable, request.ManualSeeds, reported)
	if err != nil {
		return Draft{}, err
	}

	components := make([]domain.ComponentDefinition, 0, len(byComponent))
	sources := make([]domain.SourceDefinition, 0, len(byComponent))
	accepted := make([]string, 0)
	for _, component := range domain.KnownComponents() {
		hosts, present := byComponent[component]
		if !present || len(hosts) == 0 {
			continue
		}
		names := slices.Sorted(maps.Keys(hosts))
		accepted = append(accepted, names...)
		components = append(components, domain.ComponentDefinition{ID: component, Required: domain.RequiredComponent(component)})
		sources = append(sources, domain.SourceDefinition{ID: "dns-" + component, Type: domain.SourceDNS, ComponentID: component, Names: names})
	}
	if len(components) == 0 {
		return Draft{}, ErrNoUsableEvidence
	}
	accepted = domain.StableStrings(accepted)
	if len(accepted) > MaxDraftNames {
		// Truncating silently would hide part of the site, so the bound is
		// reported rather than applied.
		return Draft{}, fmt.Errorf("%w: %d accepted hosts exceed the draft bound %d", ErrNoUsableEvidence, len(accepted), MaxDraftNames)
	}
	// A seed covers every component of the same list: the site's own domain is
	// as true for its media area as for its core. Without this a domain-capable
	// target would report an uncovered required component even though the domain
	// suffix already routes it.
	seeds := make([]domain.Seed, 0, len(seedValues)*len(components))
	for _, component := range components {
		for _, value := range seedValues {
			seeds = append(seeds, domain.Seed{Kind: domain.RuleDomainSuffix, Value: value, ComponentID: component.ID, SourceClass: domain.SourceManual})
		}
	}

	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = registrable
	}
	return Draft{
		Definition: domain.ListDefinition{
			ID:         request.ListID,
			Title:      title,
			Components: components,
			Seeds:      seeds,
			DNSNames:   accepted,
			Sources:    sources,
		},
		AcceptedHosts: accepted,
		SeedDomains:   seedValues,
		Candidates:    candidates,
		Relations:     request.Relations,
		Decisions:     reported,
	}, nil
}

// draftSeedValues validates the routing seed domains. The registrable domain is
// always one of them, even when it never resolves itself, and a manual seed is
// validated rather than trusted.
func draftSeedValues(registrable string, manual []string, decisions map[string][]string) ([]string, error) {
	values := []string{registrable}
	for _, value := range manual {
		normalized, err := domain.NormalizeDomain(strings.TrimSpace(strings.ToLower(value)))
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrUnsafeSeed, value)
		}
		// A numeric label sequence is a syntactically valid domain name, so an
		// address literal would otherwise pass as a domain seed. A seed must also
		// sit under a public suffix: seeding a bare suffix would route a zone.
		if _, addrErr := netip.ParseAddr(normalized); addrErr == nil {
			return nil, fmt.Errorf("%w: %q is an address", ErrUnsafeSeed, value)
		}
		if _, domainErr := RegistrableDomainOf(normalized); domainErr != nil {
			return nil, fmt.Errorf("%w: %q has no registrable domain", ErrUnsafeSeed, value)
		}
		if normalized == registrable {
			continue
		}
		values = append(values, normalized)
		decisions[normalized] = domain.StableStrings(append(decisions[normalized], AcceptedManualSeed))
	}
	return values, nil
}

func mergeCandidates(candidates []Candidate) []Candidate {
	byHost := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		existing, known := byHost[candidate.Host]
		if !known {
			candidate.Reasons = domain.StableStrings(candidate.Reasons)
			candidate.Requests++
			byHost[candidate.Host] = candidate
			continue
		}
		existing.Reasons = domain.StableStrings(append(existing.Reasons, candidate.Reasons...))
		existing.Requests++
		byHost[candidate.Host] = existing
	}
	merged := make([]Candidate, 0, len(byHost))
	for _, candidate := range byHost {
		merged = append(merged, candidate)
	}
	slices.SortFunc(merged, func(a, b Candidate) int { return cmp.Compare(a.Host, b.Host) })
	return merged
}
