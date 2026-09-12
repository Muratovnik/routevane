package application

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// These are the read-model projections a screen asks for: the catalog as it
// currently stands, and what each stored profile and output currently resolves
// to. Every one of them only reads Store, Categories, or Targets and returns
// a presentation shape; none is called from Build, AddOutput, or CreateProfile,
// and none of them calls into those either.

// ListDetail is the catalog metadata that is safe for the local control
// surface. It deliberately omits source configuration and routing evidence.
type ListDetail struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Categories are every catalog grouping that names this list. A list
	// belongs to as many as fit it, so this is a set rather than one label.
	Categories []string `json:"categories"`
	// Domains are only the bounded rules shipped in the local catalog. Fetched
	// observations and source configuration stay behind diagnostics and never
	// enter this collection response.
	Domains []ListDomain `json:"domains"`
	// Sources names the configured automatic inputs without exposing their URL
	// or other network configuration. They explain where additional rules come
	// from while the editable domain set remains a bounded profile-local override.
	Sources     []ListSource `json:"sources"`
	SourceCount int          `json:"source_count"`
	// Custom marks an operator-defined list. The screen offers editing its
	// own definition only where this is true: a shipped catalog entry is data
	// the process reads, not a stored row it may rewrite.
	Custom bool `json:"custom,omitempty"`
}

// ListDomain is a human-readable catalog rule. IncludeSubdomains is kept
// separate from Value so the UI can explain suffix semantics without asking a
// user to decode an internal rule kind.
type ListDomain struct {
	Value             string `json:"value"`
	IncludeSubdomains bool   `json:"include_subdomains"`
}

type ListSource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// CategoryDetail is one category as a screen needs it: what to call it and
// which lists it carries after the operator's overlay is merged with the
// shipped catalog (ADR 0028).
type CategoryDetail struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Lists is the merged membership, not the catalog's own. A screen that
	// resolved the overlay itself would answer differently from the build
	// whenever its copy was a moment stale.
	Lists []string `json:"lists"`
	// Custom marks a category the operator created: it may be renamed and
	// deleted, while a shipped one may only gain and lose members. It carries
	// no omitempty because false is the answer for every shipped category and
	// an absent field would leave the surface guessing.
	Custom bool `json:"custom"`
}

// TargetOption is one selectable device as a screen needs to describe it: what
// the operator calls it, and what kind of file it will receive. The file
// identity comes from the renderer descriptor rather than from a profile a screen
// keeps, so a target added by a plugin describes itself correctly.
type TargetOption struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Kind          string `json:"kind"`
	FormatKey     string `json:"format_key"`
	RendererID    string `json:"renderer_id"`
	FileExtension string `json:"file_extension"`
	// ManualInstallationHint is what the operator does with the file. It is
	// catalog text, so it is the same sentence the command line prints.
	ManualInstallationHint string `json:"manual_installation_hint"`
	// The English pair is omitted when the catalog carries no translation, so a
	// caller falls back to the catalog's own language rather than showing an
	// empty name. Title has an honest fallback in the target id and takes it;
	// an English title has none, because an id is not a translation.
	TitleEN                  string `json:"title_en,omitempty"`
	ManualInstallationHintEN string `json:"manual_installation_hint_en,omitempty"`
}

// OutputArtifact is the latest publication of one output. The snapshot
// identity travels with it so a screen can open the build's diagnostics
// without a local record of the build.
type OutputArtifact struct {
	ID               string    `json:"id"`
	SnapshotID       string    `json:"snapshot_id"`
	SizeBytes        int64     `json:"size_bytes"`
	ContentType      string    `json:"content_type"`
	ContentCreatedAt time.Time `json:"content_created_at"`
}

// OutputCard is one output as a screen states it. Target identity is resolved
// against the current catalog; an output whose target left the catalog still
// appears under its stored identity, because hiding it would misreport what
// the store holds.
type OutputCard struct {
	ID              string          `json:"id"`
	TargetID        string          `json:"target_id"`
	DeviceID        string          `json:"device_id,omitempty"`
	FQDNGroupPrefix string          `json:"fqdn_group_prefix,omitempty"`
	TargetTitle     string          `json:"target_title"`
	TargetKind      string          `json:"target_kind,omitempty"`
	FileExtension   string          `json:"file_extension,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Latest          *OutputArtifact `json:"latest,omitempty"`
	LastAttempt     *OutputAttempt  `json:"last_attempt,omitempty"`
}

// ProfileCard is one row of the library screen: the profile itself plus the outputs
// it currently feeds.
type ProfileCard struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Lists       []string            `json:"lists"`
	Categories  []string            `json:"categories"`
	Exclusions  []string            `json:"exclusions"`
	ListDomains map[string][]string `json:"list_domains,omitempty"`
	Priority    []string            `json:"priority"`
	// Resolved is what the profile publishes right now: named lists plus every
	// category's members, minus exclusions, deduplicated. A screen shows this
	// and can still explain it, because the stored parts are here too.
	Resolved []string `json:"resolved"`
	// MissingCategories names references the catalog no longer supplies. The
	// profile keeps working on what remains rather than shrinking silently.
	MissingCategories []string     `json:"missing_categories"`
	ArchivedAt        time.Time    `json:"archived_at,omitzero"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	Outputs           []OutputCard `json:"outputs"`
}

// ProfilePage is one stable, bounded slice of the profile library. NextCursor is
// empty only after the final page; it is an internal profile identity rather than
// an offset so rows created while a page is being read cannot shift later rows.
type ProfilePage struct {
	Profiles   []Profile `json:"-"`
	NextCursor string    `json:"-"`
}

// ProfileCardPage is the browser-safe representation of one ProfilePage.
// The cursor deliberately names no time or storage implementation detail.
type ProfileCardPage struct {
	Profiles   []ProfileCard `json:"profiles"`
	NextCursor string        `json:"next"`
}

// Lists names every list the library currently holds. A list the operator
// removed is absent, decided by the same accessor a build resolves an id with,
// so the picker and the plan can never disagree about what exists (ADR 0029).
func (s *PublicationService) Lists() []string {
	custom := s.customLists()
	ids := make([]string, 0, len(s.config.Definitions)+len(custom))
	for id := range s.config.Definitions {
		ids = append(ids, id)
	}
	for _, list := range custom {
		ids = append(ids, list.ID)
	}
	slices.Sort(ids)
	held := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.knownList(id) {
			held = append(held, id)
		}
	}
	return held
}

func (s *PublicationService) ListDetails() []ListDetail {
	membership := make(map[string][]string, len(s.config.Definitions))
	for _, category := range s.mergedCategories() {
		for _, listID := range category.Lists {
			membership[listID] = append(membership[listID], category.ID)
		}
	}
	ids := s.Lists()
	details := make([]ListDetail, 0, len(ids))
	for _, id := range ids {
		definition, known := s.definition(id)
		if !known {
			continue
		}
		_, shipped := s.config.Definitions[id]
		domainModes := make(map[string]bool)
		for _, seed := range definition.Seeds {
			if seed.Kind != domain.RuleDomainExact && seed.Kind != domain.RuleDomainSuffix {
				continue
			}
			domainModes[seed.Value] = domainModes[seed.Value] || seed.Kind == domain.RuleDomainSuffix
		}
		domainValues := make([]string, 0, len(domainModes))
		for value := range domainModes {
			domainValues = append(domainValues, value)
		}
		slices.Sort(domainValues)
		domains := make([]ListDomain, 0, len(domainValues))
		for _, value := range domainValues {
			domains = append(domains, ListDomain{Value: value, IncludeSubdomains: domainModes[value]})
		}
		sources := make([]ListSource, 0, len(definition.Sources))
		for _, source := range definition.Sources {
			sources = append(sources, ListSource{ID: source.ID, Type: string(source.Type)})
		}
		slices.SortFunc(sources, func(a, b ListSource) int { return cmp.Compare(a.ID, b.ID) })
		details = append(details, ListDetail{
			ID: definition.ID, Title: definition.Title,
			Categories: domain.StableStrings(membership[id]), Domains: domains, Sources: sources, SourceCount: len(sources),
			Custom: !shipped,
		})
	}
	return details
}

// Categories lists every category in a stable order, shipped and
// operator-created alike, with the operator's membership overlay applied. It
// reads through the same accessor the planner expands a profile with, so the
// picker and the build can never describe a category differently.
func (s *PublicationService) Categories() []CategoryDetail {
	return s.mergedCategories()
}

// Targets lists every selectable target in a stable order. A target whose
// renderer cannot be resolved is omitted rather than offered, for the same
// reason the catalog drops it: an unserviceable option must not be selectable.
func (s *PublicationService) Targets() []TargetOption {
	ids := slices.Sorted(maps.Keys(s.config.Targets))
	options := make([]TargetOption, 0, len(ids))
	for _, id := range ids {
		target, renderer, err := s.target(id)
		if err != nil {
			continue
		}
		descriptor := renderer.Descriptor()
		title := target.Title
		if title == "" {
			title = target.ID
		}
		options = append(options, TargetOption{
			ID: target.ID, Title: title, Kind: target.Kind, FormatKey: target.FormatKey, RendererID: target.RendererID,
			FileExtension:            descriptor.FileExtension,
			ManualInstallationHint:   target.ManualInstallationHint,
			TitleEN:                  target.TitleEN,
			ManualInstallationHintEN: target.ManualInstallationHintEN,
		})
	}
	return options
}

// ProfileCards keeps the original first-page read for application consumers
// that do not yet page. Browser-facing consumers use ProfileCardsPage so a
// profile can never become invisible merely because it is older than the page.
func (s *PublicationService) ProfileCards(ctx context.Context) ([]ProfileCard, error) {
	page, err := s.ProfileCardsPage(ctx, "")
	if err != nil {
		return nil, err
	}
	return page.Profiles, nil
}

// ProfileCardsPage lists one profile page with the outputs each row owns. A
// latest artifact that cannot be read is omitted from its output rather than
// failing the listing: the profile still exists and says so.
func (s *PublicationService) ProfileCardsPage(ctx context.Context, afterID string) (ProfileCardPage, error) {
	page, err := s.config.Store.ProfilePage(ctx, afterID)
	if err != nil {
		return ProfileCardPage{}, err
	}
	cards := make([]ProfileCard, 0, len(page.Profiles))
	for _, profile := range page.Profiles {
		// Reading by profile keeps every row's actions intact even where a
		// library page contains more outputs than the old global shelf bound.
		outputs, err := s.config.Store.OutputsByProfile(ctx, profile.ID)
		if err != nil {
			return ProfileCardPage{}, err
		}
		card := ProfileCard{
			ID: profile.ID, Name: profile.Name,
			Lists: profile.Lists, Categories: profile.Categories, Exclusions: profile.Exclusions, ListDomains: profile.ListDomains, Priority: profile.Priority,
			Resolved: s.ResolvedLists(profile), MissingCategories: s.MissingCategories(profile),
			ArchivedAt: profile.ArchivedAt,
			CreatedAt:  profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
			Outputs: make([]OutputCard, 0, len(outputs)),
		}
		for _, output := range outputs {
			card.Outputs = append(card.Outputs, s.outputCard(ctx, output))
		}
		cards = append(cards, card)
	}
	return ProfileCardPage{Profiles: cards, NextCursor: page.NextCursor}, nil
}

// OutputCards describes one profile's outputs for the profile screen.
func (s *PublicationService) OutputCards(ctx context.Context, profileID string) ([]OutputCard, error) {
	outputs, err := s.Outputs(ctx, profileID)
	if err != nil {
		return nil, err
	}
	cards := make([]OutputCard, 0, len(outputs))
	for _, output := range outputs {
		cards = append(cards, s.outputCard(ctx, output))
	}
	return cards, nil
}

func (s *PublicationService) outputCard(ctx context.Context, output Output) OutputCard {
	card := OutputCard{
		ID: output.ID, TargetID: output.TargetID, DeviceID: output.DeviceID, TargetTitle: output.TargetID,
		CreatedAt: output.CreatedAt, FQDNGroupPrefix: output.FQDNGroupPrefix,
	}
	if target, renderer, err := s.target(output.TargetID); err == nil {
		if target.Title != "" {
			card.TargetTitle = target.Title
		}
		card.TargetKind = target.Kind
		card.FileExtension = renderer.Descriptor().FileExtension
	}
	if output.LatestArtifactID != "" {
		if artifact, err := s.config.Store.ArtifactBuild(ctx, output.LatestArtifactID); err == nil {
			card.Latest = &OutputArtifact{
				ID: artifact.ID, SnapshotID: artifact.PlanSnapshotID, SizeBytes: artifact.SizeBytes,
				ContentType: artifact.ContentType, ContentCreatedAt: artifact.ContentCreatedAt,
			}
		}
	}
	if attempt, err := s.config.Store.LatestOutputAttempt(ctx, output.ID); err == nil {
		card.LastAttempt = &attempt
	}
	return card
}
