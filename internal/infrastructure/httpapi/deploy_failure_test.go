package httpapi

import (
	"errors"
	"github.com/Muratovnik/routevane/internal/application"
	"testing"
)

func TestFailedRecoveryOutranksTheOriginalVerificationFailure(t *testing.T) {
	joined := errors.Join(application.ErrVerifyFailed, application.ErrRollbackFailed)
	if got := deployFailure(joined); got != "rollback_failed" {
		t.Fatalf("failure=%s", got)
	}
	if got := deployFailure(application.ErrVerifyFailed); got != "verify_failed" {
		t.Fatalf("recovered verification=%s", got)
	}
}
