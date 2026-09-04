package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// The schema is one baseline, so an effective profile written straight into it
// must read back through the planning snapshot without any upgrade step.
func TestBaselineSchemaServesEffectiveProfile(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	encoded, _ := json.Marshal(domain.RawJSONTargetProfile())
	if _, err := store.db.Exec(`INSERT INTO effective_profiles(profile_key,service_id,target_id,renderer_id,catalog_revision,config_json,updated_at_ns) VALUES(?,?,?,?,?,?,?)`, "raw-v1", "example", "raw-json", "raw-json", strings.Repeat("c", 64), string(encoded), now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), "example", map[string]string{}, "raw-v1", now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Profile.ServiceID != "example" || !bytes.Equal(snapshot.Profile.ConfigJSON, encoded) {
		t.Fatalf("profile=%#v", snapshot.Profile)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if version, err := ReadUserVersion(context.Background(), root); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

// A list stores what the operator said, not what it resolves to: the named
// services, the referenced categories, exclusions and ownership priority all
// survive a write and a read unchanged.
func TestListCompositionRoundTripsEveryPart(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := application.List{
		ID: "44444444444444444444444444444444", Name: "Дом",
		Services:       []string{"youtube"},
		Categories:     []string{"video", "communication"},
		Exclusions:     []string{"discord"},
		Priority:       []string{"youtube", "telegram"},
		ServiceDomains: map[string][]string{"youtube": {}},
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	stored, err := store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlices(stored.Services, []string{"youtube"}) ||
		!equalStringSlices(stored.Categories, []string{"communication", "video"}) ||
		!equalStringSlices(stored.Exclusions, []string{"discord"}) ||
		!equalStringSlices(stored.Priority, []string{"youtube", "telegram"}) {
		t.Fatalf("stored=%#v", stored)
	}
	if domains, ok := stored.ServiceDomains["youtube"]; !ok || len(domains) != 0 {
		t.Fatalf("empty domain override lost: %#v", stored.ServiceDomains)
	}

	stored.ServiceDomains = map[string][]string{"youtube": {"youtu.be", "youtube.com"}}
	stored.Priority = []string{"telegram", "youtube"}
	stored.UpdatedAt = now.Add(time.Minute)
	if err := store.UpdateList(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	updated, err := store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlices(updated.ServiceDomains["youtube"], []string{"youtu.be", "youtube.com"}) {
		t.Fatalf("updated domain override lost: %#v", updated.ServiceDomains)
	}
	if !equalStringSlices(updated.Priority, []string{"telegram", "youtube"}) {
		t.Fatalf("updated priority lost: %#v", updated.Priority)
	}
}

// Naming a service and excluding it is a contradiction the resolver would have
// to break arbitrarily. The store refuses it rather than picking a winner.
func TestListRefusesAServiceThatIsBothNamedAndExcluded(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := application.List{
		ID: "55555555555555555555555555555555", Name: "Противоречие",
		Services: []string{"youtube"}, Exclusions: []string{"youtube"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateList(context.Background(), list); err == nil {
		t.Fatal("expected the store to refuse a contradictory composition")
	}
}

// A list that names nothing at all would publish an empty file under a name
// that promises content.
func TestListRefusesAnEmptyComposition(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := application.List{ID: "66666666666666666666666666666666", Name: "Пусто", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateList(context.Background(), list); err == nil {
		t.Fatal("expected the store to refuse an empty composition")
	}
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestPublicationRowsAreImmutableAndPointersAdvanceAtomically(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list, output := publicationListAndOutput("11111111111111111111111111111111", "99999999999999999999999999999999", now)
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("rv1.token.secret"))
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: output, TokenID: "22222222222222222222222222222222", TokenHash: tokenHash}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OutputBySubscription(context.Background(), "22222222222222222222222222222222", sha256.Sum256([]byte("wrong"))); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("wrong secret err=%v", err)
	}
	a := publicationCandidate(output.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", []byte(`{"cutoff":1}`), "a", now)
	result, firstSnapshot, firstArtifact, err := store.Publish(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if result.LatestArtifactID != firstArtifact.ID || result.PreviousArtifactID != "" {
		t.Fatalf("output=%#v", result)
	}
	b := publicationCandidate(output.ID, "cccccccccccccccccccccccccccccccc", "dddddddddddddddddddddddddddddddd", []byte(`{"cutoff":2}`), "b", now.Add(time.Hour))
	// Same semantic hash, distinct exact snapshot JSON and artifact.
	b.Snapshot.RoutingPlanHash = a.Snapshot.RoutingPlanHash
	result, secondSnapshot, secondArtifact, err := store.Publish(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if firstSnapshot.ID == secondSnapshot.ID || result.LatestArtifactID != secondArtifact.ID || result.PreviousArtifactID != firstArtifact.ID {
		t.Fatalf("publication=%#v %#v %#v", result, firstSnapshot, secondSnapshot)
	}
	if _, err := store.db.Exec(`UPDATE plan_snapshots SET status='valid' WHERE id=?`, firstSnapshot.ID); err == nil {
		t.Fatal("snapshot update succeeded")
	}
	if _, err := store.db.Exec(`DELETE FROM artifact_builds WHERE id=?`, firstArtifact.ID); err == nil {
		t.Fatal("artifact delete succeeded")
	}
	if snapshots, artifacts, err := store.PublicationCounts(context.Background()); err != nil || snapshots != 2 || artifacts != 2 {
		t.Fatalf("counts=%d/%d err=%v", snapshots, artifacts, err)
	}
}

func TestSamePayloadRetainsEarliestContentCreationTime(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list, output := publicationListAndOutput("11111111111111111111111111111111", "99999999999999999999999999999999", now)
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("token"))
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: output, TokenID: "22222222222222222222222222222222", TokenHash: h}); err != nil {
		t.Fatal(err)
	}
	first := publicationCandidate(output.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", []byte(`{"cutoff":1}`), "a", now)
	_, _, a, err := store.Publish(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	second := publicationCandidate(output.ID, "cccccccccccccccccccccccccccccccc", "dddddddddddddddddddddddddddddddd", []byte(`{"cutoff":2}`), "a", now.Add(time.Hour))
	_, _, b, err := store.Publish(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if !a.ContentCreatedAt.Equal(b.ContentCreatedAt) {
		t.Fatalf("content time changed: %s != %s", a.ContentCreatedAt, b.ContentCreatedAt)
	}
}

func TestPointerFaultRollsBackSnapshotBuildAndLatest(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list, output := publicationListAndOutput(strings.Repeat("1", 32), strings.Repeat("9", 32), now)
	token := sha256.Sum256([]byte("token"))
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: output, TokenID: strings.Repeat("2", 32), TokenHash: token}); err != nil {
		t.Fatal(err)
	}
	a := publicationCandidate(output.ID, strings.Repeat("a", 32), strings.Repeat("b", 32), []byte(`{"cutoff":1}`), "a", now)
	result, _, artifact, err := store.Publish(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TEMP TRIGGER reject_pointer BEFORE UPDATE OF latest_artifact_id ON outputs BEGIN SELECT RAISE(ABORT,'injected pointer fault'); END`); err != nil {
		t.Fatal(err)
	}
	b := publicationCandidate(output.ID, strings.Repeat("c", 32), strings.Repeat("d", 32), []byte(`{"cutoff":2}`), "b", now.Add(time.Hour))
	if _, _, _, err := store.Publish(context.Background(), b); err == nil {
		t.Fatal("injected pointer fault did not fail publication")
	}
	if snapshots, builds, err := store.PublicationCounts(context.Background()); err != nil || snapshots != 1 || builds != 1 {
		t.Fatalf("candidate rows leaked: %d/%d err=%v", snapshots, builds, err)
	}
	after, err := store.Output(context.Background(), output.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LatestArtifactID != artifact.ID || after.LatestArtifactID != result.LatestArtifactID || after.PreviousArtifactID != "" {
		t.Fatalf("pointers changed: %#v", after)
	}
}

func TestPublicationHardeningFailureHappensBeforeTransaction(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list, output := publicationListAndOutput(strings.Repeat("1", 32), strings.Repeat("9", 32), now)
	token := sha256.Sum256([]byte("token"))
	injected := errors.New("injected hardening failure")
	store.publicationPreflight = func() error { return injected }
	if err := store.CreateList(context.Background(), list); !errors.Is(err, injected) {
		t.Fatalf("create preflight err=%v", err)
	}
	var lists int
	if err := store.db.QueryRow(`SELECT count(*) FROM lists`).Scan(&lists); err != nil || lists != 0 {
		t.Fatalf("failed create committed lists=%d err=%v", lists, err)
	}
	store.publicationPreflight = nil
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: output, TokenID: strings.Repeat("2", 32), TokenHash: token}); err != nil {
		t.Fatal(err)
	}
	a := publicationCandidate(output.ID, strings.Repeat("a", 32), strings.Repeat("b", 32), []byte(`{"cutoff":1}`), "a", now)
	result, _, artifact, err := store.Publish(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	store.publicationPreflight = func() error { return injected }
	b := publicationCandidate(output.ID, strings.Repeat("c", 32), strings.Repeat("d", 32), []byte(`{"cutoff":2}`), "b", now.Add(time.Hour))
	if _, _, _, err := store.Publish(context.Background(), b); !errors.Is(err, injected) {
		t.Fatalf("publish preflight err=%v", err)
	}
	if snapshots, builds, err := store.PublicationCounts(context.Background()); err != nil || snapshots != 1 || builds != 1 {
		t.Fatalf("preflight failure leaked rows: %d/%d err=%v", snapshots, builds, err)
	}
	after, err := store.Output(context.Background(), output.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LatestArtifactID != artifact.ID || after.LatestArtifactID != result.LatestArtifactID || after.PreviousArtifactID != "" {
		t.Fatalf("preflight failure changed pointers: %#v", after)
	}
}

func TestListsListNewestFirstAndOutputsSurviveTheRoundTrip(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	empty, err := store.Lists(context.Background())
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty listing=%#v err=%v", empty, err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	olderList, olderOutput := publicationListAndOutput(strings.Repeat("1", 32), strings.Repeat("9", 32), now)
	newerList, newerOutput := publicationListAndOutput(strings.Repeat("3", 32), strings.Repeat("8", 32), now.Add(time.Hour))
	token := sha256.Sum256([]byte("token"))
	if err := store.CreateList(context.Background(), olderList); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: olderOutput, TokenID: strings.Repeat("2", 32), TokenHash: token}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateList(context.Background(), newerList); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOutput(context.Background(), application.NewOutput{Output: newerOutput, TokenID: strings.Repeat("4", 32), TokenHash: token}); err != nil {
		t.Fatal(err)
	}
	lists, err := store.Lists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || lists[0].ID != newerList.ID || lists[1].ID != olderList.ID {
		t.Fatalf("listing=%#v", lists)
	}
	if len(lists[0].Services) != 1 || lists[0].Services[0] != "example" {
		t.Fatalf("listing lost list composition: %#v", lists[0])
	}
	// The target and renderer identity moved from the profile to its output;
	// the round trip through the store must not lose it either.
	outputs, err := store.OutputsByList(context.Background(), newerList.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0].TargetID != "keenetic" || outputs[0].RendererID != "keenetic-route-bat" {
		t.Fatalf("output lost target fields: %#v", outputs)
	}
}

func publicationListAndOutput(listID, outputID string, now time.Time) (application.List, application.Output) {
	list := application.List{ID: listID, Name: "example list", Services: []string{"example"}, CreatedAt: now, UpdatedAt: now}
	output := application.Output{ID: outputID, ListID: listID, TargetID: "keenetic", ProfileKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat", RendererVersion: "keenetic-bat-ipv4-v1", TargetRevision: string(make([]byte, 64)), CreatedAt: now}
	return list, output
}
func publicationCandidate(outputID, snapshot, artifact string, jsonBytes []byte, payloadMarker string, now time.Time) application.PublicationCandidate {
	planHash := sha256.Sum256([]byte("plan"))
	artifactHash := sha256.Sum256([]byte(payloadMarker))
	hash := hex.EncodeToString(artifactHash[:])
	return application.PublicationCandidate{Snapshot: application.PlanSnapshotRecord{ID: snapshot, OutputID: outputID, RoutingPlanHash: hex.EncodeToString(planHash[:]), RoutingPlanJSON: jsonBytes, PolicyVersion: "auto-v1", CatalogRevision: string(make([]byte, 64)), ObservationCutoff: now, CreatedAt: now, Status: "valid"}, Artifact: application.ArtifactBuildRecord{ID: artifact, OutputID: outputID, PlanSnapshotID: snapshot, RendererID: "keenetic-route-bat", RendererVersion: "keenetic-bat-ipv4-v1", ArtifactHash: hash, ArtifactPath: "artifacts/published/keenetic-route-bat/" + hash + ".bat", SizeBytes: 1, ContentType: "application/x-bat", ContentCreatedAt: now, ValidationStatus: "valid", Status: "published"}, Attempt: application.OutputAttempt{OutputID: outputID, Status: "success", ProjectedRules: 1, MaximumRules: 1024, ArtifactID: artifact, CompletedAt: now}}
}

// The schedule is stored apart from the composition, so a timer writing when a
// list last refreshed never rewrites what the list contains, and an edit never
// resets the timer.
func TestScheduleAndCompositionAreWrittenIndependently(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := application.List{
		ID: "77777777777777777777777777777777", Name: "Дом",
		Categories: []string{"video"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	stored, err := store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshInterval != application.RefreshDefault || !stored.LastRefreshedAt.IsZero() || stored.LastRefreshFailed {
		t.Fatalf("a new list already carries a schedule: %#v", stored)
	}

	refreshed := now.Add(time.Hour)
	if err := store.UpdateListSchedule(context.Background(), list.ID, application.RefreshDaily, refreshed, true, refreshed); err != nil {
		t.Fatal(err)
	}
	stored, err = store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshInterval != application.RefreshDaily || !stored.LastRefreshedAt.Equal(refreshed) || !stored.LastRefreshFailed {
		t.Fatalf("schedule not stored: %#v", stored)
	}
	if !equalStringSlices(stored.Categories, []string{"video"}) {
		t.Fatalf("the timer rewrote the composition: %#v", stored)
	}

	renamed := stored
	renamed.Name = "Дом и офис"
	renamed.UpdatedAt = refreshed.Add(time.Hour)
	if err := store.UpdateList(context.Background(), renamed); err != nil {
		t.Fatal(err)
	}
	stored, err = store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshInterval != application.RefreshDaily || !stored.LastRefreshedAt.Equal(refreshed) || !stored.LastRefreshFailed {
		t.Fatalf("the edit reset the schedule: %#v", stored)
	}
}

// Archival is stored apart from both the composition and the schedule: the
// column carries the moment the list left the shelf, and nothing else moves
// with it. A restored list keeps the rule and the refresh history it had.
func TestArchivalIsStoredApartFromTheRestOfTheList(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := application.List{
		ID: "88888888888888888888888888888888", Name: "Дача",
		Services: []string{"youtube"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	refreshed := now.Add(time.Hour)
	if err := store.UpdateListSchedule(context.Background(), list.ID, application.RefreshWeekly, refreshed, false, refreshed); err != nil {
		t.Fatal(err)
	}
	stored, err := store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Archived() {
		t.Fatalf("a new list is already archived: %#v", stored)
	}

	archivedAt := refreshed.Add(time.Hour)
	if err := store.SetListArchived(context.Background(), list.ID, archivedAt, archivedAt); err != nil {
		t.Fatal(err)
	}
	stored, err = store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Archived() || !stored.ArchivedAt.Equal(archivedAt) || !stored.UpdatedAt.Equal(archivedAt) {
		t.Fatalf("archival not stored: %#v", stored)
	}
	if stored.RefreshInterval != application.RefreshWeekly || !stored.LastRefreshedAt.Equal(refreshed) {
		t.Fatalf("archiving rewrote the schedule: %#v", stored)
	}
	if !equalStringSlices(stored.Services, []string{"youtube"}) {
		t.Fatalf("archiving rewrote the composition: %#v", stored)
	}

	// The library read carries the state, so the shelf and the archive are one
	// query rather than two.
	listed, err := store.Lists(context.Background())
	if err != nil || len(listed) != 1 || !listed[0].Archived() {
		t.Fatalf("listed = %#v, err = %v", listed, err)
	}

	restoredAt := archivedAt.Add(time.Hour)
	if err := store.SetListArchived(context.Background(), list.ID, time.Time{}, restoredAt); err != nil {
		t.Fatal(err)
	}
	stored, err = store.List(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Archived() || stored.RefreshInterval != application.RefreshWeekly || !stored.LastRefreshedAt.Equal(refreshed) {
		t.Fatalf("restoring did not leave the rest alone: %#v", stored)
	}

	if err := store.SetListArchived(context.Background(), "99999999999999999999999999999999", archivedAt, archivedAt); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("archiving an unknown list = %v", err)
	}
}

// An unset preference is a fact the caller interprets, not an error.
func TestAnUnsetSettingReadsEmpty(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	value, err := store.Setting(context.Background(), application.SettingRefreshInterval)
	if err != nil || value != "" {
		t.Fatalf("value = %q err = %v", value, err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.PutSetting(context.Background(), application.SettingRefreshInterval, "weekly", now); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), application.SettingRefreshInterval, "daily", now); err != nil {
		t.Fatal(err)
	}
	value, err = store.Setting(context.Background(), application.SettingRefreshInterval)
	if err != nil || value != "daily" {
		t.Fatalf("value = %q err = %v", value, err)
	}
}
