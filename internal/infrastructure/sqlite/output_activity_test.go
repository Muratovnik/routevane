package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// recordFailedSend stores one failed attempt whose audit trail stopped at step.
func recordFailedSend(t *testing.T, store *Store, id, artifactID, deviceID string, completedAt time.Time, code, step string) {
	t.Helper()
	ctx := context.Background()
	pending := deliveryAttempt(id, artifactID, deviceID)
	if _, created, err := store.BeginDeploymentAttempt(ctx, pending); err != nil || !created {
		t.Fatalf("begin %s: created=%t err=%v", id, created, err)
	}
	completed := pending
	completed.CompletedAt, completed.Status, completed.Error = completedAt, application.DeploymentAttemptFailed, code
	completed.Result = application.DeployResult{ArtifactID: artifactID, Events: []application.DeployEvent{
		{Step: application.StepProbe, Outcome: "ok"},
		{Step: step, Outcome: "failed"},
	}}
	if err := store.CompleteDeploymentAttempt(ctx, completed); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func readActivity(t *testing.T, store *Store, outputIDs ...string) map[string]application.OutputActivity {
	t.Helper()
	activities, err := store.OutputActivities(context.Background(), outputIDs)
	if err != nil {
		t.Fatalf("output activities: %v", err)
	}
	return activities
}

// A format's delivery state names the newest successful send of any of its
// files to any saved connection, bound or not, and the newest failed send to a
// saved connection only while no send has succeeded since.
func TestOutputActivityReportsTheNewestSendToAnySavedConnection(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store
	deviceA := f.device.ID
	deviceB := application.Device{ID: strings.Repeat("f", 32), TargetID: "keenetic", Name: "Spare", Address: "http://192.0.2.2", Account: "admin", Interface: "Wireguard0", CreatedAt: retirementNow, UpdatedAt: retirementNow}
	if err := store.CreateDevice(ctx, deviceB); err != nil {
		t.Fatal(err)
	}
	otherArtifact := strings.Repeat("e", 32)
	publishAnother(t, f, f.sibling, f.other.ID, strings.Repeat("8", 32), otherArtifact, "3")
	at := func(minutes int) time.Time { return retirementNow.Add(time.Duration(minutes) * time.Minute) }
	state := func(label string) application.OutputActivity {
		t.Helper()
		activity, ok := readActivity(t, store, f.output.ID, f.other.ID)[f.output.ID]
		if !ok {
			t.Fatalf("%s: the output has no activity", label)
		}
		return activity
	}
	expect := func(label string, delivery *application.OutputDelivery, failure *application.OutputDeliveryFailure) {
		t.Helper()
		got := state(label)
		if !reflect.DeepEqual(got.LatestDelivery, delivery) || !reflect.DeepEqual(got.LatestFailure, failure) {
			t.Fatalf("%s: delivery=%+v failure=%+v, want delivery=%+v failure=%+v", label, got.LatestDelivery, got.LatestFailure, delivery, failure)
		}
	}

	initial := state("never sent")
	if initial.DeviceName != f.device.Name || initial.LatestDelivery != nil || initial.LatestFailure != nil {
		t.Fatalf("never sent: %+v", initial)
	}

	// The live acceptance case: a manual send to a saved connection the format
	// is not bound to is a delivery, though the bound connection has none.
	recordSend(t, store, attemptID("1"), f.artifact, deviceB.ID, at(10), "")
	toB := &application.OutputDelivery{DeviceID: deviceB.ID, ArtifactID: f.artifact, CompletedAt: at(10)}
	expect("a send to an unbound saved connection", toB, nil)
	if _, err := store.LatestOutputDelivery(ctx, f.output.ID); err == nil {
		t.Fatal("last_delivery changed meaning: it now counts a send to an unbound connection")
	}

	// Neither a typed address nor an unfinished attempt names a delivery or a
	// failure, however recent; another output's sends never leak in.
	recordSend(t, store, attemptID("2"), f.artifact, "", at(20), "")
	recordFailedSend(t, store, attemptID("3"), f.artifact, "", at(21), "device_unreachable", application.StepProbe)
	recordSend(t, store, attemptID("4"), f.artifact, deviceA, at(22), "pending")
	recordSend(t, store, attemptID("5"), otherArtifact, deviceA, at(23), "")
	recordFailedSend(t, store, attemptID("6"), otherArtifact, deviceA, at(24), "verify_failed", application.StepVerify)
	expect("typed, pending and foreign sends", toB, nil)
	if other := readActivity(t, store, f.other.ID)[f.other.ID]; other.LatestFailure == nil || other.LatestFailure.Code != "verify_failed" {
		t.Fatalf("the other output's own failure is missing: %+v", other)
	}

	// A failure after the delivery is reported with its code and step.
	recordFailedSend(t, store, attemptID("7"), f.artifact, deviceA, at(30), "device_auth_rejected", application.StepBackup)
	failure := &application.OutputDeliveryFailure{DeviceID: deviceA, Code: "device_auth_rejected", Step: application.StepBackup, CompletedAt: at(30)}
	expect("a failure after a delivery", toB, failure)
	recordSend(t, store, attemptID("8"), f.artifact, deviceA, at(25), "")
	toA25 := &application.OutputDelivery{DeviceID: deviceA, ArtifactID: f.artifact, CompletedAt: at(25)}
	expect("a success completed before the failure", toA25, failure)

	// A newer success clears it; a failure at the same moment is not newer.
	recordSend(t, store, attemptID("9"), f.artifact, deviceA, at(40), "")
	toA := &application.OutputDelivery{DeviceID: deviceA, ArtifactID: f.artifact, CompletedAt: at(40)}
	expect("a success after the failure", toA, nil)
	recordFailedSend(t, store, attemptID("a"), f.artifact, deviceB.ID, at(40), "deploy_failed", "")
	expect("a failure at the moment of the success", toA, nil)

	// The binding does not filter: unbinding keeps the delivery and drops the name.
	if err := store.UpdateOutputDevice(ctx, f.output.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := state("unbound"); got.DeviceName != "" || !reflect.DeepEqual(got.LatestDelivery, toA) {
		t.Fatalf("unbound: %+v", got)
	}
}

// The read covers a page of outputs in one call and refuses what is not one.
func TestOutputActivitiesBoundItsInput(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	if got, err := f.store.OutputActivities(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("no outputs: %v err=%v", got, err)
	}
	if got := readActivity(t, f.store, strings.Repeat("9", 32)); len(got) != 0 {
		t.Fatalf("an unknown output has activity: %v", got)
	}
	for label, ids := range map[string][]string{
		"a malformed id": {"not-an-id"},
		"beyond a page":  make([]string, maxActivityOutputs+1),
	} {
		if _, err := f.store.OutputActivities(ctx, ids); err == nil {
			t.Fatalf("%s was accepted", label)
		}
	}
	if err := f.store.RemoveOutput(ctx, f.output.ID, retirementNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := readActivity(t, f.store, f.output.ID); len(got) != 0 {
		t.Fatalf("a removed output has activity: %v", got)
	}
}

// built_at is when the format's current file became current. A rebuild whose
// plan changed publishes a new file and moves it even when the bytes, and so
// the content time, are those of an older file; a rebuild that produced the
// current file again does not, though it is the newest build attempt.
func TestBuiltAtIsWhenTheCurrentFileBecameCurrent(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	store := f.store
	publishAt := func(hours int, snapshot, artifact, plan string) {
		t.Helper()
		candidate := publicationCandidate(f.profile, f.output.ID, snapshot, artifact, []byte(plan), "retired", retirementNow.Add(time.Duration(hours)*time.Hour))
		if _, _, _, err := store.Publish(ctx, candidate); err != nil {
			t.Fatalf("publish at +%dh: %v", hours, err)
		}
	}
	expect := func(label string, artifactID string, builtAt time.Time) {
		t.Helper()
		output, err := store.Output(ctx, f.output.ID)
		if err != nil || output.LatestArtifactID != artifactID {
			t.Fatalf("%s: latest=%q want %q err=%v", label, output.LatestArtifactID, artifactID, err)
		}
		artifact, err := store.ArtifactBuild(ctx, artifactID)
		if err != nil || !artifact.ContentCreatedAt.Equal(retirementNow) {
			t.Fatalf("%s: content time %v err=%v", label, artifact.ContentCreatedAt, err)
		}
		if got := readActivity(t, store, f.output.ID)[f.output.ID].BuiltAt; !got.Equal(builtAt) {
			t.Fatalf("%s: built_at=%v want %v", label, got, builtAt)
		}
	}

	expect("first build", f.artifact, retirementNow)

	publishAt(1, strings.Repeat("6", 32), strings.Repeat("7", 32), `{"cutoff":1}`)
	expect("the same file built again", f.artifact, retirementNow)
	if attempt, err := store.LatestOutputAttempt(ctx, f.output.ID); err != nil || !attempt.CompletedAt.Equal(retirementNow.Add(time.Hour)) {
		t.Fatalf("the repeated build is not the latest attempt: %+v err=%v", attempt, err)
	}

	changed := strings.Repeat("c", 32)
	publishAt(2, strings.Repeat("9", 32), changed, `{"cutoff":2}`)
	expect("a changed plan with the same bytes", changed, retirementNow.Add(2*time.Hour))

	publishAt(3, strings.Repeat("6", 32), strings.Repeat("7", 32), `{"cutoff":1}`)
	expect("the first file current again", f.artifact, retirementNow.Add(3*time.Hour))

	if got := readActivity(t, store, f.other.ID)[f.other.ID].BuiltAt; !got.IsZero() {
		t.Fatalf("an output that never built has built_at %v", got)
	}
}
