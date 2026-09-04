package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// Service tuning is the operator's standing correction to one service's
// automatic material: a catalog source switched off, an added HTTP feed, and
// per-domain verdicts over what the catalog and the sources keep offering. It
// is global — a service tells one truth to every list that names it — and it
// composes into the effective definition every refresh, preview, and build
// reads through the one definition accessor.

const customSourceIDPrefix = "feed-"

// Bounds. A tuned service stays bounded, but the verdict bound is sized for a
// file import — an operator pasting a routes file states hundreds of
// destinations in one action, not sixty-four.
const (
	maxCustomSourcesPerService = 8
	maxCustomSourcesTotal      = 64
	maxServiceVerdicts         = 2048
	maxVerdictBatch            = 1024
	// maxDestinationLength is the longest destination any of the three shapes
	// can be; it bounds the text a refusal quotes back.
	maxDestinationLength = 253
)

// InvalidDestinationError names the single value that refused a batch. A batch
// is all or nothing, so the operator who pasted a file needs to be told which
// line to fix rather than that something in it was wrong.
type InvalidDestinationError struct{ Value string }

func (e InvalidDestinationError) Error() string {
	return fmt.Sprintf("invalid destination %q", e.Value)
}

// invalidDestination quotes the operator's own text back, bounded, so a
// malformed paste cannot make the refusal larger than the thing refused.
func invalidDestination(value string) error {
	if len(value) > maxDestinationLength {
		value = strings.ToValidUTF8(value[:maxDestinationLength], "")
	}
	return InvalidDestinationError{Value: value}
}

type CustomSource struct {
	ID        string            `json:"id"`
	ServiceID string            `json:"service_id"`
	URL       string            `json:"url"`
	Format    domain.FeedFormat `json:"format"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type DomainVerdict string

const (
	DomainVerdictInclude DomainVerdict = "include"
	DomainVerdictExclude DomainVerdict = "exclude"
	// DomainVerdictAuto removes a standing verdict: the automatic material
	// speaks for itself again.
	DomainVerdictAuto DomainVerdict = "auto"
)

// ServiceTuning is one service's stored corrections, as the repository hands
// them over in one pass.
type ServiceTuning struct {
	DisabledSources []string
	CustomSources   []CustomSource
	Includes        []string
	Excludes        []string
}

type serviceTuningRegistry struct {
	mu   sync.RWMutex
	byID map[string]ServiceTuning
}

// LoadServiceTuning hydrates the registry from the store, exactly like
// LoadCustomServices: one read at composition time, write-through afterwards,
// with the process lock guaranteeing the single writer.
func (s *PublicationService) LoadServiceTuning(ctx context.Context) error {
	stored, err := s.config.Store.ServiceTunings(ctx)
	if err != nil {
		return fmt.Errorf("load service tuning: %w", err)
	}
	normalized := make(map[string]ServiceTuning, len(stored))
	for serviceID, tuning := range stored {
		clean, err := normalizedServiceTuning(serviceID, tuning)
		if err != nil {
			return fmt.Errorf("load service tuning: %w", err)
		}
		normalized[serviceID] = clean
	}
	s.tuning.mu.Lock()
	s.tuning.byID = normalized
	s.tuning.mu.Unlock()
	return nil
}

func (s *PublicationService) serviceTuning(serviceID string) ServiceTuning {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	s.tuning.mu.RLock()
	defer s.tuning.mu.RUnlock()
	return s.tuning.byID[serviceID]
}

// tunedDefinition applies the operator's corrections to a base definition:
// disabled sources leave the source set, added feeds join it, and domain
// verdicts rewrite the static seeds. Because every observation is filtered by
// the source revisions this definition declares, a disabled source's stored
// sightings stop reaching plans without being deleted.
func (s *PublicationService) tunedDefinition(base domain.ServiceDefinition) domain.ServiceDefinition {
	tuning := s.serviceTuning(base.ID)
	if len(tuning.DisabledSources) == 0 && len(tuning.CustomSources) == 0 &&
		len(tuning.Includes) == 0 && len(tuning.Excludes) == 0 {
		return base
	}
	disabled := make(map[string]struct{}, len(tuning.DisabledSources))
	for _, id := range tuning.DisabledSources {
		disabled[id] = struct{}{}
	}
	sources := make([]domain.SourceDefinition, 0, len(base.Sources)+len(tuning.CustomSources))
	for _, source := range base.Sources {
		if _, off := disabled[source.ID]; off {
			continue
		}
		sources = append(sources, source)
	}
	componentID := firstComponentID(base)
	for _, feed := range tuning.CustomSources {
		if _, off := disabled[feed.ID]; off {
			continue
		}
		sources = append(sources, customSourceDefinition(feed, componentID))
	}

	excluded := make(map[string]struct{}, len(tuning.Excludes))
	for _, value := range tuning.Excludes {
		excluded[value] = struct{}{}
	}
	seeds := make([]domain.Seed, 0, len(base.Seeds)+len(tuning.Includes))
	present := make(map[string]struct{}, len(base.Seeds))
	for _, seed := range base.Seeds {
		if _, off := excluded[seed.Value]; off {
			continue
		}
		seeds = append(seeds, seed)
		present[seed.Value] = struct{}{}
	}
	for _, value := range tuning.Includes {
		if _, already := present[value]; already {
			continue
		}
		canonical, kind, err := domain.NormalizeRuleValue(value)
		if err != nil || canonical != value {
			// A stored verdict is validated on load; a value that stopped
			// parsing is dropped from plans rather than poisoning them.
			continue
		}
		seeds = append(seeds, domain.Seed{
			Kind: kind, Value: value, ComponentID: componentID,
			SourceID: "manual:tuning:" + base.ID, SourceClass: domain.SourceManual,
		})
	}
	base.Sources = sources
	base.Seeds = seeds
	return base
}

// customSourceDefinition projects an operator feed into the planner's source
// shape. The revision binds stored observations to this exact URL and decoder,
// so editing by remove-and-add retires the old observations by revision.
func customSourceDefinition(feed CustomSource, componentID string) domain.SourceDefinition {
	return domain.SourceDefinition{
		ID: feed.ID, Type: domain.SourceHTTP, ComponentID: componentID,
		URL: feed.URL, Format: feed.Format, Class: domain.SourceCommunity,
		Revision: customSourceRevision(feed),
	}
}

func customSourceRevision(feed CustomSource) string {
	payload := struct {
		Implementation string            `json:"implementation"`
		ID             string            `json:"id"`
		URL            string            `json:"url"`
		Format         domain.FeedFormat `json:"format"`
	}{"routevane-custom-feed/1", feed.ID, feed.URL, feed.Format}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func firstComponentID(definition domain.ServiceDefinition) string {
	if len(definition.Components) > 0 {
		return definition.Components[0].ID
	}
	return "web"
}

// sourceRevisions derives the observation filter from the effective
// definition: a stored sighting reaches a plan only while its source and
// revision are still declared. This is what makes disabling a source honest —
// nothing is deleted, it just stops being read.
func sourceRevisions(definition domain.ServiceDefinition) map[string]string {
	revisions := make(map[string]string, len(definition.Sources))
	for _, source := range definition.Sources {
		revisions[source.ID] = source.Revision
	}
	return revisions
}

// SetServiceSourceEnabled switches one automatic source of one service on or
// off. Disabling is a standing row; enabling removes it, so the catalog's own
// default is the absence of a correction.
func (s *PublicationService) SetServiceSourceEnabled(ctx context.Context, serviceID, sourceID string, enabled bool) error {
	base, ok := s.baseDefinition(serviceID)
	if !ok {
		return ErrNotFound
	}
	if !s.sourceKnown(base, sourceID) {
		return ErrNotFound
	}
	if err := s.config.Store.SetSourceDisabled(ctx, serviceID, sourceID, !enabled); err != nil {
		return err
	}
	s.tuning.mu.Lock()
	tuning := s.tuning.byID[serviceID]
	kept := make([]string, 0, len(tuning.DisabledSources)+1)
	for _, id := range tuning.DisabledSources {
		if id != sourceID {
			kept = append(kept, id)
		}
	}
	if !enabled {
		kept = append(kept, sourceID)
	}
	slices.Sort(kept)
	tuning.DisabledSources = kept
	s.storeTuningLocked(serviceID, tuning)
	s.tuning.mu.Unlock()
	return nil
}

// AddServiceSource stores one operator HTTP feed for a service. The URL is
// validated by the same boundary the catalog loader applies, injected by the
// composition so this package stays below the network layer.
func (s *PublicationService) AddServiceSource(ctx context.Context, serviceID, url string, format domain.FeedFormat) (CustomSource, error) {
	if _, ok := s.baseDefinition(serviceID); !ok {
		return CustomSource{}, ErrNotFound
	}
	switch format {
	case domain.FeedFormatText, domain.FeedFormatJSON, domain.FeedFormatDomainList:
	default:
		return CustomSource{}, fmt.Errorf("invalid feed format")
	}
	if s.config.FeedURL == nil {
		return CustomSource{}, fmt.Errorf("feed sources are not available")
	}
	if err := s.config.FeedURL(url); err != nil {
		return CustomSource{}, fmt.Errorf("invalid feed URL: %w", err)
	}
	s.tuning.mu.RLock()
	perService := len(s.tuning.byID[serviceID].CustomSources)
	total := 0
	for _, tuning := range s.tuning.byID {
		total += len(tuning.CustomSources)
	}
	s.tuning.mu.RUnlock()
	if perService >= maxCustomSourcesPerService || total >= maxCustomSourcesTotal {
		return CustomSource{}, fmt.Errorf("feed source limit reached")
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return CustomSource{}, fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := randomHex(s.config.Entropy, 8)
		if err != nil {
			return CustomSource{}, fmt.Errorf("generate feed identity: %w", err)
		}
		feed := CustomSource{ID: customSourceIDPrefix + suffix, ServiceID: serviceID, URL: url, Format: format, CreatedAt: now, UpdatedAt: now}
		if err := s.config.Store.CreateCustomSource(ctx, feed); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return CustomSource{}, err
		}
		s.tuning.mu.Lock()
		tuning := s.tuning.byID[serviceID]
		tuning.CustomSources = append(tuning.CustomSources, feed)
		slices.SortFunc(tuning.CustomSources, func(a, b CustomSource) int { return cmp.Compare(a.ID, b.ID) })
		s.storeTuningLocked(serviceID, tuning)
		s.tuning.mu.Unlock()
		return feed, nil
	}
	return CustomSource{}, ErrIdentityCollision
}

// RemoveServiceSource deletes one operator feed. It is configuration, not a
// publication: everything already published stays, and the feed's stored
// observations simply stop being read because no declared revision names them.
func (s *PublicationService) RemoveServiceSource(ctx context.Context, serviceID, sourceID string) error {
	if !strings.HasPrefix(sourceID, customSourceIDPrefix) {
		return ErrNotFound
	}
	s.tuning.mu.RLock()
	owned := false
	for _, feed := range s.tuning.byID[serviceID].CustomSources {
		if feed.ID == sourceID {
			owned = true
		}
	}
	s.tuning.mu.RUnlock()
	if !owned {
		return ErrNotFound
	}
	if err := s.config.Store.RemoveCustomSource(ctx, sourceID); err != nil {
		return err
	}
	s.tuning.mu.Lock()
	tuning := s.tuning.byID[serviceID]
	feeds := make([]CustomSource, 0, len(tuning.CustomSources))
	for _, feed := range tuning.CustomSources {
		if feed.ID != sourceID {
			feeds = append(feeds, feed)
		}
	}
	tuning.CustomSources = feeds
	disabled := make([]string, 0, len(tuning.DisabledSources))
	for _, id := range tuning.DisabledSources {
		if id != sourceID {
			disabled = append(disabled, id)
		}
	}
	tuning.DisabledSources = disabled
	s.storeTuningLocked(serviceID, tuning)
	s.tuning.mu.Unlock()
	return nil
}

// SetServiceValues records the operator's verdict on destinations of one
// service — domains, IP addresses, or networks: include adds them, exclude
// switches them off wherever they come from, auto removes the standing
// verdicts. One call is one action, so a pasted or imported file lands as a
// single bounded batch instead of hundreds of requests.
func (s *PublicationService) SetServiceValues(ctx context.Context, serviceID string, values []string, verdict DomainVerdict) error {
	if _, ok := s.baseDefinition(serviceID); !ok {
		return ErrNotFound
	}
	if verdict != DomainVerdictInclude && verdict != DomainVerdictExclude && verdict != DomainVerdictAuto {
		return fmt.Errorf("invalid destination verdict")
	}
	if len(values) == 0 || len(values) > maxVerdictBatch {
		return fmt.Errorf("a batch carries between 1 and %d destinations", maxVerdictBatch)
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		canonical, _, err := domain.NormalizeRuleValue(value)
		if err != nil {
			return invalidDestination(value)
		}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	if verdict != DomainVerdictAuto {
		s.tuning.mu.RLock()
		current := s.tuning.byID[serviceID]
		s.tuning.mu.RUnlock()
		standing := len(current.Includes) + len(current.Excludes)
		fresh := 0
		for _, value := range normalized {
			if !slices.Contains(current.Includes, value) && !slices.Contains(current.Excludes, value) {
				fresh++
			}
		}
		if standing+fresh > maxServiceVerdicts {
			return fmt.Errorf("destination verdict limit reached")
		}
	}
	if err := s.config.Store.SetDomainVerdicts(ctx, serviceID, normalized, verdict); err != nil {
		return err
	}
	s.tuning.mu.Lock()
	tuning := s.tuning.byID[serviceID]
	for _, value := range normalized {
		tuning.Includes = withoutString(tuning.Includes, value)
		tuning.Excludes = withoutString(tuning.Excludes, value)
	}
	switch verdict {
	case DomainVerdictInclude:
		tuning.Includes = append(tuning.Includes, normalized...)
		slices.Sort(tuning.Includes)
	case DomainVerdictExclude:
		tuning.Excludes = append(tuning.Excludes, normalized...)
		slices.Sort(tuning.Excludes)
	}
	s.storeTuningLocked(serviceID, tuning)
	s.tuning.mu.Unlock()
	return nil
}

// storeTuningLocked writes one service's tuning back, dropping the entry when
// nothing remains so an untouched service stays absent from the registry.
func (s *PublicationService) storeTuningLocked(serviceID string, tuning ServiceTuning) {
	if len(tuning.DisabledSources) == 0 && len(tuning.CustomSources) == 0 &&
		len(tuning.Includes) == 0 && len(tuning.Excludes) == 0 {
		delete(s.tuning.byID, serviceID)
		return
	}
	s.tuning.byID[serviceID] = tuning
}

// baseDefinition answers with the untuned definition: the shipped catalog or
// the operator's custom service, before corrections apply.
//
// It is also the one accessor a list's existence is decided by, so a list the
// operator removed from the library is subtracted here rather than at each of
// the readers below it. A catalog list keeps its shipped definition and simply
// stops resolving; an operator-created one has no row left to resolve.
func (s *PublicationService) baseDefinition(id string) (domain.ServiceDefinition, bool) {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	if s.removedFromLibrary(RemovalService, id) {
		return domain.ServiceDefinition{}, false
	}
	if definition, ok := s.config.Definitions[id]; ok {
		return definition, true
	}
	s.custom.mu.RLock()
	service, ok := s.custom.services[id]
	s.custom.mu.RUnlock()
	if !ok {
		return domain.ServiceDefinition{}, false
	}
	return customServiceDefinition(service, s.catalogRevision), true
}

func (s *PublicationService) sourceKnown(base domain.ServiceDefinition, sourceID string) bool {
	for _, source := range base.Sources {
		if source.ID == sourceID {
			return true
		}
	}
	for _, feed := range s.serviceTuning(base.ID).CustomSources {
		if feed.ID == sourceID {
			return true
		}
	}
	return false
}

func normalizedServiceTuning(serviceID string, tuning ServiceTuning) (ServiceTuning, error) {
	if domain.ValidateSlug(serviceID) != nil {
		return ServiceTuning{}, fmt.Errorf("invalid tuned service %q", serviceID)
	}
	tuning.DisabledSources = domain.StableStrings(tuning.DisabledSources)
	for _, value := range append(append([]string{}, tuning.Includes...), tuning.Excludes...) {
		if canonical, _, err := domain.NormalizeRuleValue(value); err != nil || canonical != value {
			return ServiceTuning{}, fmt.Errorf("invalid stored destination verdict for %q", serviceID)
		}
	}
	tuning.Includes = domain.StableStrings(tuning.Includes)
	tuning.Excludes = domain.StableStrings(tuning.Excludes)
	included := make(map[string]struct{}, len(tuning.Includes))
	for _, value := range tuning.Includes {
		included[value] = struct{}{}
	}
	for _, value := range tuning.Excludes {
		if _, both := included[value]; both {
			return ServiceTuning{}, fmt.Errorf("destination %q is both included and excluded for %q", value, serviceID)
		}
	}
	slices.SortFunc(tuning.CustomSources, func(a, b CustomSource) int { return cmp.Compare(a.ID, b.ID) })
	return tuning, nil
}

func withoutString(values []string, dropped string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != dropped {
			kept = append(kept, value)
		}
	}
	return kept
}
