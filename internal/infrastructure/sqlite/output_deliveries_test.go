package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func deliveryAttempt(id, artifactID, deviceID string) application.DeploymentAttempt {
	return application.DeploymentAttempt{
		ID: id, ArtifactID: artifactID, DeviceID: deviceID,
		RequestHash: strings.Repeat("e", 64),
		Status:      application.DeploymentAttemptPending, StartedAt: retirementNow,
	}
}

// recordSend stores one attempt through the journal the application uses and
// finishes it at completedAt: succeeded, or failed when failure is not empty.
func recordSend(t *testing.T, store *Store, id, artifactID, deviceID string, completedAt time.Time, failure string) {
	t.Helper()
	ctx := context.Background()
	pending := deliveryAttempt(id, artifactID, deviceID)
	if _, created, err := store.BeginDeploymentAttempt(ctx, pending); err != nil || !created {
		t.Fatalf("begin %s: created=%t err=%v", id, created, err)
	}
	if failure == "pending" {
		return
	}
	completed := pending
	completed.CompletedAt = completedAt
	if failure == "" {
		completed.Status, completed.Result = application.DeploymentAttemptSucceeded, application.DeployResult{ArtifactID: artifactID, Applied: true}
	} else {
		completed.Status, completed.Error = application.DeploymentAttemptFailed, failure
	}
	if err := store.CompleteDeploymentAttempt(ctx, completed); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func publishAnother(t *testing.T, f retirementFixture, profile application.Profile, outputID, snapshot, artifact, marker string) {
	t.Helper()
	if _, _, _, err := f.store.Publish(context.Background(), publicationCandidate(profile, outputID, snapshot, artifact, []byte(`{"cutoff":`+marker+`}`), marker, retirementNow.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
}

func attemptID(n string) string { return strings.Repeat(n, 32) }

// A format's delivery is the newest succeeded send of any of its files to the
// saved connection the format is bound to now. Everything else a deployment
// attempt can be is not one.
func TestLatestOutputDeliveryIsTheNewestSuccessfulSendToTheBoundConnection(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store
	second := strings.Repeat("c", 32)
	publishAnother(t, f, f.profile, f.output.ID, strings.Repeat("9", 32), second, "2")
	otherArtifact := strings.Repeat("e", 32)
	publishAnother(t, f, f.sibling, f.other.ID, strings.Repeat("8", 32), otherArtifact, "3")
	deviceA := f.device.ID
	deviceB := application.Device{ID: strings.Repeat("f", 32), TargetID: "keenetic", Name: "Spare", Address: "http://192.0.2.2", Account: "admin", Interface: "Wireguard0", CreatedAt: retirementNow, UpdatedAt: retirementNow}
	if err := store.CreateDevice(ctx, deviceB); err != nil {
		t.Fatal(err)
	}
	at := func(minutes int) time.Time { return retirementNow.Add(time.Duration(minutes) * time.Minute) }
	expect := func(outputID, label string, want *application.OutputDelivery) {
		t.Helper()
		got, err := store.LatestOutputDelivery(ctx, outputID)
		if want == nil {
			if !errors.Is(err, application.ErrNotFound) {
				t.Fatalf("%s: delivery=%#v err=%v", label, got, err)
			}
			return
		}
		if err != nil || got != *want {
			t.Fatalf("%s: got %#v want %#v err=%v", label, got, *want, err)
		}
	}
	bind := func(outputID, deviceID, label string) {
		t.Helper()
		if err := store.UpdateOutputDevice(ctx, outputID, deviceID); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	expect(f.output.ID, "an output never sent", nil)
	recordSend(t, store, attemptID("1"), f.artifact, deviceA, at(1), "deploy_failed")
	expect(f.output.ID, "a failed send", nil)
	recordSend(t, store, attemptID("2"), f.artifact, deviceA, at(2), "pending")
	expect(f.output.ID, "a send whose outcome is unknown", nil)
	recordSend(t, store, attemptID("3"), f.artifact, "", at(3), "")
	expect(f.output.ID, "a send to an address typed by hand", nil)
	recordSend(t, store, attemptID("4"), otherArtifact, deviceA, at(4), "")
	expect(f.output.ID, "a send of another output's file", nil)
	expect(f.other.ID, "an output bound to no connection", nil)
	bind(f.other.ID, deviceA, "bind the other output")
	expect(f.other.ID, "the other output's own send", &application.OutputDelivery{DeviceID: deviceA, ArtifactID: otherArtifact, CompletedAt: at(4)})
	bind(f.other.ID, "", "unbind the other output")
	expect(f.other.ID, "an output unbound after a send", nil)

	recordSend(t, store, attemptID("5"), f.artifact, deviceA, at(10), "")
	expect(f.output.ID, "first success", &application.OutputDelivery{DeviceID: deviceA, ArtifactID: f.artifact, CompletedAt: at(10)})

	// The newest send to the bound connection wins across files, whatever order
	// the attempts were begun in. A newer send to another connection, a later
	// failure and a typed address do not hide it.
	recordSend(t, store, attemptID("6"), second, deviceB.ID, at(30), "")
	recordSend(t, store, attemptID("7"), second, deviceA, at(20), "")
	recordSend(t, store, attemptID("8"), second, deviceA, at(40), "verify_failed")
	recordSend(t, store, attemptID("9"), second, "", at(50), "")
	sentToA := application.OutputDelivery{DeviceID: deviceA, ArtifactID: second, CompletedAt: at(20)}
	sentToB := application.OutputDelivery{DeviceID: deviceB.ID, ArtifactID: second, CompletedAt: at(30)}
	expect(f.output.ID, "bound to A with a newer one-off send to B", &sentToA)
	bind(f.output.ID, deviceB.ID, "bind to B")
	expect(f.output.ID, "re-bound to B", &sentToB)
	bind(f.output.ID, deviceA, "bind back to A")
	expect(f.output.ID, "re-bound back to A", &sentToA)
	bind(f.output.ID, "", "unbind")
	expect(f.output.ID, "an output unbound after sends", nil)

	if _, err := store.LatestOutputDelivery(ctx, "not-an-id"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("a malformed output id err=%v", err)
	}
}

// The connection an attempt names is part of its identity: it is stored as
// given, cannot be rewritten, and only a registered device id or nothing fits.
func TestDeploymentAttemptKeepsItsConnectionImmutably(t *testing.T) {
	ctx := context.Background()
	store := retirementStore(t)
	device := strings.Repeat("d", 32)
	recordSend(t, store, attemptID("1"), attemptID("a"), device, retirementNow.Add(time.Minute), "")
	got, err := store.DeploymentAttempt(ctx, attemptID("1"))
	if err != nil || got.DeviceID != device {
		t.Fatalf("stored attempt = %#v err=%v", got, err)
	}
	for _, statement := range []string{
		`UPDATE deployment_attempts SET device_id=''`,
		`UPDATE deployment_attempts SET device_id='` + strings.Repeat("a", 32) + `'`,
	} {
		if _, err := store.db.Exec(statement); err == nil || !strings.Contains(err.Error(), "immutable deployment attempt identity") {
			t.Fatalf("%s: err=%v", statement, err)
		}
	}
	for _, bad := range []string{"typed-by-hand", strings.ToUpper(device), device[:31]} {
		if _, _, err := store.BeginDeploymentAttempt(ctx, deliveryAttempt(attemptID("2"), attemptID("a"), bad)); err == nil {
			t.Fatalf("an attempt naming device %q was begun", bad)
		}
		if _, err := store.db.Exec(`INSERT INTO deployment_attempts(id,artifact_id,device_id,request_hash,status,started_at_ns,result_json,error_code) VALUES(?,?,?,?,'pending',1,'','')`,
			attemptID("2"), attemptID("a"), bad, strings.Repeat("e", 64)); err == nil {
			t.Fatalf("the table admitted device %q", bad)
		}
	}
	// A completion cannot move an attempt to another connection.
	pending := deliveryAttempt(attemptID("3"), attemptID("a"), device)
	if _, _, err := store.BeginDeploymentAttempt(ctx, pending); err != nil {
		t.Fatal(err)
	}
	pending.DeviceID = strings.Repeat("f", 32)
	pending.Status, pending.CompletedAt, pending.Result = application.DeploymentAttemptSucceeded, retirementNow.Add(time.Minute), application.DeployResult{Applied: true}
	if err := store.CompleteDeploymentAttempt(ctx, pending); !errors.Is(err, application.ErrDeploymentAttemptMismatch) {
		t.Fatalf("completing under another connection err=%v", err)
	}
}

// A database written before version 20 holds sends whose connection was never
// recorded. They stay as they were, are not guessed into a connection, and
// leave the next send to a saved connection reportable.
func TestVersionNineteenAttemptsAreNotReportedAsDeliveries(t *testing.T) {
	const previousVersion = 19
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
	artifact := strings.Repeat("b", 32)
	if _, err := db.Exec(`INSERT INTO deployment_attempts(id,artifact_id,request_hash,status,started_at_ns,completed_at_ns,result_json,error_code) VALUES(?,?,?,'succeeded',1,?,'{"applied":true}','')`,
		attemptID("1"), artifact, strings.Repeat("e", 64), retirementNow.Add(time.Minute).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("migrating a version-19 database: %v", err)
	}
	defer store.Close()
	legacy, err := store.DeploymentAttempt(ctx, attemptID("1"))
	if err != nil || legacy.DeviceID != "" || legacy.Status != application.DeploymentAttemptSucceeded || legacy.CompletedAt.IsZero() {
		t.Fatalf("an earlier attempt changed: %#v err=%v", legacy, err)
	}
	if _, err := store.db.Exec(`UPDATE deployment_attempts SET device_id=?`, strings.Repeat("d", 32)); err == nil {
		t.Fatal("an earlier attempt was attributed to a connection afterwards")
	}

	profile, output := publicationProfileAndOutput(strings.Repeat("1", 32), strings.Repeat("3", 32), retirementNow)
	if err := store.CreateProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOutput(ctx, application.NewOutput{Output: output}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Publish(ctx, publicationCandidate(profile, output.ID, strings.Repeat("a", 32), artifact, []byte(`{"cutoff":1}`), "legacy", retirementNow)); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LatestOutputDelivery(ctx, output.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("an unattributed send was reported: %#v err=%v", got, err)
	}
	device := strings.Repeat("d", 32)
	if err := store.CreateDevice(ctx, application.Device{ID: device, TargetID: "keenetic", Name: "Router", Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0", CreatedAt: retirementNow, UpdatedAt: retirementNow}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOutputDevice(ctx, output.ID, device); err != nil {
		t.Fatal(err)
	}
	recordSend(t, store, attemptID("2"), artifact, device, retirementNow.Add(2*time.Minute), "")
	if got, err := store.LatestOutputDelivery(ctx, output.ID); err != nil || got.DeviceID != device || got.ArtifactID != artifact {
		t.Fatalf("the first attributed send: %#v err=%v", got, err)
	}
}
