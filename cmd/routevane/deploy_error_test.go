package main

import (
	"errors"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestDeployErrorReportsFailedRecoveryBeforeItsCause(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{application.ErrVerifyFailed, "verify_failed"},
		{errors.Join(application.ErrVerifyFailed, application.ErrRollbackFailed), "rollback_failed"},
		{errors.Join(application.ErrDeployFailed, application.ErrFQDNOwnershipConflict), "fqdn_ownership_conflict"},
		{errors.Join(application.ErrDeployFailed, application.ErrDeviceAuthRejected), "device_auth_rejected"},
		{errors.Join(application.ErrDeployFailed, application.ErrDeviceUnreachable), "device_unreachable"},
		{application.ErrDeployFailed, "deploy_failed"},
		{errors.Join(application.ErrDeployComposition, application.ErrDeviceUnreachable), "composition_invalid"},
	} {
		if got := deployErrorCode(tc.err); got != tc.code {
			t.Fatalf("%v: got %s, want %s", tc.err, got, tc.code)
		}
	}
}
