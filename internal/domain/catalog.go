package domain

import "fmt"

type SourceClass string

const (
	SourceManual    SourceClass = "manual"
	SourceOfficial  SourceClass = "official"
	SourceObserved  SourceClass = "observed"
	SourceCommunity SourceClass = "community"
	SourceMetadata  SourceClass = "metadata"
)

type ComponentDefinition struct {
	ID       string
	Required bool
}

type Seed struct {
	Kind        RuleKind
	Value       string
	ComponentID string
	SourceID    string
	SourceClass SourceClass
}

func (s Seed) Normalize() (Seed, error) {
	if s.SourceClass == "" {
		s.SourceClass = SourceManual
	}
	switch s.Kind {
	case RuleDomainExact, RuleDomainSuffix:
		d, err := NormalizeDomain(s.Value)
		if err != nil {
			return Seed{}, err
		}
		s.Value = d
	case RuleIPv4, RuleIPv6:
		a, err := ParseAddr(s.Value)
		if err != nil {
			return Seed{}, err
		}
		s.Value = a.String()
		if (s.Kind == RuleIPv4) != a.Is4() {
			return Seed{}, ErrInvalidAddr
		}
	case RulePrefix4, RulePrefix6:
		p, err := ParsePrefix(s.Value)
		if err != nil {
			return Seed{}, err
		}
		s.Value = p.String()
		if (s.Kind == RulePrefix4) != p.Addr().Is4() {
			return Seed{}, ErrInvalidPrefix
		}
	default:
		return Seed{}, fmt.Errorf("unsupported seed kind %q", s.Kind)
	}
	return s, nil
}

type ListDefinition struct {
	ID              string
	Title           string
	Components      []ComponentDefinition
	Seeds           []Seed
	DNSNames        []string
	Sources         []SourceDefinition
	CatalogRevision string
}

// CategoryDefinition is a catalog-supplied grouping of services. It owns no
// data of its own, and a service belongs to as many categories as fit it: the
// grouping lives here rather than as a field on the service precisely so that
// membership can be many-to-many (ADR 0016).
type CategoryDefinition struct {
	ID    string
	Title string
	Lists []string
}

type SourceType string

const (
	SourceDNS  SourceType = "dns"
	SourceHTTP SourceType = "http"
)

// FeedFormat is the wire shape of an HTTP network feed. The set stays closed so
// a catalog entry cannot ask the source for an unimplemented decoder.
type FeedFormat string

const (
	FeedFormatText FeedFormat = "text"
	FeedFormatJSON FeedFormat = "json"
	// FeedFormatDomainList is the v2fly domain-list dialect: bare names with
	// optional "full:", "keyword:", "regexp:" and "include:" directives. It is
	// a separate format because the same bytes read as plain text would take a
	// pattern for a name.
	FeedFormatDomainList FeedFormat = "domain-list"
)

// SourceDefinition carries the configuration consumed by the implemented source
// cycles. Names belong to the DNS cycle; URL and Format belong to the HTTP feed
// cycle. Source-specific configuration belongs with the source that
// consumes it rather than a general adapter schema.
type SourceDefinition struct {
	ID          string
	Type        SourceType
	ComponentID string
	Names       []string
	URL         string
	Format      FeedFormat
	// Class is what this source is, declared by the catalog rather than assumed
	// by the reader. A third-party list is community curation; only a vendor's
	// own publication about itself is official.
	Class SourceClass
	// ImplementationRevision names the external source protocol revision the
	// catalog expects. Built-ins leave it empty because their implementation
	// revision is compiled into the loader; plugin sources must match their
	// installed manifest before a cycle may run.
	ImplementationRevision string
	// Revision binds the implementation revision and all source configuration
	// into the observation identity stored by Routevane.
	Revision string
}

func ExampleListDefinition() ListDefinition {
	return ListDefinition{
		ID:              "example",
		Title:           "Example",
		Components:      []ComponentDefinition{{ID: "web", Required: true}},
		Seeds:           []Seed{{Kind: RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:example.com", SourceClass: SourceManual}},
		DNSNames:        []string{"example.com"},
		Sources:         []SourceDefinition{{ID: "dns", Type: SourceDNS, ComponentID: "web", Names: []string{"example.com"}, Revision: "stdlib-dns-v1"}},
		CatalogRevision: "m0-example-v1",
	}
}

// Component identities shared by discovery, the catalog, and the planner. The
// set is closed: an observation is attributed to one of these or stays unknown.
const (
	ComponentCore        = "core"
	ComponentAuth        = "auth"
	ComponentMedia       = "media"
	ComponentVoice       = "voice"
	ComponentDownloads   = "downloads"
	ComponentTelemetry   = "telemetry"
	ComponentAdvertising = "advertising"
	ComponentThirdParty  = "third-party"
)

// KnownComponents is the ordered taxonomy. Order is presentation order and is
// deliberately stable.
func KnownComponents() []string {
	return []string{
		ComponentCore, ComponentAuth, ComponentMedia, ComponentVoice,
		ComponentDownloads, ComponentTelemetry, ComponentAdvertising, ComponentThirdParty,
	}
}

// KnownComponent reports whether an identity belongs to the taxonomy.
func KnownComponent(value string) bool {
	for _, known := range KnownComponents() {
		if known == value {
			return true
		}
	}
	return false
}

// RequiredComponent reports whether a component must be covered for a service to
// be considered complete. Telemetry, advertising, and shared third parties are
// dependencies, never requirements.
func RequiredComponent(value string) bool {
	switch value {
	case ComponentCore, ComponentAuth, ComponentMedia, ComponentVoice, ComponentDownloads:
		return true
	default:
		return false
	}
}
