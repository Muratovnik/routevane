package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrIdentityCollision   = errors.New("random identity collision")
	ErrTargetChanged       = errors.New("output target mapping changed")
	ErrArtifactUnavailable = errors.New("published artifact unavailable")
	ErrPublicationStorage  = errors.New("publication storage failed")
	ErrOutputExists        = errors.New("output already exists for this target")
	ErrSubscriptionExists  = errors.New("output subscription already exists")
	ErrProfileArchived     = errors.New("profile is archived")
)

// Profile is the unit of storage and of output. Composition is stored as what the
// operator said, not as what it currently resolves to: the lists the profile
// names itself, the catalog categories it references, and what it excluded from
// them. Resolution happens on read, so a category that gains a list reaches
// every profile referencing it without an edit (ADR 0016).
type Profile struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Lists       []string            `json:"lists"`
	Categories  []string            `json:"categories"`
	Exclusions  []string            `json:"exclusions"`
	ListDomains map[string][]string `json:"list_domains,omitempty"`
	// Priority names the currently resolved lists from highest to lowest.
	// Category references remain live; lists they gain later are appended by
	// resolution rather than making an old profile invalid.
	Priority []string `json:"priority"`
	// RefreshInterval is this profile's own scheduling rule, empty when it follows
	// the service-wide default. The two are kept apart on purpose: a profile that
	// never disagreed follows the default as it changes.
	RefreshInterval   RefreshInterval `json:"refresh_interval"`
	LastRefreshedAt   time.Time       `json:"last_refreshed_at,omitzero"`
	LastRefreshFailed bool            `json:"last_refresh_failed"`
	// ArchivedAt is when the profile left the shelf, zero while it is on it. It is
	// the only stored fact about archival; whether a profile is archived is read
	// from it rather than kept beside it (see archive.go).
	//
	// Every moment that may not have happened is tagged omitzero rather than
	// omitempty: omitempty does not apply to a struct, so a zero time.Time
	// would reach the client as the year one, which reads as a real date
	// instead of as nothing.
	ArchivedAt time.Time `json:"archived_at,omitzero"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ProfileComposition is a requested composition before it is checked against the
// catalog. It is one argument rather than three positional slices so a caller
// cannot silently swap the excluded set for the named one.
type ProfileComposition struct {
	Lists       []string            `json:"lists"`
	Categories  []string            `json:"categories"`
	Exclusions  []string            `json:"exclusions"`
	ListDomains map[string][]string `json:"list_domains,omitempty"`
	Priority    []string            `json:"priority"`
}

// Output binds one profile to one renderer format. It owns the subscription and
// the chain of published artifacts; the lists it publishes come from its
// profile, so an edited profile is served by the same output rather than a new one.
type Output struct {
	ID        string `json:"id"`
	ProfileID string `json:"list_id"`
	TargetID  string `json:"target_id"`
	// DeviceID is the one registered destination this output may deliver to.
	// Empty means file/subscription only. The explicit binding prevents a timer
	// from guessing among multiple profiles for the same kind of device.
	DeviceID           string    `json:"device_id,omitempty"`
	FormatKey          string    `json:"profile_key"`
	RendererID         string    `json:"renderer_id"`
	RendererVersion    string    `json:"renderer_version"`
	TargetRevision     string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	LatestArtifactID   string    `json:"latest_artifact_id,omitempty"`
	PreviousArtifactID string    `json:"previous_artifact_id,omitempty"`
}

type PlanSnapshotRecord struct {
	ID                string    `json:"id"`
	OutputID          string    `json:"output_id"`
	RoutingPlanHash   string    `json:"routing_plan_hash"`
	RoutingPlanJSON   []byte    `json:"-"`
	PolicyVersion     string    `json:"policy_version"`
	CatalogRevision   string    `json:"catalog_revision"`
	ObservationCutoff time.Time `json:"observation_cutoff"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
}
type ArtifactBuildRecord struct {
	ID               string    `json:"id"`
	OutputID         string    `json:"output_id"`
	PlanSnapshotID   string    `json:"plan_snapshot_id"`
	RendererID       string    `json:"renderer_id"`
	RendererVersion  string    `json:"renderer_version"`
	ArtifactHash     string    `json:"artifact_hash"`
	ArtifactPath     string    `json:"-"`
	SizeBytes        int64     `json:"size_bytes"`
	ContentType      string    `json:"content_type"`
	ContentCreatedAt time.Time `json:"content_created_at"`
	ValidationStatus string    `json:"validation_status"`
	Status           string    `json:"status"`
}
type NewOutput struct {
	Output Output
	// Token fields are accepted by the repository import boundary for legacy
	// atomic restores. Product creation leaves them empty and issues the
	// subscription only after a successful build.
	TokenID   string
	TokenHash [32]byte
}

// OutputAttempt is the bounded, durable result of one publication attempt.
// It deliberately stores no raw error text, plan, source data, or bearer
// credential: screens need a stable reason and safe counts, not internals.
type OutputAttempt struct {
	OutputID       string    `json:"-"`
	Status         string    `json:"status"`
	Code           string    `json:"code,omitempty"`
	ProjectedRules int       `json:"projected_rules,omitempty"`
	MaximumRules   int       `json:"maximum_rules,omitempty"`
	ArtifactID     string    `json:"artifact_id,omitempty"`
	CompletedAt    time.Time `json:"completed_at"`
}
type PublicationCandidate struct {
	Snapshot PlanSnapshotRecord
	Artifact ArtifactBuildRecord
	Attempt  OutputAttempt
}

type PublicationRepository interface {
	// Setting and PutSetting carry the preferences the process acts on itself.
	Setting(context.Context, string) (string, error)
	PutSetting(context.Context, string, string, time.Time) error
	// UpdateProfileSchedule writes only the scheduling columns. It is separate
	// from UpdateProfile because a timer must not rewrite a composition, and a
	// composition edit must not reset when the profile last refreshed.
	UpdateProfileSchedule(ctx context.Context, profileID string, interval RefreshInterval, lastRefreshedAt time.Time, failed bool, updatedAt time.Time) error
	CreateProfile(context.Context, Profile) error
	Profile(context.Context, string) (Profile, error)
	// Profiles returns stored profiles newest first. The repository owns the bound
	// because this transport has no pagination parameters.
	Profiles(context.Context) ([]Profile, error)
	// UpdateProfile replaces the mutable part of a profile: its name and its
	// composition. Identity and creation time are rejected by the store.
	UpdateProfile(context.Context, Profile) error
	// SetProfileArchived writes only when the profile left or rejoined the shelf. A
	// zero moment is the restored state, so the column carries both the fact
	// and its date without a second flag to disagree with.
	SetProfileArchived(ctx context.Context, profileID string, archivedAt time.Time, updatedAt time.Time) error
	CreateOutput(context.Context, NewOutput) error
	UpdateOutputDevice(context.Context, string, string) error
	// The custom-list rows are part of the same repository: a composition
	// is validated against the catalog and this registry in one place, so the
	// two must not be able to come from different stores.
	CreateCustomList(context.Context, CustomList) error
	UpdateCustomList(context.Context, CustomList) error
	CustomLists(context.Context) ([]CustomList, error)
	// Categories are operator-owned over the catalog seed (ADR 0028): the
	// categories the operator created and the membership overlay over the
	// shipped ones. They belong to this repository because every reader merges
	// the two with the catalog, and two stores could disagree about what a
	// profile expands to.
	CreateCustomCategory(ctx context.Context, category CustomCategory, memberships []CategoryMembership) error
	UpdateCategory(context.Context, CategoryWrite) error
	// RemoveFromLibrary deletes one category or one list, and the lists a
	// category takes with it, as one transaction. A shipped object leaves a
	// removal record and an operator-created one leaves nothing; either way
	// the state that only made sense while the object existed goes with it
	// (ADR 0029).
	RemoveFromLibrary(context.Context, LibraryRemoval) error
	CategoryOverlay(context.Context) (CategoryOverlay, error)
	// List tuning rows are the operator's standing corrections to a
	// list's automatic material; the same repository holds them so the
	// effective definition composes from one store.
	SetSourceDisabled(ctx context.Context, listID, sourceID string, disabled bool) error
	CreateCustomSource(context.Context, CustomSource) error
	RemoveCustomSource(ctx context.Context, id string) error
	// SetDomainVerdicts writes one operator action about several destinations —
	// domains, addresses, networks — as one unit, because a pasted or imported
	// file is one decision rather than one per line.
	SetDomainVerdicts(ctx context.Context, listID string, values []string, verdict DomainVerdict) error
	ListTunings(context.Context) (map[string]ListTuning, error)
	CreateSubscription(ctx context.Context, outputID, tokenID string, tokenHash [32]byte, createdAt time.Time) error
	RecordOutputAttempt(context.Context, OutputAttempt) error
	LatestOutputAttempt(context.Context, string) (OutputAttempt, error)
	Output(context.Context, string) (Output, error)
	// OutputsByProfile returns one profile's outputs in a stable order; Outputs
	// returns every stored output, bounded the same way Profiles is.
	OutputsByProfile(context.Context, string) ([]Output, error)
	Outputs(context.Context) ([]Output, error)
	OutputBySubscription(context.Context, string, [32]byte) (Output, error)
	Publish(context.Context, PublicationCandidate) (Output, PlanSnapshotRecord, ArtifactBuildRecord, error)
	PlanSnapshot(context.Context, string) (PlanSnapshotRecord, error)
	ArtifactBuild(context.Context, string) (ArtifactBuildRecord, error)
}

// LibraryPriorityRepository is the optional persistence seam for the
// library-wide default ordering. It is deliberately separate from
// PublicationRepository so existing in-memory repositories and import-only
// callers are not forced to manufacture a global preference they do not use.
// The SQLite implementation stores this in its dedicated global table.
type LibraryPriorityRepository interface {
	DefaultPriority(context.Context) ([]string, error)
	SetDefaultPriority(context.Context, []string) error
}

// SetOutputDevice attaches one registered device to an output, or detaches it
// when device is nil. A target mismatch is refused before storage: format and
// device kind are an invariant of the connection, not a deploy-time guess.
func (s *PublicationService) SetOutputDevice(ctx context.Context, outputID string, device *Device) (Output, error) {
	output, err := s.Output(ctx, outputID)
	if err != nil {
		return Output{}, err
	}
	deviceID := ""
	if device != nil {
		if device.ID == "" || device.TargetID != output.TargetID {
			return Output{}, fmt.Errorf("device is incompatible with output target")
		}
		deviceID = device.ID
	}
	if err := s.config.Store.UpdateOutputDevice(ctx, output.ID, deviceID); err != nil {
		return Output{}, err
	}
	output.DeviceID = deviceID
	return output, nil
}

type PublishedFile struct {
	RelativePath string
	Hash         string
	Size         int64
	Reused       bool
}

// PublishedArtifacts stores and re-reads verified artifact bytes. It takes the
// renderer descriptor rather than a renderer id so the storage layer holds no
// per-format constant of its own: the descriptor supplies both the directory
// segment and the file suffix, and its validation makes both safe.
type PublishedArtifacts interface {
	PutPublished(context.Context, domain.RendererDescriptor, []byte) (PublishedFile, error)
	ReadPublished(context.Context, ArtifactBuildRecord, domain.RendererDescriptor) ([]byte, error)
}

type PublicationConfig struct {
	Definitions map[string]domain.ListDefinition
	// LocalListIDs identifies definitions loaded from the local catalog.
	// They are usable by this process, but cannot be named by an exported
	// portable configuration because another machine has no matching catalog
	// entry by definition.
	LocalListIDs   map[string]struct{}
	Target         domain.TargetDefinition
	TargetRevision string
	// FeedURL validates an operator-supplied feed URL with the same boundary
	// the catalog loader applies. It is injected by the composition so this
	// package stays below the network layer; without it, adding feeds is
	// refused rather than accepted unchecked.
	FeedURL func(string) error
	Store   interface {
		ObservationStore
		PublicationRepository
	}
	Files PublishedArtifacts
	// Categories is the catalog of groupings, keyed by category id. It may be
	// empty: a catalog with no categories still serves profiles that name lists
	// directly.
	Categories map[string]domain.CategoryDefinition
	// Targets is the catalog of selectable devices, keyed by target id.
	Targets map[string]domain.TargetDefinition
	// Renderers is the format registry, keyed by renderer id. Several targets
	// may share one renderer, so the two maps stay separate.
	Renderers RendererRegistry
	Sources   SourceRegistry
	Clock     Clock
	Entropy   io.Reader
}

type PublicationService struct {
	config PublicationConfig
	// catalogRevision is the one revision every shipped definition carries.
	// Custom lists are stamped with it too: the planner accepts exactly one
	// revision per plan, and an operator-defined list must compose with the
	// catalog it extends.
	catalogRevision string
	custom          customListRegistry
	tuning          listTuningRegistry
	// overlay is the operator's category ownership over the catalog seed. It
	// is read through mergedCategory and never directly.
	overlay categoryRegistry
	// registryMu makes a transferred custom library, tuning overlay, and
	// category overlay appear as one generation to planner readers.
	registryMu sync.RWMutex
}

func NewPublicationService(config PublicationConfig) (*PublicationService, error) {
	if len(config.Definitions) == 0 || config.Store == nil || config.Files == nil || len(config.Targets) == 0 || len(config.Sources) == 0 || config.Clock == nil || config.TargetRevision == "" {
		return nil, fmt.Errorf("invalid publication composition")
	}
	if err := config.Renderers.Validate(); err != nil {
		return nil, fmt.Errorf("invalid publication composition: %w", err)
	}
	// Every selectable target must resolve to a registered renderer at
	// composition time, so a request can never discover a missing format.
	for id, target := range config.Targets {
		if id != target.ID || domain.ValidateSlug(id) != nil || target.FormatKey == "" || target.RendererID == "" {
			return nil, fmt.Errorf("invalid publication composition: target %q", id)
		}
		if _, err := config.Renderers.For(target); err != nil {
			return nil, fmt.Errorf("invalid publication composition: %w", err)
		}
	}
	// A category naming a list this composition does not carry would resolve
	// to a silently smaller profile at build time. It is refused here instead.
	for id, category := range config.Categories {
		if id != category.ID || domain.ValidateSlug(id) != nil || len(category.Lists) == 0 {
			return nil, fmt.Errorf("invalid publication composition: category %q", id)
		}
		for _, listID := range category.Lists {
			if _, known := config.Definitions[listID]; !known {
				return nil, fmt.Errorf("invalid publication composition: category %q names unknown list %q", id, listID)
			}
		}
	}
	if config.Entropy == nil {
		config.Entropy = rand.Reader
	}
	// One catalog, one revision: the planner refuses a plan whose definitions
	// disagree, so a composition that already disagrees is refused here with a
	// name instead of failing at the first build.
	catalogRevision := ""
	for id, definition := range config.Definitions {
		if definition.CatalogRevision == "" || (catalogRevision != "" && definition.CatalogRevision != catalogRevision) {
			return nil, fmt.Errorf("invalid publication composition: list %q catalog revision", id)
		}
		catalogRevision = definition.CatalogRevision
	}
	for id := range config.LocalListIDs {
		if _, ok := config.Definitions[id]; !ok {
			return nil, fmt.Errorf("invalid publication composition: local list %q", id)
		}
	}
	return &PublicationService{
		config:          config,
		catalogRevision: catalogRevision,
		custom:          customListRegistry{lists: map[string]CustomList{}},
		tuning:          listTuningRegistry{byID: map[string]ListTuning{}},
		overlay:         categoryRegistry{custom: map[string]CustomCategory{}, membership: map[string]map[string]MembershipState{}, removed: emptyRemovalIndex()},
	}, nil
}

// target resolves one selectable target and the renderer that implements it.
func (s *PublicationService) target(id string) (domain.TargetDefinition, Renderer, error) {
	target, ok := s.config.Targets[id]
	if !ok {
		return domain.TargetDefinition{}, nil, ErrNotFound
	}
	renderer, err := s.config.Renderers.For(target)
	if err != nil {
		return domain.TargetDefinition{}, nil, err
	}
	return target, renderer, nil
}

// maxObjectNameRunes bounds a stored profile name. The store enforces the same
// bound; this one exists so a request is refused before it reaches SQLite.
const maxObjectNameRunes = 120

// maxCompositionItems bounds each stored part of a composition independently.
// The resolved set is bounded by the catalog, which is itself bounded.
const maxCompositionItems = 128

// A domain override is deliberately smaller than the request body limit. It
// is an operator-authored correction to one catalog list, not another feed
// import surface.
const (
	maxDomainsPerList     = 64
	maxProfileListDomains = 512
)

// CreatedOutput is the newly bound format. Subscription issuance is delayed
// until its first successful publication, so a failed initial build cannot
// burn the only copy of the bearer URL (ADR 0004).
type CreatedOutput struct{ Output Output }

// validComposition normalizes and checks a requested composition against the
// catalog. A composition that names an unknown list or category is refused
// rather than silently reduced, because a reduced profile would publish less than
// it says. It must resolve to at least one list: an empty profile has nothing
// to render and would publish an empty file under a name that promises content.
func (s *PublicationService) validComposition(requested ProfileComposition) (ProfileComposition, error) {
	lists := domain.StableStrings(requested.Lists)
	categories := domain.StableStrings(requested.Categories)
	exclusions := domain.StableStrings(requested.Exclusions)
	if len(lists) > maxCompositionItems || len(categories) > maxCompositionItems || len(exclusions) > maxCompositionItems {
		return ProfileComposition{}, fmt.Errorf("invalid profile composition")
	}
	for _, id := range lists {
		if domain.ValidateSlug(id) != nil {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
		if !s.hasDefinition(id) {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
	}
	for _, id := range categories {
		if domain.ValidateSlug(id) != nil {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
		if _, ok := s.mergedCategory(id); !ok {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
	}
	named := make(map[string]struct{}, len(lists))
	for _, id := range lists {
		named[id] = struct{}{}
	}
	for _, id := range exclusions {
		if domain.ValidateSlug(id) != nil {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
		if !s.hasDefinition(id) {
			return ProfileComposition{}, fmt.Errorf("invalid profile composition")
		}
		// Naming a list and excluding it is a contradiction, not a
		// preference. The store refuses it too; refusing here names the reason.
		if _, both := named[id]; both {
			return ProfileComposition{}, fmt.Errorf("list %q is both named and excluded", id)
		}
	}
	composition := ProfileComposition{Lists: lists, Categories: categories, Exclusions: exclusions}
	resolved := s.resolveComposition(composition)
	if len(resolved) == 0 {
		return ProfileComposition{}, fmt.Errorf("invalid profile composition")
	}
	listDomains, err := normalizeListDomains(requested.ListDomains, resolved)
	if err != nil {
		return ProfileComposition{}, err
	}
	composition.ListDomains = listDomains
	priority, err := normalizePriority(requested.Priority, resolved)
	if err != nil {
		return ProfileComposition{}, err
	}
	composition.Priority = priority
	return composition, nil
}

// validCompositionWithDefaultPriority gives newly-created or forecast-only
// compositions the current library ordering when the request leaves priority
// empty. Explicit non-empty priority remains profile-local and is validated by
// validComposition unchanged. Existing stored profiles never pass through this
// helper, so a later library reorder cannot rewrite them.
func (s *PublicationService) validCompositionWithDefaultPriority(ctx context.Context, requested ProfileComposition) (ProfileComposition, error) {
	composition, err := s.validComposition(requested)
	if err != nil || len(requested.Priority) != 0 {
		return composition, err
	}
	resolved := s.resolveComposition(ProfileComposition{
		Lists: composition.Lists, Categories: composition.Categories,
		Exclusions: composition.Exclusions,
	})
	global, err := s.DefaultPriority(ctx)
	if err != nil {
		return ProfileComposition{}, err
	}
	priority, err := normalizePriority(filterKnownPriority(global, resolved), resolved)
	if err != nil {
		return ProfileComposition{}, err
	}
	composition.Priority = priority
	return composition, nil
}

// DefaultPriority returns the live library order. Stored ids that no longer
// resolve are dropped, known ids retain their persisted order, and current
// catalog ids not yet stored are appended by category, then uncategorized.
// An older or in-memory repository without the optional seam uses that grouped
// order for the entire catalog.
func (s *PublicationService) DefaultPriority(ctx context.Context) ([]string, error) {
	// The first category in the catalog owns a multi-category list's slot.
	// Uncategorized lists follow the category groups. This is presentation
	// priority only: Lists and plan list identities remain canonical.
	grouped := []string{}
	for _, category := range s.Categories() {
		grouped = append(grouped, category.Lists...)
	}
	available := mergePriority(grouped, s.Lists())
	stored := []string(nil)
	if repo, ok := s.config.Store.(LibraryPriorityRepository); ok {
		var err error
		stored, err = repo.DefaultPriority(ctx)
		if err != nil {
			return nil, fmt.Errorf("read default priority: %w", err)
		}
	}
	return mergePriority(stored, available), nil
}

// SetDefaultPriority replaces the library order transactionally after
// requiring exactly one full permutation of the currently available list
// ids. A stale or missing id is a request error rather than an implicit
// cleanup; removed rows may remain in storage for future catalog recovery.
func (s *PublicationService) SetDefaultPriority(ctx context.Context, priority []string) error {
	available := s.Lists()
	if !validPriorityPermutation(priority, available) {
		return fmt.Errorf("invalid default priority")
	}
	repo, ok := s.config.Store.(LibraryPriorityRepository)
	if !ok {
		return fmt.Errorf("default priority storage unavailable")
	}
	if err := repo.SetDefaultPriority(ctx, append([]string(nil), priority...)); err != nil {
		return fmt.Errorf("write default priority: %w", err)
	}
	return nil
}

func mergePriority(stored, available []string) []string {
	known := make(map[string]struct{}, len(available))
	for _, id := range available {
		known[id] = struct{}{}
	}
	ordered := make([]string, 0, len(available))
	seen := make(map[string]struct{}, len(available))
	for _, id := range stored {
		if _, ok := known[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, id := range available {
		if _, present := seen[id]; present {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered
}

func validPriorityPermutation(priority, available []string) bool {
	if len(priority) != len(available) {
		return false
	}
	known := make(map[string]struct{}, len(available))
	for _, id := range available {
		known[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(priority))
	for _, id := range priority {
		if domain.ValidateSlug(id) != nil {
			return false
		}
		if _, ok := known[id]; !ok {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return len(seen) == len(known)
}

func normalizePriority(requested, resolved []string) ([]string, error) {
	available := make(map[string]struct{}, len(resolved))
	for _, id := range resolved {
		available[id] = struct{}{}
	}
	ordered := make([]string, 0, len(resolved))
	seen := make(map[string]struct{}, len(resolved))
	for _, id := range requested {
		if domain.ValidateSlug(id) != nil {
			return nil, fmt.Errorf("invalid profile priority")
		}
		if _, ok := available[id]; !ok {
			return nil, fmt.Errorf("invalid profile priority")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("invalid profile priority")
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, id := range resolved {
		if _, present := seen[id]; !present {
			ordered = append(ordered, id)
		}
	}
	return ordered, nil
}

// normalizeListDomains validates the editable, profile-local domain set. A key
// with an empty slice is meaningful: it removes the catalog's static domains
// for this profile while leaving dynamic observations in place. An absent key
// means the profile follows the catalog.
func normalizeListDomains(requested map[string][]string, resolved []string) (map[string][]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	if len(requested) > maxCompositionItems {
		return nil, fmt.Errorf("invalid profile list domains")
	}
	available := make(map[string]struct{}, len(resolved))
	for _, listID := range resolved {
		available[listID] = struct{}{}
	}
	normalized := make(map[string][]string, len(requested))
	total := 0
	for listID, values := range requested {
		if domain.ValidateSlug(listID) != nil {
			return nil, fmt.Errorf("invalid profile list domains")
		}
		if _, ok := available[listID]; !ok || len(values) > maxDomainsPerList {
			return nil, fmt.Errorf("invalid profile list domains")
		}
		domains := make([]string, 0, len(values))
		for _, value := range values {
			normalizedDomain, err := domain.NormalizeDomain(value)
			if err != nil {
				return nil, fmt.Errorf("invalid profile list domains")
			}
			domains = append(domains, normalizedDomain)
		}
		domains = domain.StableStrings(domains)
		total += len(domains)
		if total > maxProfileListDomains {
			return nil, fmt.Errorf("invalid profile list domains")
		}
		normalized[listID] = domains
	}
	return normalized, nil
}

// resolveComposition flattens a stored composition into the list set a build
// actually plans: what the profile names, plus everything its categories carry,
// minus what it excluded. Deduplication happens here, so a list reachable
// through two categories is planned and charged to the target's budget once
// (ADR 0016).
func (s *PublicationService) resolveComposition(composition ProfileComposition) []string {
	resolved := make([]string, 0, len(composition.Lists))
	resolved = append(resolved, composition.Lists...)
	for _, categoryID := range composition.Categories {
		category, ok := s.mergedCategory(categoryID)
		if !ok {
			// A category that left the catalog contributes nothing rather than
			// failing the read. The profile reports the reference as gone.
			continue
		}
		resolved = append(resolved, category.Lists...)
	}
	excluded := make(map[string]struct{}, len(composition.Exclusions))
	for _, id := range composition.Exclusions {
		excluded[id] = struct{}{}
	}
	kept := make([]string, 0, len(resolved))
	for _, id := range resolved {
		if _, drop := excluded[id]; drop {
			continue
		}
		// A list that left the catalog cannot be planned, and pretending
		// otherwise would fail deeper with a worse message.
		if !s.hasDefinition(id) {
			continue
		}
		kept = append(kept, id)
	}
	resolved = domain.StableStrings(kept)
	priority, err := normalizePriority(
		filterKnownPriority(composition.Priority, resolved),
		resolved,
	)
	if err != nil {
		return resolved
	}
	return priority
}

// Stored priority may name a list that a live category no longer carries.
// That stale position disappears on read; newly carried lists are appended
// by normalizePriority after every still-relevant position.
func filterKnownPriority(priority, resolved []string) []string {
	available := make(map[string]struct{}, len(resolved))
	for _, id := range resolved {
		available[id] = struct{}{}
	}
	kept := make([]string, 0, len(priority))
	for _, id := range priority {
		if _, ok := available[id]; ok {
			kept = append(kept, id)
		}
	}
	return kept
}

// ResolvedLists is what a profile publishes right now.
func (s *PublicationService) ResolvedLists(profile Profile) []string {
	return s.resolveComposition(ProfileComposition{Lists: profile.Lists, Categories: profile.Categories, Exclusions: profile.Exclusions, Priority: profile.Priority})
}

// MissingCategories names the references a profile holds that the catalog no
// longer supplies. The profile keeps working on what remains and says so, rather
// than shrinking silently.
func (s *PublicationService) MissingCategories(profile Profile) []string {
	missing := make([]string, 0)
	for _, id := range profile.Categories {
		if _, ok := s.mergedCategory(id); !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func validObjectName(name string) (string, bool) {
	return validName(name, maxObjectNameRunes)
}

// validName is the grammar every operator-supplied name passes: trimmed,
// non-empty, and bounded by whatever its own kind allows. The bound is the
// argument because a profile name and a category title are different lengths of
// the same thing, not different rules.
func validName(name string, maxRunes int) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxRunes {
		return "", false
	}
	// A control character would travel into a screen and a log unchanged.
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

func (s *PublicationService) CreateProfile(ctx context.Context, name string, requested ProfileComposition) (Profile, error) {
	cleanName, ok := validObjectName(name)
	if !ok {
		return Profile{}, fmt.Errorf("invalid profile name")
	}
	composition, err := s.validCompositionWithDefaultPriority(ctx, requested)
	if err != nil {
		return Profile{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Profile{}, fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		id, err := randomHex(s.config.Entropy, 16)
		if err != nil {
			return Profile{}, fmt.Errorf("generate profile identity: %w", err)
		}
		profile := Profile{ID: id, Name: cleanName, Lists: composition.Lists, Categories: composition.Categories, Exclusions: composition.Exclusions, ListDomains: composition.ListDomains, Priority: composition.Priority, CreatedAt: now, UpdatedAt: now}
		if err := s.config.Store.CreateProfile(ctx, profile); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return Profile{}, err
		}
		return profile, nil
	}
	return Profile{}, ErrIdentityCollision
}

func (s *PublicationService) Profile(ctx context.Context, id string) (Profile, error) {
	if !isHexID(id) {
		return Profile{}, ErrNotFound
	}
	return s.config.Store.Profile(ctx, id)
}

// UpdateProfile replaces a profile's name and composition. It does not rebuild: the
// caller decides whether the edit is followed by a build, because a rebuild
// reaches the network and a rename does not.
func (s *PublicationService) UpdateProfile(ctx context.Context, id, name string, requested ProfileComposition) (Profile, error) {
	current, err := s.Profile(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if err := current.writable(); err != nil {
		return Profile{}, err
	}
	cleanName, ok := validObjectName(name)
	if !ok {
		return Profile{}, fmt.Errorf("invalid profile name")
	}
	composition, err := s.validComposition(requested)
	if err != nil {
		return Profile{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Profile{}, fmt.Errorf("clock returned zero time")
	}
	// The schedule is not part of a composition edit: it is carried through so
	// renaming a profile never resets when it last refreshed.
	updated := Profile{
		ID: current.ID, Name: cleanName,
		Lists: composition.Lists, Categories: composition.Categories, Exclusions: composition.Exclusions, ListDomains: composition.ListDomains, Priority: composition.Priority,
		RefreshInterval: current.RefreshInterval, LastRefreshedAt: current.LastRefreshedAt, LastRefreshFailed: current.LastRefreshFailed,
		CreatedAt: current.CreatedAt, UpdatedAt: now,
	}
	if err := s.config.Store.UpdateProfile(ctx, updated); err != nil {
		return Profile{}, err
	}
	return updated, nil
}

// AddOutput binds a profile to one format. A profile may hold one output per target;
// asking twice returns the existing one. No subscription is issued here: the
// output has not yet proven it can publish a valid artifact.
func (s *PublicationService) AddOutput(ctx context.Context, profileID, targetID string) (CreatedOutput, error) {
	profile, err := s.Profile(ctx, profileID)
	if err != nil {
		return CreatedOutput{}, err
	}
	if err := profile.writable(); err != nil {
		return CreatedOutput{}, err
	}
	target, renderer, err := s.target(targetID)
	if err != nil {
		return CreatedOutput{}, fmt.Errorf("invalid output request")
	}
	if existing := s.existingOutput(ctx, profile.ID, target.ID); existing.ID != "" {
		return CreatedOutput{Output: existing}, ErrOutputExists
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CreatedOutput{}, fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		outputID, err := randomHex(s.config.Entropy, 16)
		if err != nil {
			return CreatedOutput{}, fmt.Errorf("generate output identity: %w", err)
		}
		output := Output{ID: outputID, ProfileID: profile.ID, TargetID: target.ID, FormatKey: target.FormatKey, RendererID: target.RendererID, RendererVersion: renderer.Version(), TargetRevision: s.config.TargetRevision, CreatedAt: now}
		if err := s.config.Store.CreateOutput(ctx, NewOutput{Output: output}); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			// A request that arrived at the same moment won the format. Answer
			// with what exists rather than with an empty object.
			if errors.Is(err, ErrOutputExists) {
				return CreatedOutput{Output: s.existingOutput(ctx, profile.ID, target.ID)}, ErrOutputExists
			}
			return CreatedOutput{}, err
		}
		return CreatedOutput{Output: output}, nil
	}
	return CreatedOutput{}, ErrIdentityCollision
}

// IssueSubscription creates the bearer credential only for a published
// output. Existing credentials are immutable and never read back.
func (s *PublicationService) IssueSubscription(ctx context.Context, outputID string) (string, error) {
	output, err := s.Output(ctx, outputID)
	if err != nil {
		return "", err
	}
	if output.LatestArtifactID == "" {
		return "", ErrArtifactUnavailable
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return "", fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		tokenID, token, tokenHash, err := issueSubscriptionToken(s.config.Entropy)
		if err != nil {
			return "", err
		}
		if err := s.config.Store.CreateSubscription(ctx, output.ID, tokenID, tokenHash, now); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return "", err
		}
		return token, nil
	}
	return "", ErrIdentityCollision
}

// existingOutput answers with the output this profile already has for a target, or
// a zero value. A read failure reads as absent: the caller is deciding whether
// to report a conflict, and inventing one would be worse than a plain error.
func (s *PublicationService) existingOutput(ctx context.Context, profileID, targetID string) Output {
	outputs, err := s.config.Store.OutputsByProfile(ctx, profileID)
	if err != nil {
		return Output{}
	}
	for _, output := range outputs {
		if output.TargetID == targetID {
			return output
		}
	}
	return Output{}
}

func (s *PublicationService) Output(ctx context.Context, id string) (Output, error) {
	if !isHexID(id) {
		return Output{}, ErrNotFound
	}
	return s.config.Store.Output(ctx, id)
}

func (s *PublicationService) Outputs(ctx context.Context, profileID string) ([]Output, error) {
	if !isHexID(profileID) {
		return nil, ErrNotFound
	}
	return s.config.Store.OutputsByProfile(ctx, profileID)
}

// Refresh re-observes every list of one profile. It is a property of the profile,
// not of an output: two formats of the same lists observe the same names.
func (s *PublicationService) Refresh(ctx context.Context, profileID string) ([]RefreshSummary, error) {
	profile, err := s.Profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if err := profile.writable(); err != nil {
		return nil, err
	}
	lists := s.ResolvedLists(profile)
	summaries := make([]RefreshSummary, 0, len(lists))
	for _, listID := range lists {
		definition, ok := s.definition(listID)
		if !ok {
			return nil, fmt.Errorf("profile list unavailable")
		}
		summary, refreshErr := RefreshList(ctx, definition, domain.RawJSONTargetDefinition(), s.config.Sources, s.config.Store, s.config.Clock)
		summaries = append(summaries, summary)
		// A degraded cycle is not a failed refresh: the stored observations are
		// still usable and the build publishes them with a source_degraded
		// warning. Refusing here would leave the user with no artifact at all.
		if refreshErr != nil && !errors.Is(refreshErr, ErrSourceDegraded) {
			return summaries, refreshErr
		}
	}
	return summaries, nil
}

// TargetDefinition resolves one selectable target. It is what a deployment needs:
// the profile the artifact was built against, not a description of it.
func (s *PublicationService) TargetDefinition(id string) (domain.TargetDefinition, error) {
	target, _, err := s.target(id)
	return target, err
}

// verifyTarget refuses an output whose stored target mapping no longer matches
// the catalog it was created from.
func (s *PublicationService) verifyTarget(output Output) (domain.TargetDefinition, Renderer, error) {
	target, renderer, err := s.target(output.TargetID)
	if err != nil {
		return domain.TargetDefinition{}, nil, ErrTargetChanged
	}
	if output.FormatKey != target.FormatKey || output.RendererID != target.RendererID || output.RendererVersion != renderer.Version() || output.TargetRevision != s.config.TargetRevision {
		return domain.TargetDefinition{}, nil, ErrTargetChanged
	}
	return target, renderer, nil
}

func randomHex(reader io.Reader, bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func isHexID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
