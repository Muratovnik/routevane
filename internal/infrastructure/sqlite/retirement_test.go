package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

var retirementNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func retirementStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func retirementCount(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return count
}

func retirementToken(id string) (string, [32]byte) {
	return id, sha256.Sum256([]byte("rv1." + id + ".secret"))
}

// A database written before version 17 holds one token per output. The
// rebuild that makes several tokens possible must carry each one across
// unchanged, so a subscriber's link keeps working through the upgrade, and the
// rebuilt tables must refuse exactly what the old one refused.
func TestVersionFifteenSubscriptionsSurviveTheTokenRebuild(t *testing.T) {
	const previousVersion = 15
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previous := &Store{db: db, path: path}
	if err := previous.initialize(context.Background(), migrations[:previousVersion]); err == nil {
		t.Fatal("a database short of the current version was accepted")
	}
	profileID, outputID := strings.Repeat("1", 32), strings.Repeat("3", 32)
	tokenID, tokenHash := retirementToken(strings.Repeat("5", 32))
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES(?,?,1,1)`, []any{profileID, "Legacy"}},
		{`INSERT INTO profile_lists(profile_id,list_id) VALUES(?, 'alpha')`, []any{profileID}},
		{`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'keenetic','keenetic-bat-ipv4-v1','keenetic-route-bat','keenetic-bat-ipv4-v1','legacy',1)`, []any{outputID, profileID}},
		{`INSERT INTO subscriptions(output_id,token_id,token_hash,created_at_ns) VALUES(?,?,?,7)`, []any{outputID, tokenID, tokenHash[:]}},
	} {
		if _, err := db.Exec(statement.sql, statement.args...); err != nil {
			t.Fatalf("%s: %v", statement.sql, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("migrating a version-15 database: %v", err)
	}
	defer store.Close()
	if err := verifySchema(context.Background(), store.db); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.OutputBySubscription(context.Background(), tokenID, tokenHash)
	if err != nil || resolved.ID != outputID {
		t.Fatalf("a token issued before the rebuild stopped resolving: output=%q err=%v", resolved.ID, err)
	}
	var createdAt int64
	var stored []byte
	if err := store.db.QueryRow(`SELECT created_at_ns,token_hash FROM subscriptions WHERE token_id=?`, tokenID).Scan(&createdAt, &stored); err != nil || createdAt != 7 || string(stored) != string(tokenHash[:]) {
		t.Fatalf("the carried token changed: created=%d err=%v", createdAt, err)
	}
	var profile application.Profile
	if profile, err = store.Profile(context.Background(), profileID); err != nil || !slices.Equal(profile.Lists, []string{"alpha"}) {
		t.Fatalf("the migrated profile is not live: %#v err=%v", profile, err)
	}

	// The rebuilt table and its new neighbour are insert-only.
	if _, err := store.db.Exec(`INSERT INTO subscription_revocations(token_id,revoked_at_ns) VALUES(?,1)`, tokenID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE subscriptions SET created_at_ns=8`,
		`DELETE FROM subscriptions`,
		`UPDATE subscription_revocations SET revoked_at_ns=2`,
		`DELETE FROM subscription_revocations`,
	} {
		if _, err := store.db.Exec(statement); err == nil {
			t.Fatalf("%s was accepted", statement)
		}
	}
	if _, err := store.db.Exec(`INSERT INTO subscription_revocations(token_id,revoked_at_ns) VALUES(?,1)`, strings.Repeat("6", 32)); err == nil {
		t.Fatal("a revocation of a token that does not exist was accepted")
	}
	if _, err := store.OutputBySubscription(context.Background(), tokenID, tokenHash); !errors.Is(err, application.ErrSubscriptionRevoked) {
		t.Fatalf("a revoked token err=%v, want %v", err, application.ErrSubscriptionRevoked)
	}
}

type retirementFixture struct {
	store              *Store
	profile, sibling   application.Profile
	output, other      application.Output
	device             application.Device
	tokenID            string
	tokenHash          [32]byte
	snapshot, artifact string
}

// newRetirementFixture stores a published, subscribed, device-bound format
// with ownership history beside a second profile that stays live throughout.
func newRetirementFixture(t *testing.T) retirementFixture {
	t.Helper()
	ctx := context.Background()
	store := retirementStore(t)
	f := retirementFixture{store: store}
	f.profile = application.Profile{ID: strings.Repeat("1", 32), Name: "Дача", Lists: []string{"alpha"}, Categories: []string{"video"}, Exclusions: []string{"gamma"}, Priority: []string{"alpha"}, ListDomains: map[string][]string{"alpha": {}}, CreatedAt: retirementNow, UpdatedAt: retirementNow}
	f.sibling = application.Profile{ID: strings.Repeat("2", 32), Name: "Офис", Lists: []string{"beta"}, Priority: []string{"beta"}, CreatedAt: retirementNow.Add(time.Second), UpdatedAt: retirementNow}
	for _, profile := range []application.Profile{f.profile, f.sibling} {
		if err := store.CreateProfile(ctx, profile); err != nil {
			t.Fatal(err)
		}
	}
	f.device = application.Device{ID: strings.Repeat("d", 32), TargetID: "keenetic", Name: "Router", Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0", CreatedAt: retirementNow, UpdatedAt: retirementNow}
	if err := store.CreateDevice(ctx, f.device); err != nil {
		t.Fatal(err)
	}
	_, f.output = publicationProfileAndOutput(f.profile.ID, strings.Repeat("3", 32), retirementNow)
	f.output.DeviceID = f.device.ID
	_, f.other = publicationProfileAndOutput(f.sibling.ID, strings.Repeat("4", 32), retirementNow)
	for _, output := range []application.Output{f.output, f.other} {
		if err := store.CreateOutput(ctx, application.NewOutput{Output: output}); err != nil {
			t.Fatal(err)
		}
	}
	f.snapshot, f.artifact = strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, _, _, err := store.Publish(ctx, publicationCandidate(f.profile, f.output.ID, f.snapshot, f.artifact, []byte(`{"cutoff":1}`), "retired", retirementNow)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutputAttempt(ctx, application.OutputAttempt{OutputID: f.output.ID, Status: "failed", Code: "source_unavailable", CompletedAt: retirementNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	f.tokenID, f.tokenHash = retirementToken(strings.Repeat("5", 32))
	if err := store.CreateSubscription(ctx, f.output.ID, f.tokenID, f.tokenHash, retirementNow); err != nil {
		t.Fatal(err)
	}
	route := netip.MustParsePrefix("192.0.2.10/32")
	if err := store.ReplaceManagedRouteOwnership(ctx, application.ManagedRouteOwnership{
		Scope:  application.ManagedRouteScope{Endpoint: "http://192.0.2.1", TargetID: "keenetic", Interface: "Wireguard0"},
		Routes: []application.ManagedRoute{{Prefix: route, CreatedByRoutevane: true}},
		Claims: []application.ManagedRouteClaim{{OutputID: f.output.ID, Prefix: route}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceManagedFQDNOwnership(ctx, application.ManagedFQDNOwnership{Endpoint: "http://192.0.2.1", OutputID: f.output.ID, Groups: []application.ManagedFQDNGroup{{Name: "routevane-alpha", Entries: []string{"alpha.example"}, Interface: "Wireguard0", Auto: true}}}); err != nil {
		t.Fatal(err)
	}
	return f
}

// Deleting is the step after archival. It retires the profile and its formats
// from every read, withdraws their subscriptions and bindings, lets the library
// forget what the profile named, and keeps every published and delivered fact.
func TestDeletingAnArchivedProfileRetiresItAndKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store

	if err := store.DeleteProfile(ctx, f.profile.ID, retirementNow.Add(time.Hour)); !errors.Is(err, application.ErrProfileNotArchived) {
		t.Fatalf("deleting a profile on the shelf err=%v", err)
	}
	if _, err := store.Profile(ctx, f.profile.ID); err != nil {
		t.Fatalf("a refused deletion changed the profile: %v", err)
	}
	if err := store.SetProfileArchived(ctx, f.profile.ID, retirementNow.Add(time.Hour), retirementNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	deletedAt := retirementNow.Add(2 * time.Hour)
	if err := store.DeleteProfile(ctx, f.profile.ID, deletedAt); err != nil {
		t.Fatal(err)
	}

	// Gone from every read of the profile, the shelf, the timer and the
	// library's in-use check, while the sibling stays exactly where it was.
	if _, err := store.Profile(ctx, f.profile.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a deleted profile read err=%v", err)
	}
	profiles, err := store.Profiles(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].ID != f.sibling.ID {
		t.Fatalf("profiles=%v err=%v", profiles, err)
	}
	page, err := store.ProfilePage(ctx, application.ProfileShelfAll, "")
	if err != nil || len(page.Profiles) != 1 || page.Profiles[0].ID != f.sibling.ID {
		t.Fatalf("page=%v err=%v", page.Profiles, err)
	}
	// The deleted profile is newer than the sibling, so a cursor naming it
	// still positions the next page instead of failing it.
	if page, err := store.ProfilePage(ctx, application.ProfileShelfAll, f.sibling.ID); err != nil || len(page.Profiles) != 0 {
		t.Fatalf("page after the sibling=%v err=%v", page.Profiles, err)
	}
	if page, err := store.ProfilePage(ctx, application.ProfileShelfAll, f.profile.ID); err != nil || len(page.Profiles) != 0 {
		t.Fatalf("a deleted cursor=%v err=%v", page.Profiles, err)
	}
	scheduled, err := store.ProfilesForScheduling(ctx)
	if err != nil || len(scheduled) != 1 || scheduled[0].ID != f.sibling.ID {
		t.Fatalf("scheduler input=%v err=%v", scheduled, err)
	}
	references, err := store.ProfileReferences(ctx, []string{"video"}, []string{"alpha", "gamma"})
	if err != nil || len(references) != 0 {
		t.Fatalf("a deleted profile still holds library references: %v err=%v", references, err)
	}

	// Its format is gone with it, and so is everything addressed through it.
	if _, err := store.Output(ctx, f.output.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a retired output read err=%v", err)
	}
	if outputs, err := store.OutputsByProfile(ctx, f.profile.ID); err != nil || len(outputs) != 0 {
		t.Fatalf("outputs of a deleted profile=%v err=%v", outputs, err)
	}
	if outputs, err := store.Outputs(ctx); err != nil || len(outputs) != 1 || outputs[0].ID != f.other.ID {
		t.Fatalf("library outputs=%v err=%v", outputs, err)
	}
	if _, err := store.OutputBySubscription(ctx, f.tokenID, f.tokenHash); !errors.Is(err, application.ErrSubscriptionRevoked) {
		t.Fatalf("a deleted profile's subscription err=%v", err)
	}
	if _, err := store.OutputBySubscription(ctx, f.tokenID, sha256.Sum256([]byte("guess"))); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a wrong secret learned the revocation: %v", err)
	}
	if _, err := store.ArtifactBuild(ctx, f.artifact); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a retired artifact read err=%v", err)
	}
	if _, err := store.PlanSnapshot(ctx, f.snapshot); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a retired snapshot read err=%v", err)
	}
	exported, err := store.ExportConfigTransfer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Profiles) != 1 || exported.Profiles[0].Ref != f.sibling.ID || len(exported.Outputs) != 1 || exported.Outputs[0].Ref != f.other.ID {
		t.Fatalf("export carried a retired object: profiles=%v outputs=%v", exported.Profiles, exported.Outputs)
	}

	// The stored facts: tombstones written once, the binding and composition
	// cleared, every token revoked, and every published or delivered row kept.
	for _, check := range []struct {
		name  string
		query string
		want  int
	}{
		{"profile tombstone", `SELECT count(*) FROM profiles WHERE id='` + f.profile.ID + `' AND deleted_at_ns=` + itoa(deletedAt.UnixNano()), 1},
		{"output tombstone", `SELECT count(*) FROM outputs WHERE id='` + f.output.ID + `' AND removed_at_ns=` + itoa(deletedAt.UnixNano()) + ` AND device_id IS NULL`, 1},
		{"sibling untouched", `SELECT count(*) FROM outputs WHERE id='` + f.other.ID + `' AND removed_at_ns=0`, 1},
		{"revocation", `SELECT count(*) FROM subscription_revocations WHERE token_id='` + f.tokenID + `'`, 1},
		{"subscription kept", `SELECT count(*) FROM subscriptions WHERE token_id='` + f.tokenID + `'`, 1},
		{"snapshot kept", `SELECT count(*) FROM plan_snapshots WHERE output_id='` + f.output.ID + `'`, 1},
		{"artifact kept", `SELECT count(*) FROM artifact_builds WHERE output_id='` + f.output.ID + `'`, 1},
		{"attempts kept", `SELECT count(*) FROM output_attempts WHERE output_id='` + f.output.ID + `'`, 2},
		{"route claim kept", `SELECT count(*) FROM managed_route_claims WHERE output_id='` + f.output.ID + `'`, 1},
		{"FQDN ownership kept", `SELECT count(*) FROM managed_fqdn_groups WHERE output_id='` + f.output.ID + `'`, 1},
		{"composition cleared", `SELECT (SELECT count(*) FROM profile_lists WHERE profile_id='` + f.profile.ID + `')
			+ (SELECT count(*) FROM profile_categories WHERE profile_id='` + f.profile.ID + `')
			+ (SELECT count(*) FROM profile_exclusions WHERE profile_id='` + f.profile.ID + `')
			+ (SELECT count(*) FROM profile_list_domains WHERE profile_id='` + f.profile.ID + `')
			+ (SELECT count(*) FROM profile_list_priorities WHERE profile_id='` + f.profile.ID + `')`, 0},
		{"device kept", `SELECT count(*) FROM devices WHERE id='` + f.device.ID + `'`, 1},
	} {
		if got := retirementCount(t, store, check.query); got != check.want {
			t.Errorf("%s: %d, want %d", check.name, got, check.want)
		}
	}

	// Deletion happens once and is final: nothing writes to the retired rows
	// again, through the store or past it.
	if err := store.DeleteProfile(ctx, f.profile.ID, deletedAt.Add(time.Hour)); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a second deletion err=%v", err)
	}
	if err := store.SetProfileArchived(ctx, f.profile.ID, time.Time{}, deletedAt.Add(time.Hour)); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("restoring a deleted profile err=%v", err)
	}
	if err := store.RecordProfileRefreshResult(ctx, f.profile.ID, deletedAt.Add(time.Hour), false); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a refresh result for a deleted profile err=%v", err)
	}
	newOutput := f.output
	newOutput.ID, newOutput.DeviceID = strings.Repeat("7", 32), ""
	if err := store.CreateOutput(ctx, application.NewOutput{Output: newOutput}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a new format on a deleted profile err=%v", err)
	}
	for _, statement := range []string{
		`UPDATE profiles SET name='renamed' WHERE id='` + f.profile.ID + `'`,
		`UPDATE profiles SET deleted_at_ns=0 WHERE id='` + f.profile.ID + `'`,
		`UPDATE outputs SET removed_at_ns=0 WHERE id='` + f.output.ID + `'`,
		`UPDATE outputs SET device_id='` + f.device.ID + `' WHERE id='` + f.output.ID + `'`,
		`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES('` + strings.Repeat("8", 32) + `','` + f.profile.ID + `','singbox','f','r','v','t',1)`,
		`INSERT INTO subscriptions(token_id,output_id,token_hash,created_at_ns) VALUES('` + strings.Repeat("9", 32) + `','` + f.output.ID + `',zeroblob(32),1)`,
	} {
		if _, err := store.db.Exec(statement); err == nil {
			t.Fatalf("the schema accepted a write to a retired row: %s", statement)
		}
	}
	// Forgetting the device the retired format was bound to does not touch it.
	if err := store.DeleteDevice(ctx, f.device.ID); err != nil {
		t.Fatalf("forgetting a device after retirement: %v", err)
	}
}

// The schema refuses a deletion that would leave a format serving, and a
// tombstone on a profile that was never archived.
func TestTheSchemaRefusesAnIncompleteDeletion(t *testing.T) {
	f := newRetirementFixture(t)
	if _, err := f.store.db.Exec(`UPDATE profiles SET deleted_at_ns=5 WHERE id=?`, f.sibling.ID); err == nil {
		t.Fatal("a profile on the shelf was deleted")
	}
	if _, err := f.store.db.Exec(`UPDATE profiles SET archived_at_ns=4 WHERE id=?`, f.sibling.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec(`UPDATE profiles SET deleted_at_ns=5 WHERE id=?`, f.sibling.ID); err == nil {
		t.Fatal("a profile with a live format was deleted")
	}
	if _, err := f.store.db.Exec(`UPDATE outputs SET removed_at_ns=5 WHERE id=?`, f.output.ID); err == nil {
		t.Fatal("a format was removed with its device binding in place")
	}
}

// Removing one format retires that format only. The profile keeps the rest,
// the same format can be bound again as a new output, and an archived profile
// keeps every format it has.
func TestRemovingAFormatRetiresOnlyThatFormat(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store
	second := application.Output{ID: strings.Repeat("6", 32), ProfileID: f.profile.ID, TargetID: "singbox", FormatKey: "singbox-source-json-v1", RendererID: "singbox-ruleset-json", RendererVersion: "v1", TargetRevision: "revision", CreatedAt: retirementNow.Add(time.Second)}
	if err := store.CreateOutput(ctx, application.NewOutput{Output: second}); err != nil {
		t.Fatal(err)
	}

	removedAt := retirementNow.Add(time.Hour)
	if err := store.RemoveOutput(ctx, f.output.ID, removedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Output(ctx, f.output.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a removed format read err=%v", err)
	}
	outputs, err := store.OutputsByProfile(ctx, f.profile.ID)
	if err != nil || len(outputs) != 1 || outputs[0].ID != second.ID {
		t.Fatalf("formats after removal=%v err=%v", outputs, err)
	}
	if _, err := store.OutputBySubscription(ctx, f.tokenID, f.tokenHash); !errors.Is(err, application.ErrSubscriptionRevoked) {
		t.Fatalf("a removed format's subscription err=%v", err)
	}
	if _, err := store.ArtifactBuild(ctx, f.artifact); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a removed format's artifact err=%v", err)
	}
	if got := retirementCount(t, store, `SELECT count(*) FROM outputs WHERE id=? AND removed_at_ns=? AND device_id IS NULL`, f.output.ID, removedAt.UnixNano()); got != 1 {
		t.Fatalf("the removal was not stored as a tombstone without a binding: %d", got)
	}
	if got := retirementCount(t, store, `SELECT count(*) FROM artifact_builds WHERE output_id=?`, f.output.ID); got != 1 {
		t.Fatalf("a removal deleted published history: %d", got)
	}
	if profile, err := store.Profile(ctx, f.profile.ID); err != nil || !slices.Equal(profile.Lists, []string{"alpha"}) {
		t.Fatalf("removing a format changed its profile: %#v err=%v", profile, err)
	}
	if err := store.RemoveOutput(ctx, f.output.ID, removedAt); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a second removal err=%v", err)
	}
	if err := store.UpdateOutputDevice(ctx, f.output.ID, f.device.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("binding a removed format err=%v", err)
	}
	if _, _, _, err := store.Publish(ctx, publicationCandidate(f.profile, f.output.ID, strings.Repeat("c", 32), strings.Repeat("e", 32), []byte(`{"cutoff":2}`), "late", removedAt)); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a build finishing after removal published: %v", err)
	}

	// The format is free again: a new output takes it with a new identity.
	again := f.output
	again.ID, again.DeviceID = strings.Repeat("7", 32), ""
	if err := store.CreateOutput(ctx, application.NewOutput{Output: again}); err != nil {
		t.Fatalf("binding the format again: %v", err)
	}
	duplicate := again
	duplicate.ID = strings.Repeat("8", 32)
	if err := store.CreateOutput(ctx, application.NewOutput{Output: duplicate}); !errors.Is(err, application.ErrOutputExists) {
		t.Fatalf("a second live output for one format err=%v", err)
	}

	// An archived profile is frozen, its formats included.
	if err := store.SetProfileArchived(ctx, f.profile.ID, removedAt, removedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveOutput(ctx, second.ID, removedAt); !errors.Is(err, application.ErrProfileArchived) {
		t.Fatalf("removing a format of an archived profile err=%v", err)
	}
	// Deleting the profile retires every format it still has.
	if err := store.DeleteProfile(ctx, f.profile.ID, removedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := retirementCount(t, store, `SELECT count(*) FROM outputs WHERE profile_id=? AND removed_at_ns=0`, f.profile.ID); got != 0 {
		t.Fatalf("live formats after deletion: %d", got)
	}
	if got := retirementCount(t, store, `SELECT removed_at_ns FROM outputs WHERE id=?`, f.output.ID); int64(got) != removedAt.UnixNano() {
		t.Fatalf("deletion rewrote an earlier removal moment: %d", got)
	}
}

// A reissued link replaces the previous one in one step: the old token is
// revoked, the new one resolves, and only the new token's hash is stored.
func TestReissueRevokesThePreviousTokenAndStoresOnlyItsHash(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store

	if err := store.ReissueSubscription(ctx, f.other.ID, strings.Repeat("a", 32), sha256.Sum256([]byte("x")), retirementNow); !errors.Is(err, application.ErrSubscriptionMissing) {
		t.Fatalf("reissuing a link that was never issued err=%v", err)
	}
	if err := store.ReissueSubscription(ctx, f.output.ID, f.tokenID, sha256.Sum256([]byte("x")), retirementNow); !errors.Is(err, application.ErrIdentityCollision) {
		t.Fatalf("reissuing with a taken token identity err=%v", err)
	}

	nextID, nextHash := retirementToken(strings.Repeat("c", 32))
	if err := store.ReissueSubscription(ctx, f.output.ID, nextID, nextHash, retirementNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OutputBySubscription(ctx, f.tokenID, f.tokenHash); !errors.Is(err, application.ErrSubscriptionRevoked) {
		t.Fatalf("the previous token err=%v", err)
	}
	if output, err := store.OutputBySubscription(ctx, nextID, nextHash); err != nil || output.ID != f.output.ID {
		t.Fatalf("the new token resolved to %q err=%v", output.ID, err)
	}
	var stored []byte
	if err := store.db.QueryRow(`SELECT token_hash FROM subscriptions WHERE token_id=?`, nextID).Scan(&stored); err != nil || string(stored) != string(nextHash[:]) {
		t.Fatalf("stored hash err=%v", err)
	}
	if got := retirementCount(t, store, `SELECT count(*) FROM subscriptions WHERE output_id=? AND token_id NOT IN (SELECT token_id FROM subscription_revocations)`, f.output.ID); got != 1 {
		t.Fatalf("active tokens after reissue: %d", got)
	}
	// A build after the reissue does not issue a third link.
	if err := store.CreateSubscription(ctx, f.output.ID, strings.Repeat("e", 32), sha256.Sum256([]byte("y")), retirementNow); !errors.Is(err, application.ErrSubscriptionExists) {
		t.Fatalf("a subscription beside an active reissued one err=%v", err)
	}

	if err := store.SetProfileArchived(ctx, f.profile.ID, retirementNow, retirementNow); err != nil {
		t.Fatal(err)
	}
	if err := store.ReissueSubscription(ctx, f.output.ID, strings.Repeat("f", 32), sha256.Sum256([]byte("z")), retirementNow); !errors.Is(err, application.ErrProfileArchived) {
		t.Fatalf("reissuing for an archived profile err=%v", err)
	}
	if output, err := store.OutputBySubscription(ctx, nextID, nextHash); err != nil || output.ID != f.output.ID {
		t.Fatalf("a refused reissue disturbed the active link: %v", err)
	}
	if err := store.SetProfileArchived(ctx, f.profile.ID, time.Time{}, retirementNow); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveOutput(ctx, f.output.ID, retirementNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReissueSubscription(ctx, f.output.ID, strings.Repeat("f", 32), sha256.Sum256([]byte("z")), retirementNow); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("reissuing for a removed format err=%v", err)
	}
	if _, err := store.OutputBySubscription(ctx, nextID, nextHash); !errors.Is(err, application.ErrSubscriptionRevoked) {
		t.Fatalf("the reissued token outlived its format: %v", err)
	}
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }
