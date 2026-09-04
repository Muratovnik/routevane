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
	ErrListArchived        = errors.New("list is archived")
)

// List is the unit of storage and of output. Composition is stored as what the
// operator said, not as what it currently resolves to: the services the list
// names itself, the catalog categories it references, and what it excluded from
// them. Resolution happens on read, so a category that gains a service reaches
// every list referencing it without an edit (ADR 0016).
type List struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Services       []string            `json:"services"`
	Categories     []string            `json:"categories"`
	Exclusions     []string            `json:"exclusions"`
	ServiceDomains map[string][]string `json:"service_domains,omitempty"`
	// Priority names the currently resolved services from highest to lowest.
	// Category references remain live; services they gain later are appended by
	// resolution rather than making an old route invalid.
	Priority []string `json:"priority"`
	// RefreshInterval is this list's own scheduling rule, empty when it follows
	// the service-wide default. The two are kept apart on purpose: a list that
	// never disagreed follows the default as it changes.
	RefreshInterval   RefreshInterval `json:"refresh_interval"`
	LastRefreshedAt   time.Time       `json:"last_refreshed_at,omitzero"`
	LastRefreshFailed bool            `json:"last_refresh_failed"`
	// ArchivedAt is when the list left the shelf, zero while it is on it. It is
	// the only stored fact about archival; whether a list is archived is read
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

// ListComposition is a requested composition before it is checked against the
// catalog. It is one argument rather than three positional slices so a caller
// cannot silently swap the excluded set for the named one.
type ListComposition struct {
	Services       []string            `json:"services"`
	Categories     []string            `json:"categories"`
	Exclusions     []string            `json:"exclusions"`
	ServiceDomains map[string][]string `json:"service_domains,omitempty"`
	Priority       []string            `json:"priority"`
}

// Output binds one list to one renderer format. It owns the subscription and
// the chain of published artifacts; the services it publishes come from its
// list, so an edited list is served by the same output rather than a new one.
type Output struct {
	ID       string `json:"id"`
	ListID   string `json:"list_id"`
	TargetID string `json:"target_id"`
	// DeviceID is the one registered destination this output may deliver to.
	// Empty means file/subscription only. The explicit binding prevents a timer
	// from guessing among multiple routes for the same kind of device.
	DeviceID           string    `json:"device_id,omitempty"`
	ProfileKey         string    `json:"profile_key"`
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
	// UpdateListSchedule writes only the scheduling columns. It is separate
	// from UpdateList because a timer must not rewrite a composition, and a
	// composition edit must not reset when the list last refreshed.
	UpdateListSchedule(ctx context.Context, listID string, interval RefreshInterval, lastRefreshedAt time.Time, failed bool, updatedAt time.Time) error
	CreateList(context.Context, List) error
	List(context.Context, string) (List, error)
	// Lists returns stored lists newest first. The repository owns the bound
	// because this transport has no pagination parameters.
	Lists(context.Context) ([]List, error)
	// UpdateList replaces the mutable part of a list: its name and its
	// composition. Identity and creation time are rejected by the store.
	UpdateList(context.Context, List) error
	// SetListArchived writes only when the list left or rejoined the shelf. A
	// zero moment is the restored state, so the column carries both the fact
	// and its date without a second flag to disagree with.
	SetListArchived(ctx context.Context, listID string, archivedAt time.Time, updatedAt time.Time) error
	CreateOutput(context.Context, NewOutput) error
	UpdateOutputDevice(context.Context, string, string) error
	// The custom-service rows are part of the same repository: a composition
	// is validated against the catalog and this registry in one place, so the
	// two must not be able to come from different stores.
	CreateCustomService(context.Context, CustomService) error
	UpdateCustomService(context.Context, CustomService) error
	CustomServices(context.Context) ([]CustomService, error)
	// Categories are operator-owned over the catalog seed (ADR 0028): the
	// categories the operator created and the membership overlay over the
	// shipped ones. They belong to this repository because every reader merges
	// the two with the catalog, and two stores could disagree about what a
	// route expands to.
	CreateCustomCategory(ctx context.Context, category CustomCategory, memberships []CategoryMembership) error
	UpdateCategory(context.Context, CategoryWrite) error
	// RemoveFromLibrary deletes one category or one list, and the lists a
	// category takes with it, as one transaction. A shipped object leaves a
	// removal record and an operator-created one leaves nothing; either way
	// the state that only made sense while the object existed goes with it
	// (ADR 0029).
	RemoveFromLibrary(context.Context, LibraryRemoval) error
	CategoryOverlay(context.Context) (CategoryOverlay, error)
	// Service tuning rows are the operator's standing corrections to a
	// service's automatic material; the same repository holds them so the
	// effective definition composes from one store.
	SetSourceDisabled(ctx context.Context, serviceID, sourceID string, disabled bool) error
	CreateCustomSource(context.Context, CustomSource) error
	RemoveCustomSource(ctx context.Context, id string) error
	// SetDomainVerdicts writes one operator action about several destinations —
	// domains, addresses, networks — as one unit, because a pasted or imported
	// file is one decision rather than one per line.
	SetDomainVerdicts(ctx context.Context, serviceID string, values []string, verdict DomainVerdict) error
	ServiceTunings(context.Context) (map[string]ServiceTuning, error)
	CreateSubscription(ctx context.Context, outputID, tokenID string, tokenHash [32]byte, createdAt time.Time) error
	RecordOutputAttempt(context.Context, OutputAttempt) error
	LatestOutputAttempt(context.Context, string) (OutputAttempt, error)
	Output(context.Context, string) (Output, error)
	// OutputsByList returns one list's outputs in a stable order; Outputs
	// returns every stored output, bounded the same way Lists is.
	OutputsByList(context.Context, string) ([]Output, error)
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
	Definitions map[string]domain.ServiceDefinition
	// LocalServiceIDs identifies definitions loaded from the local catalog.
	// They are usable by this process, but cannot be named by an exported
	// portable configuration because another machine has no matching catalog
	// entry by definition.
	LocalServiceIDs map[string]struct{}
	Target          domain.TargetProfile
	TargetRevision  string
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
	// empty: a catalog with no categories still serves lists that name services
	// directly.
	Categories map[string]domain.CategoryDefinition
	// Targets is the catalog of selectable devices, keyed by target id.
	Targets map[string]domain.TargetProfile
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
	// Custom services are stamped with it too: the planner accepts exactly one
	// revision per plan, and an operator-defined service must compose with the
	// catalog it extends.
	catalogRevision string
	custom          customServiceRegistry
	tuning          serviceTuningRegistry
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
		if id != target.ID || domain.ValidateSlug(id) != nil || target.ProfileKey == "" || target.RendererID == "" {
			return nil, fmt.Errorf("invalid publication composition: target %q", id)
		}
		if _, err := config.Renderers.For(target); err != nil {
			return nil, fmt.Errorf("invalid publication composition: %w", err)
		}
	}
	// A category naming a service this composition does not carry would resolve
	// to a silently smaller list at build time. It is refused here instead.
	for id, category := range config.Categories {
		if id != category.ID || domain.ValidateSlug(id) != nil || len(category.Services) == 0 {
			return nil, fmt.Errorf("invalid publication composition: category %q", id)
		}
		for _, serviceID := range category.Services {
			if _, known := config.Definitions[serviceID]; !known {
				return nil, fmt.Errorf("invalid publication composition: category %q names unknown service %q", id, serviceID)
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
			return nil, fmt.Errorf("invalid publication composition: service %q catalog revision", id)
		}
		catalogRevision = definition.CatalogRevision
	}
	for id := range config.LocalServiceIDs {
		if _, ok := config.Definitions[id]; !ok {
			return nil, fmt.Errorf("invalid publication composition: local service %q", id)
		}
	}
	return &PublicationService{
		config:          config,
		catalogRevision: catalogRevision,
		custom:          customServiceRegistry{services: map[string]CustomService{}},
		tuning:          serviceTuningRegistry{byID: map[string]ServiceTuning{}},
		overlay:         categoryRegistry{custom: map[string]CustomCategory{}, membership: map[string]map[string]MembershipState{}, removed: emptyRemovalIndex()},
	}, nil
}

// target resolves one selectable target and the renderer that implements it.
func (s *PublicationService) target(id string) (domain.TargetProfile, Renderer, error) {
	target, ok := s.config.Targets[id]
	if !ok {
		return domain.TargetProfile{}, nil, ErrNotFound
	}
	renderer, err := s.config.Renderers.For(target)
	if err != nil {
		return domain.TargetProfile{}, nil, err
	}
	return target, renderer, nil
}

// maxListNameRunes bounds a stored list name. The store enforces the same
// bound; this one exists so a request is refused before it reaches SQLite.
const maxListNameRunes = 120

// maxCompositionItems bounds each stored part of a composition independently.
// The resolved set is bounded by the catalog, which is itself bounded.
const maxCompositionItems = 128

// A domain override is deliberately smaller than the request body limit. It
// is an operator-authored correction to one catalog service, not another feed
// import surface.
const (
	maxDomainsPerService = 64
	maxListDomains       = 512
)

// CreatedOutput is the newly bound format. Subscription issuance is delayed
// until its first successful publication, so a failed initial build cannot
// burn the only copy of the bearer URL (ADR 0004).
type CreatedOutput struct{ Output Output }

// validComposition normalizes and checks a requested composition against the
// catalog. A composition that names an unknown service or category is refused
// rather than silently reduced, because a reduced list would publish less than
// it says. It must resolve to at least one service: an empty list has nothing
// to render and would publish an empty file under a name that promises content.
func (s *PublicationService) validComposition(requested ListComposition) (ListComposition, error) {
	services := domain.StableStrings(requested.Services)
	categories := domain.StableStrings(requested.Categories)
	exclusions := domain.StableStrings(requested.Exclusions)
	if len(services) > maxCompositionItems || len(categories) > maxCompositionItems || len(exclusions) > maxCompositionItems {
		return ListComposition{}, fmt.Errorf("invalid list composition")
	}
	for _, id := range services {
		if domain.ValidateSlug(id) != nil {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
		if !s.hasDefinition(id) {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
	}
	for _, id := range categories {
		if domain.ValidateSlug(id) != nil {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
		if _, ok := s.mergedCategory(id); !ok {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
	}
	named := make(map[string]struct{}, len(services))
	for _, id := range services {
		named[id] = struct{}{}
	}
	for _, id := range exclusions {
		if domain.ValidateSlug(id) != nil {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
		if !s.hasDefinition(id) {
			return ListComposition{}, fmt.Errorf("invalid list composition")
		}
		// Naming a service and excluding it is a contradiction, not a
		// preference. The store refuses it too; refusing here names the reason.
		if _, both := named[id]; both {
			return ListComposition{}, fmt.Errorf("service %q is both named and excluded", id)
		}
	}
	composition := ListComposition{Services: services, Categories: categories, Exclusions: exclusions}
	resolved := s.resolveComposition(composition)
	if len(resolved) == 0 {
		return ListComposition{}, fmt.Errorf("invalid list composition")
	}
	serviceDomains, err := normalizeServiceDomains(requested.ServiceDomains, resolved)
	if err != nil {
		return ListComposition{}, err
	}
	composition.ServiceDomains = serviceDomains
	priority, err := normalizePriority(requested.Priority, resolved)
	if err != nil {
		return ListComposition{}, err
	}
	composition.Priority = priority
	return composition, nil
}

// validCompositionWithDefaultPriority gives newly-created or forecast-only
// compositions the current library ordering when the request leaves priority
// empty. Explicit non-empty priority remains route-local and is validated by
// validComposition unchanged. Existing stored routes never pass through this
// helper, so a later library reorder cannot rewrite them.
func (s *PublicationService) validCompositionWithDefaultPriority(ctx context.Context, requested ListComposition) (ListComposition, error) {
	composition, err := s.validComposition(requested)
	if err != nil || len(requested.Priority) != 0 {
		return composition, err
	}
	resolved := s.resolveComposition(ListComposition{
		Services: composition.Services, Categories: composition.Categories,
		Exclusions: composition.Exclusions,
	})
	global, err := s.DefaultPriority(ctx)
	if err != nil {
		return ListComposition{}, err
	}
	priority, err := normalizePriority(filterKnownPriority(global, resolved), resolved)
	if err != nil {
		return ListComposition{}, err
	}
	composition.Priority = priority
	return composition, nil
}

// DefaultPriority returns the live library order. Stored ids that no longer
// resolve are dropped, known ids retain their persisted order, and current
// catalog ids not yet stored are appended in canonical order. An older or
// in-memory repository without the optional seam naturally falls back to the
// same canonical order.
func (s *PublicationService) DefaultPriority(ctx context.Context) ([]string, error) {
	available := s.Services()
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
// requiring exactly one full permutation of the currently available service
// ids. A stale or missing id is a request error rather than an implicit
// cleanup; removed rows may remain in storage for future catalog recovery.
func (s *PublicationService) SetDefaultPriority(ctx context.Context, priority []string) error {
	available := s.Services()
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
			return nil, fmt.Errorf("invalid list priority")
		}
		if _, ok := available[id]; !ok {
			return nil, fmt.Errorf("invalid list priority")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("invalid list priority")
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

// normalizeServiceDomains validates the editable, list-local domain set. A key
// with an empty slice is meaningful: it removes the catalog's static domains
// for this list while leaving dynamic observations in place. An absent key
// means the list follows the catalog.
func normalizeServiceDomains(requested map[string][]string, resolved []string) (map[string][]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	if len(requested) > maxCompositionItems {
		return nil, fmt.Errorf("invalid list service domains")
	}
	available := make(map[string]struct{}, len(resolved))
	for _, serviceID := range resolved {
		available[serviceID] = struct{}{}
	}
	normalized := make(map[string][]string, len(requested))
	total := 0
	for serviceID, values := range requested {
		if domain.ValidateSlug(serviceID) != nil {
			return nil, fmt.Errorf("invalid list service domains")
		}
		if _, ok := available[serviceID]; !ok || len(values) > maxDomainsPerService {
			return nil, fmt.Errorf("invalid list service domains")
		}
		domains := make([]string, 0, len(values))
		for _, value := range values {
			normalizedDomain, err := domain.NormalizeDomain(value)
			if err != nil {
				return nil, fmt.Errorf("invalid list service domains")
			}
			domains = append(domains, normalizedDomain)
		}
		domains = domain.StableStrings(domains)
		total += len(domains)
		if total > maxListDomains {
			return nil, fmt.Errorf("invalid list service domains")
		}
		normalized[serviceID] = domains
	}
	return normalized, nil
}

// resolveComposition flattens a stored composition into the service set a build
// actually plans: what the list names, plus everything its categories carry,
// minus what it excluded. Deduplication happens here, so a service reachable
// through two categories is planned and charged to the target's budget once
// (ADR 0016).
func (s *PublicationService) resolveComposition(composition ListComposition) []string {
	resolved := make([]string, 0, len(composition.Services))
	resolved = append(resolved, composition.Services...)
	for _, categoryID := range composition.Categories {
		category, ok := s.mergedCategory(categoryID)
		if !ok {
			// A category that left the catalog contributes nothing rather than
			// failing the read. The list reports the reference as gone.
			continue
		}
		resolved = append(resolved, category.Services...)
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
		// A service that left the catalog cannot be planned, and pretending
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

// Stored priority may name a service that a live category no longer carries.
// That stale position disappears on read; newly carried services are appended
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

// ResolvedServices is what a list publishes right now.
func (s *PublicationService) ResolvedServices(list List) []string {
	return s.resolveComposition(ListComposition{Services: list.Services, Categories: list.Categories, Exclusions: list.Exclusions, Priority: list.Priority})
}

// MissingCategories names the references a list holds that the catalog no
// longer supplies. The list keeps working on what remains and says so, rather
// than shrinking silently.
func (s *PublicationService) MissingCategories(list List) []string {
	missing := make([]string, 0)
	for _, id := range list.Categories {
		if _, ok := s.mergedCategory(id); !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func validListName(name string) (string, bool) {
	return validName(name, maxListNameRunes)
}

// validName is the grammar every operator-supplied name passes: trimmed,
// non-empty, and bounded by whatever its own kind allows. The bound is the
// argument because a list name and a category title are different lengths of
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

func (s *PublicationService) CreateList(ctx context.Context, name string, requested ListComposition) (List, error) {
	cleanName, ok := validListName(name)
	if !ok {
		return List{}, fmt.Errorf("invalid list name")
	}
	composition, err := s.validCompositionWithDefaultPriority(ctx, requested)
	if err != nil {
		return List{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return List{}, fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		id, err := randomHex(s.config.Entropy, 16)
		if err != nil {
			return List{}, fmt.Errorf("generate list identity: %w", err)
		}
		list := List{ID: id, Name: cleanName, Services: composition.Services, Categories: composition.Categories, Exclusions: composition.Exclusions, ServiceDomains: composition.ServiceDomains, Priority: composition.Priority, CreatedAt: now, UpdatedAt: now}
		if err := s.config.Store.CreateList(ctx, list); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return List{}, err
		}
		return list, nil
	}
	return List{}, ErrIdentityCollision
}

func (s *PublicationService) List(ctx context.Context, id string) (List, error) {
	if !isHexID(id) {
		return List{}, ErrNotFound
	}
	return s.config.Store.List(ctx, id)
}

// UpdateList replaces a list's name and composition. It does not rebuild: the
// caller decides whether the edit is followed by a build, because a rebuild
// reaches the network and a rename does not.
func (s *PublicationService) UpdateList(ctx context.Context, id, name string, requested ListComposition) (List, error) {
	current, err := s.List(ctx, id)
	if err != nil {
		return List{}, err
	}
	if err := current.writable(); err != nil {
		return List{}, err
	}
	cleanName, ok := validListName(name)
	if !ok {
		return List{}, fmt.Errorf("invalid list name")
	}
	composition, err := s.validComposition(requested)
	if err != nil {
		return List{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return List{}, fmt.Errorf("clock returned zero time")
	}
	// The schedule is not part of a composition edit: it is carried through so
	// renaming a list never resets when it last refreshed.
	updated := List{
		ID: current.ID, Name: cleanName,
		Services: composition.Services, Categories: composition.Categories, Exclusions: composition.Exclusions, ServiceDomains: composition.ServiceDomains, Priority: composition.Priority,
		RefreshInterval: current.RefreshInterval, LastRefreshedAt: current.LastRefreshedAt, LastRefreshFailed: current.LastRefreshFailed,
		CreatedAt: current.CreatedAt, UpdatedAt: now,
	}
	if err := s.config.Store.UpdateList(ctx, updated); err != nil {
		return List{}, err
	}
	return updated, nil
}

// AddOutput binds a list to one format. A list may hold one output per target;
// asking twice returns the existing one. No subscription is issued here: the
// output has not yet proven it can publish a valid artifact.
func (s *PublicationService) AddOutput(ctx context.Context, listID, targetID string) (CreatedOutput, error) {
	list, err := s.List(ctx, listID)
	if err != nil {
		return CreatedOutput{}, err
	}
	if err := list.writable(); err != nil {
		return CreatedOutput{}, err
	}
	target, renderer, err := s.target(targetID)
	if err != nil {
		return CreatedOutput{}, fmt.Errorf("invalid output request")
	}
	if existing := s.existingOutput(ctx, list.ID, target.ID); existing.ID != "" {
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
		output := Output{ID: outputID, ListID: list.ID, TargetID: target.ID, ProfileKey: target.ProfileKey, RendererID: target.RendererID, RendererVersion: renderer.Version(), TargetRevision: s.config.TargetRevision, CreatedAt: now}
		if err := s.config.Store.CreateOutput(ctx, NewOutput{Output: output}); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			// A request that arrived at the same moment won the format. Answer
			// with what exists rather than with an empty object.
			if errors.Is(err, ErrOutputExists) {
				return CreatedOutput{Output: s.existingOutput(ctx, list.ID, target.ID)}, ErrOutputExists
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

// existingOutput answers with the output this list already has for a target, or
// a zero value. A read failure reads as absent: the caller is deciding whether
// to report a conflict, and inventing one would be worse than a plain error.
func (s *PublicationService) existingOutput(ctx context.Context, listID, targetID string) Output {
	outputs, err := s.config.Store.OutputsByList(ctx, listID)
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

func (s *PublicationService) Outputs(ctx context.Context, listID string) ([]Output, error) {
	if !isHexID(listID) {
		return nil, ErrNotFound
	}
	return s.config.Store.OutputsByList(ctx, listID)
}

// Refresh re-observes every service of one list. It is a property of the list,
// not of an output: two formats of the same services observe the same names.
func (s *PublicationService) Refresh(ctx context.Context, listID string) ([]RefreshSummary, error) {
	list, err := s.List(ctx, listID)
	if err != nil {
		return nil, err
	}
	if err := list.writable(); err != nil {
		return nil, err
	}
	services := s.ResolvedServices(list)
	summaries := make([]RefreshSummary, 0, len(services))
	for _, serviceID := range services {
		definition, ok := s.definition(serviceID)
		if !ok {
			return nil, fmt.Errorf("list service unavailable")
		}
		summary, refreshErr := RefreshService(ctx, definition, domain.RawJSONTargetProfile(), s.config.Sources, s.config.Store, s.config.Clock)
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

// TargetProfile resolves one selectable target. It is what a deployment needs:
// the profile the artifact was built against, not a description of it.
func (s *PublicationService) TargetProfile(id string) (domain.TargetProfile, error) {
	target, _, err := s.target(id)
	return target, err
}

// verifyTarget refuses an output whose stored target mapping no longer matches
// the catalog it was created from.
func (s *PublicationService) verifyTarget(output Output) (domain.TargetProfile, Renderer, error) {
	target, renderer, err := s.target(output.TargetID)
	if err != nil {
		return domain.TargetProfile{}, nil, ErrTargetChanged
	}
	if output.ProfileKey != target.ProfileKey || output.RendererID != target.RendererID || output.RendererVersion != renderer.Version() || output.TargetRevision != s.config.TargetRevision {
		return domain.TargetProfile{}, nil, ErrTargetChanged
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
