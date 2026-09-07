package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

type publicationFakeStore struct {
	collisions     int
	creates        []NewOutput
	tokenID        string
	tokenHash      [32]byte
	attempts       []OutputAttempt
	profile        Profile
	output         Output
	published      []PublicationCandidate
	settings       map[string]string
	globalPriority []string
	custom         map[string]CustomList
	tunings        map[string]ListTuning
	// categories and memberships hold the operator's category overlay the way
	// the real store holds it: a title row per created category, and one
	// verdict row per disagreement with the shipped catalog.
	categories  map[string]CustomCategory
	memberships map[string][]CategoryMembership
	// removals is the removal half of the overlay: what the operator deleted
	// that the catalog ships. An operator-created object never appears here,
	// exactly as in the real store.
	removals []CatalogRemoval
	// removalErr makes the store refuse a deletion, so a test can prove the
	// registry is written after the store rather than beside it.
	removalErr error
	// profiles is the library the reference check reads. profile stays the one a
	// composition test writes; a category test needs several.
	profiles []Profile
	// verdictWrites counts destination-verdict batches that reached storage.
	verdictWrites int
	// observedDomains, keyed by source id, makes ReadPlanningSnapshot answer
	// with domain sightings that respect the caller's revision filter — the
	// same contract the real store honors.
	observedDomains map[string][]string
}

func (s *publicationFakeStore) CreateCustomList(_ context.Context, list CustomList) error {
	if s.custom == nil {
		s.custom = map[string]CustomList{}
	}
	if _, taken := s.custom[list.ID]; taken {
		return ErrIdentityCollision
	}
	s.custom[list.ID] = list
	return nil
}

func (s *publicationFakeStore) UpdateCustomList(_ context.Context, list CustomList) error {
	if _, ok := s.custom[list.ID]; !ok {
		return ErrNotFound
	}
	s.custom[list.ID] = list
	return nil
}

func (s *publicationFakeStore) CustomLists(context.Context) ([]CustomList, error) {
	lists := make([]CustomList, 0, len(s.custom))
	for _, list := range s.custom {
		lists = append(lists, list)
	}
	return lists, nil
}

func (s *publicationFakeStore) CreateCustomCategory(_ context.Context, category CustomCategory, memberships []CategoryMembership) error {
	if s.categories == nil {
		s.categories = map[string]CustomCategory{}
	}
	if _, taken := s.categories[category.ID]; taken {
		return ErrIdentityCollision
	}
	s.categories[category.ID] = category
	s.putMemberships(category.ID, memberships)
	return nil
}

func (s *publicationFakeStore) UpdateCategory(_ context.Context, write CategoryWrite) error {
	if write.Title != "" {
		stored, ok := s.categories[write.CategoryID]
		if !ok {
			return ErrNotFound
		}
		stored.Title, stored.UpdatedAt = write.Title, write.UpdatedAt
		s.categories[write.CategoryID] = stored
	}
	if write.ReplaceMemberships {
		s.putMemberships(write.CategoryID, write.Memberships)
	}
	return nil
}

// RemoveFromLibrary mirrors what the real store does in one transaction: an
// operator-created object loses its own rows, a shipped one gains a removal
// record, and either way the state that only made sense while it existed goes
// with it.
func (s *publicationFakeStore) RemoveFromLibrary(_ context.Context, removal LibraryRemoval) error {
	if s.removalErr != nil {
		return s.removalErr
	}
	lists := removal.Lists
	if removal.Kind == RemovalCategory {
		if strings.HasPrefix(removal.ID, CustomCategoryIDPrefix) {
			if _, ok := s.categories[removal.ID]; !ok {
				return ErrNotFound
			}
			delete(s.categories, removal.ID)
		} else {
			s.recordRemoval(RemovalCategory, removal.ID, removal.RemovedAt)
		}
		delete(s.memberships, removal.ID)
	} else {
		lists = []string{removal.ID}
	}
	for _, listID := range lists {
		if strings.HasPrefix(listID, CustomCategoryIDPrefix) {
			if _, ok := s.custom[listID]; !ok {
				return ErrNotFound
			}
			delete(s.custom, listID)
		} else {
			s.recordRemoval(RemovalList, listID, removal.RemovedAt)
		}
		delete(s.tunings, listID)
		for categoryID, rows := range s.memberships {
			kept := make([]CategoryMembership, 0, len(rows))
			for _, row := range rows {
				if row.ListID != listID {
					kept = append(kept, row)
				}
			}
			s.putMemberships(categoryID, kept)
		}
	}
	return nil
}

func (s *publicationFakeStore) recordRemoval(kind RemovalKind, id string, at time.Time) {
	for _, existing := range s.removals {
		if existing.Kind == kind && existing.ID == id {
			return
		}
	}
	s.removals = append(s.removals, CatalogRemoval{Kind: kind, ID: id, RemovedAt: at})
}

func (s *publicationFakeStore) CategoryOverlay(context.Context) (CategoryOverlay, error) {
	overlay := CategoryOverlay{
		Categories:  make([]CustomCategory, 0, len(s.categories)),
		Memberships: make([]CategoryMembership, 0),
		Removals:    append(make([]CatalogRemoval, 0, len(s.removals)), s.removals...),
	}
	for _, category := range s.categories {
		overlay.Categories = append(overlay.Categories, category)
	}
	for _, rows := range s.memberships {
		overlay.Memberships = append(overlay.Memberships, rows...)
	}
	return overlay, nil
}

func (s *publicationFakeStore) putMemberships(categoryID string, rows []CategoryMembership) {
	if s.memberships == nil {
		s.memberships = map[string][]CategoryMembership{}
	}
	if len(rows) == 0 {
		delete(s.memberships, categoryID)
		return
	}
	s.memberships[categoryID] = append([]CategoryMembership(nil), rows...)
}

func (s *publicationFakeStore) SetSourceDisabled(_ context.Context, listID, sourceID string, disabled bool) error {
	if s.tunings == nil {
		s.tunings = map[string]ListTuning{}
	}
	tuning := s.tunings[listID]
	kept := make([]string, 0, len(tuning.DisabledSources)+1)
	for _, id := range tuning.DisabledSources {
		if id != sourceID {
			kept = append(kept, id)
		}
	}
	if disabled {
		kept = append(kept, sourceID)
	}
	tuning.DisabledSources = kept
	s.tunings[listID] = tuning
	return nil
}

func (s *publicationFakeStore) CreateCustomSource(_ context.Context, source CustomSource) error {
	if s.tunings == nil {
		s.tunings = map[string]ListTuning{}
	}
	for _, tuning := range s.tunings {
		for _, existing := range tuning.CustomSources {
			if existing.ID == source.ID {
				return ErrIdentityCollision
			}
		}
	}
	tuning := s.tunings[source.ListID]
	tuning.CustomSources = append(tuning.CustomSources, source)
	s.tunings[source.ListID] = tuning
	return nil
}

func (s *publicationFakeStore) RemoveCustomSource(_ context.Context, id string) error {
	for listID, tuning := range s.tunings {
		kept := make([]CustomSource, 0, len(tuning.CustomSources))
		found := false
		for _, source := range tuning.CustomSources {
			if source.ID == id {
				found = true
				continue
			}
			kept = append(kept, source)
		}
		if found {
			tuning.CustomSources = kept
			s.tunings[listID] = tuning
			return nil
		}
	}
	return ErrNotFound
}

// SetDomainVerdicts stores the batch the way the real store stores it: one
// action over every value. verdictWrites counts the calls, so a test can prove
// a refused batch never reached storage at all.
func (s *publicationFakeStore) SetDomainVerdicts(_ context.Context, listID string, values []string, verdict DomainVerdict) error {
	if s.tunings == nil {
		s.tunings = map[string]ListTuning{}
	}
	s.verdictWrites++
	tuning := s.tunings[listID]
	for _, value := range values {
		tuning.Includes = withoutString(tuning.Includes, value)
		tuning.Excludes = withoutString(tuning.Excludes, value)
		switch verdict {
		case DomainVerdictInclude:
			tuning.Includes = append(tuning.Includes, value)
		case DomainVerdictExclude:
			tuning.Excludes = append(tuning.Excludes, value)
		}
	}
	s.tunings[listID] = tuning
	return nil
}

func (s *publicationFakeStore) ListTunings(context.Context) (map[string]ListTuning, error) {
	out := make(map[string]ListTuning, len(s.tunings))
	for listID, tuning := range s.tunings {
		out[listID] = tuning
	}
	return out, nil
}

// settings and the schedule are stored the way the real store stores them:
// separately from the composition, so a timer cannot rewrite one by touching
// the other.
func (s *publicationFakeStore) Setting(_ context.Context, key string) (string, error) {
	if s.settings == nil {
		return "", nil
	}
	return s.settings[key], nil
}

func (s *publicationFakeStore) PutSetting(_ context.Context, key, value string, _ time.Time) error {
	if s.settings == nil {
		s.settings = map[string]string{}
	}
	s.settings[key] = value
	return nil
}

func (s *publicationFakeStore) DefaultPriority(context.Context) ([]string, error) {
	return append([]string(nil), s.globalPriority...), nil
}

func (s *publicationFakeStore) SetDefaultPriority(_ context.Context, priority []string) error {
	s.globalPriority = append([]string(nil), priority...)
	return nil
}

func (s *publicationFakeStore) UpdateProfileSchedule(_ context.Context, profileID string, interval RefreshInterval, lastRefreshedAt time.Time, failed bool, updatedAt time.Time) error {
	if s.profile.ID != profileID {
		return ErrNotFound
	}
	s.profile.RefreshInterval = interval
	s.profile.LastRefreshedAt = lastRefreshedAt
	s.profile.LastRefreshFailed = failed
	s.profile.UpdatedAt = updatedAt
	return nil
}

func (s *publicationFakeStore) SetProfileArchived(_ context.Context, profileID string, archivedAt time.Time, updatedAt time.Time) error {
	if s.profile.ID != profileID {
		return ErrNotFound
	}
	s.profile.ArchivedAt = archivedAt
	s.profile.UpdatedAt = updatedAt
	return nil
}

func (s *publicationFakeStore) CreateProfile(_ context.Context, profile Profile) error {
	s.profile = profile
	return nil
}
func (s *publicationFakeStore) Profile(context.Context, string) (Profile, error) {
	return s.profile, nil
}
func (s *publicationFakeStore) Profiles(context.Context) ([]Profile, error) {
	if len(s.profiles) > 0 {
		return append([]Profile(nil), s.profiles...), nil
	}
	if s.profile.ID == "" {
		return []Profile{}, nil
	}
	return []Profile{s.profile}, nil
}
func (s *publicationFakeStore) UpdateProfile(_ context.Context, profile Profile) error {
	s.profile = profile
	return nil
}
func (s *publicationFakeStore) CreateOutput(_ context.Context, c NewOutput) error {
	s.creates = append(s.creates, c)
	if s.collisions > 0 {
		s.collisions--
		return ErrIdentityCollision
	}
	s.output = c.Output
	return nil
}

func (s *publicationFakeStore) UpdateOutputDevice(_ context.Context, outputID, deviceID string) error {
	if s.output.ID == outputID {
		s.output.DeviceID = deviceID
		return nil
	}
	return ErrNotFound
}
func (s *publicationFakeStore) CreateSubscription(_ context.Context, outputID, tokenID string, tokenHash [32]byte, _ time.Time) error {
	if s.tokenID != "" {
		return ErrSubscriptionExists
	}
	if outputID != s.output.ID {
		return ErrNotFound
	}
	s.tokenID, s.tokenHash = tokenID, tokenHash
	return nil
}
func (s *publicationFakeStore) RecordOutputAttempt(_ context.Context, attempt OutputAttempt) error {
	s.attempts = append(s.attempts, attempt)
	return nil
}
func (s *publicationFakeStore) LatestOutputAttempt(context.Context, string) (OutputAttempt, error) {
	if len(s.attempts) == 0 {
		return OutputAttempt{}, ErrNotFound
	}
	return s.attempts[len(s.attempts)-1], nil
}
func (s *publicationFakeStore) Output(context.Context, string) (Output, error) {
	return s.output, nil
}
func (s *publicationFakeStore) OutputsByProfile(context.Context, string) ([]Output, error) {
	if s.output.ID == "" {
		return []Output{}, nil
	}
	return []Output{s.output}, nil
}
func (s *publicationFakeStore) Outputs(context.Context) ([]Output, error) {
	if s.output.ID == "" {
		return []Output{}, nil
	}
	return []Output{s.output}, nil
}
func (s *publicationFakeStore) OutputBySubscription(context.Context, string, [32]byte) (Output, error) {
	return s.output, nil
}
func (s *publicationFakeStore) Publish(_ context.Context, c PublicationCandidate) (Output, PlanSnapshotRecord, ArtifactBuildRecord, error) {
	s.published = append(s.published, c)
	s.output.LatestArtifactID = c.Artifact.ID
	s.attempts = append(s.attempts, c.Attempt)
	return s.output, c.Snapshot, c.Artifact, nil
}
func (*publicationFakeStore) PlanSnapshot(context.Context, string) (PlanSnapshotRecord, error) {
	return PlanSnapshotRecord{}, ErrNotFound
}
func (*publicationFakeStore) ArtifactBuild(context.Context, string) (ArtifactBuildRecord, error) {
	return ArtifactBuildRecord{}, ErrNotFound
}
func (s *publicationFakeStore) ApplySuccess(context.Context, SuccessCycle) error  { return nil }
func (s *publicationFakeStore) RecordFailure(context.Context, FailureCycle) error { return nil }
func (s *publicationFakeStore) ReadPlanningSnapshot(_ context.Context, listID string, activeRevisions map[string]string, _ string, _ time.Time) (PlanningSnapshot, error) {
	raw := domain.RawJSONTargetDefinition()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if s.observedDomains != nil {
		snapshot := PlanningSnapshot{Format: FormatRecord{FormatKey: raw.FormatKey, ListID: listID, TargetID: raw.ID, RendererID: raw.RendererID, CatalogRevision: strings.Repeat("c", 64)}}
		sourceIDs := make([]string, 0, len(s.observedDomains))
		for sourceID := range s.observedDomains {
			sourceIDs = append(sourceIDs, sourceID)
		}
		sort.Strings(sourceIDs)
		for _, sourceID := range sourceIDs {
			revision, active := activeRevisions[sourceID]
			if !active {
				continue
			}
			for _, value := range s.observedDomains[sourceID] {
				resource, err := domain.NewDomainResource(value)
				if err != nil {
					return PlanningSnapshot{}, err
				}
				snapshot.Sightings = append(snapshot.Sightings, domain.Sighting{
					ID: sourceID + ":" + value, ListID: listID, ComponentID: "web", Resource: resource,
					SourceID: sourceID, SourceClass: domain.SourceCommunity, SourceRevision: revision,
					FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), ObservationCount: 1, Validity: domain.ValidityValid,
				})
			}
		}
		return snapshot, nil
	}
	resource, _ := domain.NewAddrResourceFromString("198.51.100.8")
	sourceDomain, _ := domain.NewDomainResource("source.example")
	targetDomain, _ := domain.NewDomainResource("target.example")
	sighting := domain.Sighting{ID: "sighting", ListID: listID, ComponentID: "web", Resource: resource, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: "v1", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), ObservationCount: 1, Metadata: "metadata", Validity: domain.ValidityValid}
	relation := domain.Relation{SourceResource: sourceDomain, RelationType: domain.RelationCNAMETo, TargetResource: targetDomain, ListID: listID, ComponentID: "web", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), SourceID: "dns-main", SourceRevision: "v1", Validity: domain.ValidityValid}
	return PlanningSnapshot{Sightings: []domain.Sighting{sighting}, Relations: []domain.Relation{relation}, Format: FormatRecord{FormatKey: raw.FormatKey, ListID: listID, TargetID: raw.ID, RendererID: raw.RendererID, CatalogRevision: strings.Repeat("c", 64)}}, nil
}

type publicationFakeFiles struct {
	puts int
	err  error
}

func (f *publicationFakeFiles) PutPublished(_ context.Context, _ domain.RendererDescriptor, payload []byte) (PublishedFile, error) {
	f.puts++
	if f.err != nil {
		return PublishedFile{}, f.err
	}
	return PublishedFile{RelativePath: "artifacts/published/keenetic-route-bat/" + strings.Repeat("a", 64) + ".bat", Hash: strings.Repeat("a", 64), Size: int64(len(payload))}, nil
}
func (*publicationFakeFiles) ReadPublished(context.Context, ArtifactBuildRecord, domain.RendererDescriptor) ([]byte, error) {
	return nil, errors.New("unused")
}

type publicationFakeSource struct{}

func (publicationFakeSource) Observe(context.Context, SourceRequest) (SourceResult, error) {
	return SourceResult{}, nil
}

type mutatingRenderer struct{ invalid, mutateProjection, mutateRender bool }

func (mutatingRenderer) ID() string      { return keenetic.ID }
func (mutatingRenderer) Version() string { return keenetic.Version }
func (mutatingRenderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: keenetic.ID, Version: keenetic.Version, ContentType: keenetic.ContentType, FileExtension: keenetic.FileExtension}
}
func (mutatingRenderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}
}

func (r mutatingRenderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	count := len(plan.Rules)
	if r.mutateProjection {
		plan.Lists[0] = "projection-mutated"
		if len(plan.Rules) > 0 {
			plan.Rules[0].ListID = "projection-mutated"
			if len(plan.Rules[0].ReasonCodes) > 0 {
				plan.Rules[0].ReasonCodes[0] = "projection-mutated"
			}
			if len(plan.Rules[0].ProvenanceRefs) > 0 {
				plan.Rules[0].ProvenanceRefs[0] = "projection-mutated"
			}
			if plan.Rules[0].ExpiresAt != nil {
				*plan.Rules[0].ExpiresAt = time.Time{}
			}
		}
		if len(plan.Excluded) > 0 {
			plan.Excluded[0].Outcome = "projection-mutated"
			plan.Excluded[0].Candidate.ListID = "projection-mutated"
		}
		if len(plan.Warnings) > 0 {
			plan.Warnings[0] = "projection-mutated"
		}
		if len(plan.Coverage) > 0 {
			plan.Coverage[0].ListID = "projection-mutated"
		}
		if len(plan.Relations) > 0 {
			plan.Relations[0].ListID = "projection-mutated"
		}
		if len(plan.Sightings) > 0 {
			plan.Sightings[0].ListID = "projection-mutated"
		}
	}
	return count, nil
}
func (r mutatingRenderer) Render(plan domain.RoutingPlan) ([]byte, error) {
	if r.mutateRender {
		plan.Lists[0] = "render-mutated"
		if len(plan.Rules) > 0 {
			plan.Rules[0].ListID = "render-mutated"
		}
	}
	return []byte("payload"), nil
}
func (r mutatingRenderer) Validate([]byte) error {
	if r.invalid {
		return errors.New("invalid")
	}
	return nil
}

func TestOutputTokenIsIssuedOnlyAfterSuccessfulBuild(t *testing.T) {
	store := &publicationFakeStore{collisions: 1}
	// Profile id, two output ids (the first collides), build snapshot/artifact ids,
	// then subscription token id and secret.
	entropyBytes := bytes.Repeat([]byte{0x10}, 16)
	entropyBytes = append(entropyBytes, bytes.Repeat([]byte{0x11}, 16)...)
	entropyBytes = append(entropyBytes, bytes.Repeat([]byte{0x22}, 16)...)
	entropyBytes = append(entropyBytes, bytes.Repeat([]byte{0x33}, 32)...)
	entropyBytes = append(entropyBytes, bytes.Repeat([]byte{0x44}, 48)...)
	entropy := bytes.NewReader(entropyBytes)
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, entropy)
	profile, err := publication.CreateProfile(context.Background(), "example", ProfileComposition{Lists: []string{"example"}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := publication.AddOutput(context.Background(), profile.ID, "keenetic")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.creates) != 2 || store.creates[0].Output.ID == store.creates[1].Output.ID {
		t.Fatalf("collision attempts=%#v", store.creates)
	}
	if created.Output.ID == "" || store.tokenID != "" {
		t.Fatalf("subscription issued before build: %#v", store)
	}
	if _, err := publication.Build(context.Background(), created.Output.ID); err != nil {
		t.Fatal(err)
	}
	token, err := publication.IssueSubscription(context.Background(), created.Output.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, hash, ok := parseToken(token)
	if !ok || len(tokenID) != 32 || hash != store.tokenHash || tokenID != store.tokenID {
		t.Fatalf("token=%q id=%q ok=%v", token, tokenID, ok)
	}
	if strings.Contains(string(store.tokenHash[:]), token) {
		t.Fatal("plaintext token stored")
	}
}

func TestAnOutputBindsOnlyADeviceOfItsOwnTargetAndCanDetachIt(t *testing.T) {
	const outputID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	store := &publicationFakeStore{output: Output{ID: outputID, TargetID: "keenetic"}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	device := Device{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TargetID: "keenetic"}
	bound, err := publication.SetOutputDevice(context.Background(), outputID, &device)
	if err != nil || bound.DeviceID != device.ID || store.output.DeviceID != device.ID {
		t.Fatalf("bound = %#v stored = %#v err = %v", bound, store.output, err)
	}
	foreign := Device{ID: "cccccccccccccccccccccccccccccccc", TargetID: "singbox"}
	if _, err := publication.SetOutputDevice(context.Background(), outputID, &foreign); err == nil || store.output.DeviceID != device.ID {
		t.Fatalf("foreign device changed binding: output = %#v err = %v", store.output, err)
	}
	detached, err := publication.SetOutputDevice(context.Background(), outputID, nil)
	if err != nil || detached.DeviceID != "" || store.output.DeviceID != "" {
		t.Fatalf("detached = %#v stored = %#v err = %v", detached, store.output, err)
	}
}

func TestSnapshotBytesAreFinalBeforeRendererMutationAndInvalidRenderDoesNotPublish(t *testing.T) {
	store := &publicationFakeStore{}
	files := &publicationFakeFiles{}
	publication := newPublicationTestService(t, store, files, mutatingRenderer{mutateProjection: true, mutateRender: true}, bytes.NewReader(bytes.Repeat([]byte{0x21}, 256)))
	profile, output := testProfileAndOutput()
	store.profile, store.output = profile, output
	if _, err := publication.Build(context.Background(), output.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.published) != 1 || !bytes.Contains(store.published[0].Snapshot.RoutingPlanJSON, []byte(`"services":["example"]`)) || bytes.Contains(store.published[0].Snapshot.RoutingPlanJSON, []byte("mutated")) {
		t.Fatalf("snapshot=%s", store.published[0].Snapshot.RoutingPlanJSON)
	}
	cleanStore := &publicationFakeStore{profile: profile, output: output}
	clean := newPublicationTestService(t, cleanStore, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x31}, 256)))
	if _, err := clean.Build(context.Background(), output.ID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(store.published[0].Snapshot.RoutingPlanJSON, cleanStore.published[0].Snapshot.RoutingPlanJSON) || store.published[0].Snapshot.RoutingPlanHash != cleanStore.published[0].Snapshot.RoutingPlanHash {
		t.Fatalf("renderer callback changed planner snapshot/hash")
	}
	badStore := &publicationFakeStore{profile: profile, output: output}
	badFiles := &publicationFakeFiles{}
	bad := newPublicationTestService(t, badStore, badFiles, mutatingRenderer{invalid: true}, bytes.NewReader(bytes.Repeat([]byte{0x22}, 256)))
	if _, err := bad.Build(context.Background(), output.ID); err == nil {
		t.Fatal("invalid renderer published")
	}
	if len(badStore.published) != 0 || badFiles.puts != 0 {
		t.Fatalf("invalid candidate store=%d files=%d", len(badStore.published), badFiles.puts)
	}
	fileStore := &publicationFakeStore{profile: profile, output: output}
	fileFailure := errors.New("publication root identity changed")
	failedFiles := &publicationFakeFiles{err: fileFailure}
	fileFailed := newPublicationTestService(t, fileStore, failedFiles, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x41}, 256)))
	if _, err := fileFailed.Build(context.Background(), output.ID); !errors.Is(err, fileFailure) {
		t.Fatalf("file failure err=%v", err)
	}
	if len(fileStore.published) != 0 || fileStore.output.LatestArtifactID != "" {
		t.Fatalf("file failure advanced publication: %#v", fileStore)
	}
	if len(fileStore.attempts) != 1 || fileStore.attempts[0].Code != BuildFailureStorage {
		t.Fatalf("file failure attempt=%#v", fileStore.attempts)
	}
}

func TestProfileCardsResolveTargetsAndTolerateMissingPieces(t *testing.T) {
	// An output whose latest artifact cannot be read still lists: the fake
	// store refuses every ArtifactBuild call.
	profile, output := testProfileAndOutput()
	output.LatestArtifactID = strings.Repeat("9", 32)
	store := &publicationFakeStore{profile: profile, output: output}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x51}, 256)))
	cards, err := publication.ProfileCards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].ID != profile.ID || len(cards[0].Outputs) != 1 || cards[0].Outputs[0].Latest != nil {
		t.Fatalf("cards = %#v", cards)
	}
	if cards[0].Outputs[0].TargetTitle != "keenetic" || cards[0].Outputs[0].FileExtension != keenetic.FileExtension {
		t.Fatalf("card target = %#v", cards[0].Outputs[0])
	}
	// An output whose target left the catalog keeps its stored identity
	// instead of disappearing or failing the listing.
	goneProfile, goneOutput := testProfileAndOutput()
	goneOutput.TargetID = "gone-device"
	goneStore := &publicationFakeStore{profile: goneProfile, output: goneOutput}
	goneService := newPublicationTestService(t, goneStore, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	cards, err = goneService.ProfileCards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || len(cards[0].Outputs) != 1 {
		t.Fatalf("cards for a vanished target = %#v", cards)
	}
	if cards[0].Outputs[0].TargetTitle != "gone-device" || cards[0].Outputs[0].FileExtension != "" || cards[0].Outputs[0].TargetKind != "" {
		t.Fatalf("cards for a vanished target = %#v", cards[0].Outputs[0])
	}
}

func newPublicationTestService(t *testing.T, store *publicationFakeStore, files *publicationFakeFiles, renderer Renderer, entropy *bytes.Reader) *PublicationService {
	t.Helper()
	definition := domain.ListDefinition{ID: "example", CatalogRevision: strings.Repeat("c", 64), Components: []domain.ComponentDefinition{{ID: "web", Required: true}}, Seeds: []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual}, {Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:domain", SourceClass: domain.SourceManual}}}
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	publication, err := NewPublicationService(PublicationConfig{Definitions: map[string]domain.ListDefinition{"example": definition}, Targets: map[string]domain.TargetDefinition{target.ID: target}, TargetRevision: strings.Repeat("t", 64), FeedURL: func(string) error { return nil }, Store: store, Files: files, Renderers: RendererRegistry{renderer.ID(): renderer}, Sources: SourceRegistry{domain.SourceDNS: publicationFakeSource{}}, Clock: ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }), Entropy: entropy})
	if err != nil {
		t.Fatal(err)
	}
	return publication
}

func TestListDetailsExposeOnlyBoundedCatalogDomains(t *testing.T) {
	publication := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	definition := publication.config.Definitions["example"]
	definition.Title = "Example"
	definition.Seeds = append(definition.Seeds,
		domain.Seed{Kind: domain.RuleDomainExact, Value: "login.example.com", ComponentID: "web"},
		domain.Seed{Kind: domain.RuleDomainSuffix, Value: "login.example.com", ComponentID: "web"},
	)
	definition.Sources = []domain.SourceDefinition{
		{ID: "community", Type: domain.SourceHTTP},
		{ID: "dns", Type: domain.SourceDNS},
	}
	publication.config.Definitions["example"] = definition

	details := publication.ListDetails()
	if len(details) != 1 || details[0].Title != "Example" || details[0].SourceCount != 2 {
		t.Fatalf("details = %#v", details)
	}
	want := []ListDomain{
		{Value: "example.com", IncludeSubdomains: true},
		{Value: "login.example.com", IncludeSubdomains: true},
	}
	if !reflect.DeepEqual(details[0].Domains, want) {
		t.Fatalf("domains = %#v, want %#v", details[0].Domains, want)
	}
	wantSources := []ListSource{{ID: "community", Type: "http"}, {ID: "dns", Type: "dns"}}
	if !reflect.DeepEqual(details[0].Sources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", details[0].Sources, wantSources)
	}
}

func TestProfileListDomainOverridesAreNormalizedAndAppliedOnlyToStaticDomains(t *testing.T) {
	store := &publicationFakeStore{}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 64)))
	profile, err := publication.CreateProfile(context.Background(), "custom domains", ProfileComposition{
		Lists: []string{"example"},
		ListDomains: map[string][]string{
			"example": {"Custom.Example.", "custom.example"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.ListDomains, map[string][]string{"example": {"custom.example"}}) {
		t.Fatalf("normalized domains = %#v", profile.ListDomains)
	}

	prepared, _, err := publication.prepareProfile(context.Background(), profile, domain.RawJSONTargetDefinition(), rawjson.Renderer{})
	if err != nil {
		t.Fatal(err)
	}
	var custom, catalog, staticIP bool
	for _, rule := range prepared.Plan.Rules {
		switch {
		case rule.Kind.IsDomain() && rule.Domain == "custom.example":
			custom = true
		case rule.Kind.IsDomain() && rule.Domain == "example.com":
			catalog = true
		case rule.Addr.String() == "192.0.2.1":
			staticIP = true
		}
	}
	if !custom || catalog || !staticIP {
		t.Fatalf("override leaked outside static domains: rules=%#v", prepared.Plan.Rules)
	}
	definition := publication.config.Definitions["example"]
	definition.Sources = []domain.SourceDefinition{{ID: "dns-main", Type: domain.SourceDNS}}
	overridden := withProfileListDomains(definition, profile.ID, profile.ListDomains["example"])
	if !reflect.DeepEqual(overridden.Sources, definition.Sources) {
		t.Fatalf("override rewrote dynamic sources: %#v", overridden.Sources)
	}

	if _, err := publication.CreateProfile(context.Background(), "foreign override", ProfileComposition{
		Lists:       []string{"example"},
		ListDomains: map[string][]string{"missing": {"missing.example"}},
	}); err == nil {
		t.Fatal("accepted a domain override for a list outside the profile")
	}
}

// testProfileAndOutput returns one profile and the output bound to it, matching the
// composition and target that newPublicationTestService wires up.
func testProfileAndOutput() (Profile, Output) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	profile := Profile{ID: strings.Repeat("1", 32), Name: "example profile", Lists: []string{"example"}, CreatedAt: now, UpdatedAt: now}
	output := Output{ID: strings.Repeat("2", 32), ProfileID: profile.ID, TargetID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, RendererVersion: keenetic.Version, TargetRevision: strings.Repeat("t", 64), CreatedAt: now}
	return profile, output
}

func FuzzParseSubscriptionToken(f *testing.F) {
	f.Add("rv1." + strings.Repeat("a", 32) + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	f.Add("")
	f.Add("rv1.invalid.secret")
	f.Fuzz(func(t *testing.T, value string) {
		id, hash, ok := parseToken(value)
		if ok {
			if !isHexID(id) || hash != sha256.Sum256([]byte(value)) {
				t.Fatalf("accepted token identity mismatch")
			}
		}
	})
}
