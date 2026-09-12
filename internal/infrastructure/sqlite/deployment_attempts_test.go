package sqlite_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

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
	root := t.TempDir()
	store, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	executor := &attemptExecutor{result: application.DeployResult{ArtifactID: strings.Repeat("a", 32), Applied: true}}
	service := attemptService(t, store, executor)
	id := strings.Repeat("b", 32)

	first, err := service.Deploy(context.Background(), id, attemptCommand("http://192.168.1.1"))
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
	recovered, err := afterRestart.Deploy(context.Background(), id, attemptCommand("http://192.168.1.1"))
	if err != nil || recovered.Status != application.DeploymentAttemptSucceeded || afterRestartExecutor.calls != 0 {
		t.Fatalf("recovered=%#v calls=%d err=%v", recovered, afterRestartExecutor.calls, err)
	}
	_, err = afterRestart.Deploy(context.Background(), id, attemptCommand("http://192.168.1.2"))
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
	store, err := sqlite.Open(context.Background(), t.TempDir())
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
	completed, err := service.Deploy(ctx, id, attemptCommand("http://192.168.1.1"))
	if !errors.Is(err, application.ErrVerifyFailed) || completed.Status != application.DeploymentAttemptFailed || !completed.Result.RolledBack {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	recorded, err := service.Attempt(context.Background(), id)
	if err != nil || recorded.Status != application.DeploymentAttemptFailed || recorded.Error != "verify_failed" || !recorded.Result.RolledBack {
		t.Fatalf("recorded=%#v err=%v", recorded, err)
	}
}

func TestPendingAttemptSurvivesRestartAsUnknown(t *testing.T) {
	root := t.TempDir()
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
