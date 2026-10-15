package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

func attemptDataRoot(t *testing.T) string {
	t.Helper()
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

type attemptExecutor struct {
	calls  int
	result application.DeployResult
	err    error
	cancel context.CancelFunc
}

func (e *attemptExecutor) Deploy(context.Context, application.DeployCommand) (application.DeployResult, error) {
	e.calls++
	if e.cancel != nil {
		e.cancel()
	}
	return e.result, e.err
}

func attemptService(t *testing.T, journal application.DeploymentAttemptJournal, executor application.DeploymentAttemptExecutor) *application.DeploymentAttemptService {
	t.Helper()
	service, err := application.NewDeploymentAttemptService(application.DeploymentAttemptConfig{
		Deployments: executor,
		Journal:     journal,
		Clock:       application.ClockFunc(func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func attemptCommand(device string) application.DeployCommand {
	return application.DeployCommand{
		ArtifactID: strings.Repeat("a", 32), Confirm: true,
		Connection: application.Connection{URL: device, Username: "admin", Password: "never-store-this", Interface: "Wireguard0"}, // betterleaks:allow -- synthetic credential for an in-process test executor
	}
}

func TestDeploymentAttemptIsDurableAndNeverReplaysEffects(t *testing.T) {
	root := attemptDataRoot(t)
	store, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	executor := &attemptExecutor{result: application.DeployResult{ArtifactID: strings.Repeat("a", 32), Applied: true}}
	service := attemptService(t, store, executor)
	id := strings.Repeat("b", 32)

	first, err := service.Deploy(context.Background(), id, attemptCommand("http://192.0.2.1"))
	if err != nil || first.Status != application.DeploymentAttemptSucceeded || executor.calls != 1 {
		t.Fatalf("first=%#v calls=%d err=%v", first, executor.calls, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	afterRestartExecutor := &attemptExecutor{}
	afterRestart := attemptService(t, reopened, afterRestartExecutor)
	recovered, err := afterRestart.Deploy(context.Background(), id, attemptCommand("http://192.0.2.1"))
	if err != nil || recovered.Status != application.DeploymentAttemptSucceeded || afterRestartExecutor.calls != 0 {
		t.Fatalf("recovered=%#v calls=%d err=%v", recovered, afterRestartExecutor.calls, err)
	}
	_, err = afterRestart.Deploy(context.Background(), id, attemptCommand("http://192.0.2.2"))
	if !errors.Is(err, application.ErrDeploymentAttemptMismatch) || afterRestartExecutor.calls != 0 {
		t.Fatalf("mismatched retry calls=%d err=%v", afterRestartExecutor.calls, err)
	}
	encoded, err := reopened.DeploymentAttempt(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.RequestHash == "" || strings.Contains(encoded.RequestHash, "never-store-this") {
		t.Fatalf("request binding=%q", encoded.RequestHash)
	}
}

func TestCanceledRequestStillRecordsTheOutcomeAfterRecovery(t *testing.T) {
	store, err := sqlite.Open(context.Background(), attemptDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	executor := &attemptExecutor{
		result: application.DeployResult{ArtifactID: strings.Repeat("a", 32), RolledBack: true},
		err:    application.ErrVerifyFailed,
		cancel: cancel,
	}
	service := attemptService(t, store, executor)
	id := strings.Repeat("c", 32)
	completed, err := service.Deploy(ctx, id, attemptCommand("http://192.0.2.1"))
	if !errors.Is(err, application.ErrVerifyFailed) || completed.Status != application.DeploymentAttemptFailed || !completed.Result.RolledBack {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	recorded, err := service.Attempt(context.Background(), id)
	if err != nil || recorded.Status != application.DeploymentAttemptFailed || recorded.Error != "verify_failed" || !recorded.Result.RolledBack {
		t.Fatalf("recorded=%#v err=%v", recorded, err)
	}
}

func TestPendingAttemptSurvivesRestartAsUnknown(t *testing.T) {
	root := attemptDataRoot(t)
	store, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 32)
	_, created, err := store.BeginDeploymentAttempt(context.Background(), application.DeploymentAttempt{
		ID: id, ArtifactID: strings.Repeat("a", 32), RequestHash: strings.Repeat("e", 64),
		Status: application.DeploymentAttemptPending, StartedAt: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || !created {
		t.Fatalf("created=%t err=%v", created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	service := attemptService(t, reopened, &attemptExecutor{})
	attempt, err := service.Attempt(context.Background(), id)
	if !errors.Is(err, application.ErrDeploymentOutcomeUnknown) || attempt.Status != application.DeploymentAttemptPending {
		t.Fatalf("attempt=%#v err=%v", attempt, err)
	}
}

// The saved connection a send was resolved from is kept with its attempt, and
// an attempt id is not reusable for a send to another connection: the id would
// otherwise answer with an outcome that belongs to a different router.
func TestDeploymentAttemptRecordsTheSavedConnectionItWasResolvedFrom(t *testing.T) {
	store, err := sqlite.Open(context.Background(), attemptDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	executor := &attemptExecutor{result: application.DeployResult{ArtifactID: strings.Repeat("a", 32), Applied: true}}
	service := attemptService(t, store, executor)
	saved := attemptCommand("http://192.0.2.1")
	saved.SavedDeviceID = strings.Repeat("d", 32)

	id := strings.Repeat("b", 32)
	if _, err := service.Deploy(context.Background(), id, saved); err != nil {
		t.Fatal(err)
	}
	recorded, err := service.Attempt(context.Background(), id)
	if err != nil || recorded.DeviceID != saved.SavedDeviceID {
		t.Fatalf("recorded=%#v err=%v", recorded, err)
	}
	// Same destination, another saved connection: neither a replay nor a send.
	other := saved
	other.SavedDeviceID = strings.Repeat("f", 32)
	if _, err := service.Deploy(context.Background(), id, other); !errors.Is(err, application.ErrDeploymentAttemptMismatch) || executor.calls != 1 {
		t.Fatalf("calls=%d err=%v", executor.calls, err)
	}
	if _, err := service.Deploy(context.Background(), id, attemptCommand("http://192.0.2.1")); !errors.Is(err, application.ErrDeploymentAttemptMismatch) || executor.calls != 1 {
		t.Fatalf("typed address under a saved attempt: calls=%d err=%v", executor.calls, err)
	}

	typedID := strings.Repeat("c", 32)
	if _, err := service.Deploy(context.Background(), typedID, attemptCommand("http://192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	if typed, err := service.Attempt(context.Background(), typedID); err != nil || typed.DeviceID != "" {
		t.Fatalf("a typed address recorded a connection: %#v err=%v", typed, err)
	}
}

type faultyJournal struct {
	application.DeploymentAttemptJournal
	failBegin, failComplete bool
}

func (j faultyJournal) BeginDeploymentAttempt(ctx context.Context, attempt application.DeploymentAttempt) (application.DeploymentAttempt, bool, error) {
	if j.failBegin {
		return application.DeploymentAttempt{}, false, errors.New("journal unavailable")
	}
	return j.DeploymentAttemptJournal.BeginDeploymentAttempt(ctx, attempt)
}

func (j faultyJournal) CompleteDeploymentAttempt(ctx context.Context, attempt application.DeploymentAttempt) error {
	if j.failComplete {
		return errors.New("journal unavailable")
	}
	return j.DeploymentAttemptJournal.CompleteDeploymentAttempt(ctx, attempt)
}

// A scheduled send is recorded like a confirmed one, but the record never
// decides the send: whatever the journal does, the deployment runs once and
// its own result is the one returned.
func TestUnattendedDeploymentIsRecordedAndNeverJudgedByItsRecord(t *testing.T) {
	device := strings.Repeat("d", 32)
	applied := application.DeployResult{ArtifactID: strings.Repeat("a", 32), Applied: true}
	tests := []struct {
		name         string
		journal      func(*sqlite.Store) application.DeploymentAttemptJournal
		id           string
		deployErr    error
		wantJournal  bool
		wantStatus   application.DeploymentAttemptStatus
		wantRecorded bool
	}{
		{name: "recorded in full", journal: func(s *sqlite.Store) application.DeploymentAttemptJournal { return s }, id: strings.Repeat("b", 32), wantStatus: application.DeploymentAttemptSucceeded, wantRecorded: true},
		{name: "a failed send is recorded as failed", journal: func(s *sqlite.Store) application.DeploymentAttemptJournal { return s }, id: strings.Repeat("b", 32), deployErr: application.ErrVerifyFailed, wantStatus: application.DeploymentAttemptFailed, wantRecorded: true},
		{name: "begin fails: sent unrecorded", journal: func(s *sqlite.Store) application.DeploymentAttemptJournal {
			return faultyJournal{DeploymentAttemptJournal: s, failBegin: true}
		}, id: strings.Repeat("b", 32), wantJournal: true},
		{name: "complete fails: outcome kept, record pending", journal: func(s *sqlite.Store) application.DeploymentAttemptJournal {
			return faultyJournal{DeploymentAttemptJournal: s, failComplete: true}
		}, id: strings.Repeat("b", 32), wantJournal: true, wantStatus: application.DeploymentAttemptPending, wantRecorded: true},
		{name: "unusable id: sent unrecorded", journal: func(s *sqlite.Store) application.DeploymentAttemptJournal { return s }, id: "", wantJournal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := sqlite.Open(context.Background(), attemptDataRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			result := applied
			if test.deployErr != nil {
				result.Applied = false
			}
			executor := &attemptExecutor{result: result, err: test.deployErr}
			service := attemptService(t, test.journal(store), executor)
			command := attemptCommand("http://192.0.2.1")
			command.SavedDeviceID = device

			outcome := service.DeployUnattended(context.Background(), test.id, command)
			if executor.calls != 1 || outcome.Result.Applied != result.Applied || outcome.Result.ArtifactID != result.ArtifactID || !errors.Is(outcome.Err, test.deployErr) {
				t.Fatalf("calls=%d outcome=%#v", executor.calls, outcome)
			}
			if (outcome.JournalErr != nil) != test.wantJournal {
				t.Fatalf("journal fault = %v, want fault=%t", outcome.JournalErr, test.wantJournal)
			}
			stored, err := store.DeploymentAttempt(context.Background(), test.id)
			if !test.wantRecorded {
				if !errors.Is(err, application.ErrNotFound) {
					t.Fatalf("an unrecorded send left a row: %#v err=%v", stored, err)
				}
				return
			}
			if err != nil || stored.Status != test.wantStatus || stored.DeviceID != device {
				t.Fatalf("stored=%#v err=%v", stored, err)
			}
		})
	}
}
