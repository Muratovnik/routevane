package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrDeploymentAttemptComposition = errors.New("invalid deployment attempt composition")
	ErrDeploymentAttemptInvalid     = errors.New("invalid deployment attempt identity")
	ErrDeploymentAttemptMismatch    = errors.New("deployment attempt belongs to another request")
	ErrDeploymentOutcomeUnknown     = errors.New("deployment outcome is unknown")
)

type DeploymentAttemptStatus string

const (
	DeploymentAttemptPending   DeploymentAttemptStatus = "pending"
	DeploymentAttemptSucceeded DeploymentAttemptStatus = "succeeded"
	DeploymentAttemptFailed    DeploymentAttemptStatus = "failed"

	deploymentAttemptRecordBudget = 5 * time.Second
)

// DeploymentAttempt is the durable identity and outcome of one confirmed
// deployment. RequestHash binds the identity to the artifact and redacted
// destination inputs; it never includes the password.
type DeploymentAttempt struct {
	ID          string
	ArtifactID  string
	RequestHash string
	Status      DeploymentAttemptStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Result      DeployResult
	Error       string
}

// DeploymentAttemptJournal atomically claims a caller-generated identity and
// records its one terminal outcome. It is declared by the application service
// that consumes it; persistence does not decide whether a deployment runs.
type DeploymentAttemptJournal interface {
	BeginDeploymentAttempt(context.Context, DeploymentAttempt) (DeploymentAttempt, bool, error)
	CompleteDeploymentAttempt(context.Context, DeploymentAttempt) error
	DeploymentAttempt(context.Context, string) (DeploymentAttempt, error)
}

type DeploymentAttemptExecutor interface {
	Deploy(context.Context, DeployCommand) (DeployResult, error)
}

type DeploymentAttemptConfig struct {
	Deployments DeploymentAttemptExecutor
	Journal     DeploymentAttemptJournal
	Clock       Clock
}

// DeploymentAttemptService makes a confirmed deployment safely identifiable.
// The existing deployment service still owns every device-side step.
type DeploymentAttemptService struct {
	config DeploymentAttemptConfig
}

func NewDeploymentAttemptService(config DeploymentAttemptConfig) (*DeploymentAttemptService, error) {
	if config.Deployments == nil || config.Journal == nil || config.Clock == nil {
		return nil, ErrDeploymentAttemptComposition
	}
	return &DeploymentAttemptService{config: config}, nil
}

// Deploy claims id before reaching the device. An existing id returns its
// recorded result, or an unknown outcome while its first process may still be
// running; neither case repeats device effects.
func (s *DeploymentAttemptService) Deploy(ctx context.Context, id string, command DeployCommand) (DeploymentAttempt, error) {
	if !command.Confirm || !validDeploymentAttemptID(id) {
		return DeploymentAttempt{}, ErrDeploymentAttemptInvalid
	}
	proposed := DeploymentAttempt{
		ID: id, ArtifactID: command.ArtifactID,
		RequestHash: deploymentRequestHash(command),
		Status:      DeploymentAttemptPending, StartedAt: s.config.Clock.Now().UTC(),
	}
	attempt, created, err := s.config.Journal.BeginDeploymentAttempt(ctx, proposed)
	if err != nil {
		return DeploymentAttempt{}, fmt.Errorf("begin deployment attempt %q: %w", id, err)
	}
	if attempt.ArtifactID != proposed.ArtifactID || attempt.RequestHash != proposed.RequestHash {
		return DeploymentAttempt{}, ErrDeploymentAttemptMismatch
	}
	if !created {
		if attempt.Status == DeploymentAttemptPending {
			return attempt, ErrDeploymentOutcomeUnknown
		}
		return attempt, nil
	}

	result, deployErr := s.config.Deployments.Deploy(ctx, command)
	completed := proposed
	completed.Result = result
	completed.CompletedAt = s.config.Clock.Now().UTC()
	completed.Error = DeploymentFailureCode(deployErr)
	if deployErr == nil {
		completed.Status = DeploymentAttemptSucceeded
	} else {
		completed.Status = DeploymentAttemptFailed
	}

	// Deployment may have used a detached rollback after request cancellation.
	// Its result must get the same protection so cancellation cannot leave a
	// completed recovery recorded forever as merely pending.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deploymentAttemptRecordBudget)
	defer cancel()
	if err := s.config.Journal.CompleteDeploymentAttempt(persistCtx, completed); err != nil {
		return proposed, fmt.Errorf("%w: record deployment attempt %q: %v", ErrDeploymentOutcomeUnknown, id, err)
	}
	return completed, deployErr
}

func (s *DeploymentAttemptService) Attempt(ctx context.Context, id string) (DeploymentAttempt, error) {
	if !validDeploymentAttemptID(id) {
		return DeploymentAttempt{}, ErrNotFound
	}
	attempt, err := s.config.Journal.DeploymentAttempt(ctx, id)
	if err != nil {
		return DeploymentAttempt{}, err
	}
	if attempt.Status == DeploymentAttemptPending {
		return attempt, ErrDeploymentOutcomeUnknown
	}
	return attempt, nil
}

func validDeploymentAttemptID(id string) bool {
	if len(id) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(decoded) == id
}

func deploymentRequestHash(command DeployCommand) string {
	redacted := struct {
		ArtifactID string `json:"artifact_id"`
		Device     string `json:"device"`
		Username   string `json:"username"`
		Interface  string `json:"interface"`
	}{
		ArtifactID: command.ArtifactID,
		Device:     command.Connection.URL, Username: command.Connection.Username,
		Interface: command.Connection.Interface,
	}
	payload, _ := json.Marshal(redacted)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// DeploymentFailureCode is the stable, non-secret classification persisted
// with an attempt and returned by the HTTP boundary.
func DeploymentFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrRollbackFailed):
		return "rollback_failed"
	case errors.Is(err, ErrFQDNOwnershipConflict):
		return "fqdn_ownership_conflict"
	case errors.Is(err, ErrConnectionInvalid):
		return "connection_invalid"
	case errors.Is(err, ErrDeviceIncompatible):
		return "device_incompatible"
	case errors.Is(err, ErrBackupRequired):
		return "backup_unavailable"
	case errors.Is(err, ErrVerifyFailed):
		return "verify_failed"
	case errors.Is(err, ErrDeployerUnavailable):
		return "deployer_unavailable"
	case errors.Is(err, ErrConfirmationRequired):
		return "confirmation_required"
	default:
		return "deploy_failed"
	}
}
