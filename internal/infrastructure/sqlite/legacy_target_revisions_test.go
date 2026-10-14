package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

type emptyObservationSource struct{}

func (emptyObservationSource) Observe(context.Context, application.SourceRequest) (application.SourceResult, error) {
	return application.SourceResult{}, nil
}

// An install upgraded from schema 18 holds outputs pinned to the retired hash
// of the whole target catalog, which no longer equals anything the running
// process computes. ADR 0054 moves each one whose format and renderer are still
// the ones it was created with to its own target's revision, keeping the
// output, its subscription and its link; an output whose renderer changed keeps
// failing as before.
func TestUpgradedOutputsKeepBuildingUnderTheirTargetRevision(t *testing.T) {
	const previousVersion = 18
	ctx := context.Background()
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previous := &Store{db: db, path: path}
	if err := previous.initialize(ctx, migrations[:previousVersion]); err == nil {
		t.Fatal("a database short of the current version was accepted")
	}
	profileID := strings.Repeat("1", 32)
	kept, stale, retired := strings.Repeat("3", 32), strings.Repeat("4", 32), strings.Repeat("5", 32)
	tokenID := strings.Repeat("6", 32)
	token := "rv1." + tokenID + "." + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	tokenHash := sha256.Sum256([]byte(token))
	// The value every output of an existing install carries: one hash of the
	// whole catalog, unrelated to any single target.
	legacyRevision := strings.Repeat("e", 64)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES(?,'Дача',1,1)`, []any{profileID}},
		{`INSERT INTO profile_lists(profile_id,list_id) VALUES(?,'example')`, []any{profileID}},
		{`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'keenetic',?,?,?,?,1)`, []any{kept, profileID, keenetic.Version, keenetic.ID, keenetic.Version, legacyRevision}},
		{`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'singbox',?,?,'singbox-retired-renderer',?,2)`, []any{stale, profileID, singbox.Version, singbox.ID, legacyRevision}},
		{`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns,removed_at_ns) VALUES(?,?,'keenetic',?,?,?,?,3,4)`, []any{retired, profileID, keenetic.Version, keenetic.ID, keenetic.Version, legacyRevision}},
		{`INSERT INTO subscriptions(token_id,output_id,token_hash,created_at_ns) VALUES(?,?,?,1)`, []any{tokenID, kept, tokenHash[:]}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("%s: %v", statement.query, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("migrating a version-18 database: %v", err)
	}
	defer store.Close()
	var marked int
	if err := store.db.QueryRow(`SELECT count(*) FROM legacy_target_revisions`).Scan(&marked); err != nil || marked != 2 {
		t.Fatalf("marked outputs=%d err=%v, want the two live ones", marked, err)
	}

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	keeneticTarget := domain.TargetDefinition{ID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Title: "Keenetic", Kind: domain.TargetKindRouter, RendererOptions: []string{},
		Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	singboxTarget := domain.TargetDefinition{ID: "singbox", FormatKey: singbox.Version, RendererID: singbox.ID, Title: "sing-box", Kind: domain.TargetKindApp, RendererOptions: []string{},
		Constraints: domain.TargetConstraints{SupportsDomainExact: true, SupportsDomainSuffix: true, SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true, MaxRules: singbox.MaxEntries, MaxArtifactSize: singbox.MaxArtifactSize}}
	targets := map[string]domain.TargetDefinition{keeneticTarget.ID: keeneticTarget, singboxTarget.ID: singboxTarget}
	renderers := application.RendererRegistry{keenetic.ID: keenetic.Renderer{}, singbox.ID: singbox.Renderer{}}
	definition := domain.ListDefinition{ID: "example", Title: "Example", CatalogRevision: strings.Repeat("c", 64),
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:      []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.10", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual}}}
	publication, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: map[string]domain.ListDefinition{definition.ID: definition}, Targets: targets, Store: store,
		Files: filesystem.PublishedStore{DataRoot: root}, Renderers: renderers,
		Sources: application.SourceRegistry{domain.SourceDNS: emptyObservationSource{}},
		Clock:   application.ClockFunc(func() time.Time { return now }),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := publication.Refresh(ctx, profileID); err != nil {
		t.Fatal(err)
	}
	// Without the rebase the upgraded output is exactly the defect: refused.
	if _, err := publication.Build(ctx, kept); !errors.Is(err, application.ErrTargetChanged) {
		t.Fatalf("before the rebase build err=%v, want ErrTargetChanged", err)
	}

	moved, err := application.RebaseLegacyTargetRevisions(ctx, store, targets, renderers)
	if err != nil || moved != 1 {
		t.Fatalf("rebase moved=%d err=%v, want 1", moved, err)
	}
	output, err := store.Output(ctx, kept)
	if err != nil || output.ID != kept || output.TargetRevision != domain.TargetRevision(keeneticTarget) || output.CreatedAt.UnixNano() != 1 {
		t.Fatalf("rebased output=%#v err=%v", output, err)
	}
	built, err := publication.Build(ctx, kept)
	if err != nil || built.Output.ID != kept {
		t.Fatalf("upgraded output build=%#v err=%v", built.Output, err)
	}
	served, err := publication.Subscription(ctx, token)
	if err != nil || !strings.Contains(string(served.Payload), "192.0.2.10") {
		t.Fatalf("the link issued before the upgrade answered err=%v", err)
	}
	var tokens int
	if err := store.db.QueryRow(`SELECT count(*) FROM subscriptions WHERE output_id=? AND token_id=? AND token_hash=?`, kept, tokenID, tokenHash[:]).Scan(&tokens); err != nil || tokens != 1 {
		t.Fatalf("subscription rows=%d err=%v", tokens, err)
	}

	// An output whose renderer changed keeps its revision and its refusal.
	if _, err := publication.Build(ctx, stale); !errors.Is(err, application.ErrTargetChanged) {
		t.Fatalf("stale renderer build err=%v, want ErrTargetChanged", err)
	}
	if output, err := store.Output(ctx, stale); err != nil || output.TargetRevision != legacyRevision {
		t.Fatalf("stale output=%#v err=%v", output, err)
	}
	var revision string
	if err := store.db.QueryRow(`SELECT target_revision FROM outputs WHERE id=?`, retired).Scan(&revision); err != nil || revision != legacyRevision {
		t.Fatalf("retired output revision=%q err=%v", revision, err)
	}

	// The pass is idempotent: the moved and retired outputs left the set, and
	// only the stale one remains marked.
	if moved, err := application.RebaseLegacyTargetRevisions(ctx, store, targets, renderers); err != nil || moved != 0 {
		t.Fatalf("second rebase moved=%d err=%v", moved, err)
	}
	var remaining string
	if err := store.db.QueryRow(`SELECT group_concat(output_id) FROM legacy_target_revisions`).Scan(&remaining); err != nil || remaining != stale {
		t.Fatalf("still marked=%q err=%v", remaining, err)
	}

	// The revision is immutable again once an output leaves the set, the set
	// takes no new member, and the rest of the identity never moved.
	for name, statement := range map[string]string{
		"a rebased revision":   `UPDATE outputs SET target_revision='x' WHERE id='` + kept + `'`,
		"a new legacy marker":  `INSERT INTO legacy_target_revisions(output_id) VALUES('` + kept + `')`,
		"a marked output's id": `UPDATE outputs SET format_key='x' WHERE id='` + stale + `'`,
	} {
		if _, err := store.db.Exec(statement); err == nil {
			t.Errorf("the schema accepted %s", name)
		}
	}
}

// A failure in the middle of the pass leaves every output and marker as it
// was, so the next start repeats the whole rebase rather than half of it.
func TestTargetRevisionRebaseIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	first, second := f.output, f.other
	// Outputs written under schema 19 are never marked, and the closed set takes
	// no insert. The subject here is the transaction, so this test lifts that
	// trigger to plant two markers on outputs of one format.
	if _, err := f.store.db.Exec(`DROP TRIGGER legacy_target_revisions_closed`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := f.store.db.Exec(`INSERT INTO legacy_target_revisions(output_id) VALUES(?)`, id); err != nil {
			t.Fatal(err)
		}
	}
	// The second update is refused by a trigger planted for this test only.
	if _, err := f.store.db.Exec(`CREATE TRIGGER fail_second_rebase BEFORE UPDATE OF target_revision ON outputs WHEN OLD.id='` + second.ID + `' BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if first.TargetID != second.TargetID || first.FormatKey != second.FormatKey {
		t.Fatal("the fixture outputs are expected to share one format")
	}
	formats := []application.TargetFormat{{TargetID: first.TargetID, FormatKey: first.FormatKey, RendererID: first.RendererID, RendererVersion: first.RendererVersion, Revision: strings.Repeat("9", 64)}}
	if _, err := f.store.RebaseLegacyTargetRevisions(ctx, formats); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("rebase err=%v, want the injected failure", err)
	}
	for _, output := range []application.Output{first, second} {
		stored, err := f.store.Output(ctx, output.ID)
		if err != nil || stored.TargetRevision != output.TargetRevision {
			t.Fatalf("output %s revision=%q err=%v, want %q", output.ID, stored.TargetRevision, err, output.TargetRevision)
		}
	}
	var marked int
	if err := f.store.db.QueryRow(`SELECT count(*) FROM legacy_target_revisions`).Scan(&marked); err != nil || marked != 2 {
		t.Fatalf("markers after a failed rebase=%d err=%v", marked, err)
	}
}
