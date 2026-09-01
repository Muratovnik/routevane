// The deployment transport, including the stable failure codes a screen shows.
// A device change carries its own budget and its own error taxonomy, so it does
// not share a file with the routes that only answer questions.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

// deploy applies one published artifact to one destination.
//
// The credential crosses this boundary and stops here: it is read from the
// request body, handed to the deployer for this one call, and never stored,
// logged, or returned. The request log records the route and method only, and
// the audit record this returns carries identities and outcomes.
//
// Without confirm the handler answers with the plan and touches nothing, which
// is the same two-step the command line requires.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Device    string `json:"device"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Interface string `json:"interface"`
		Confirm   bool   `json:"confirm"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	command := application.DeployCommand{
		ArtifactID: id,
		Connection: application.Connection{
			URL: request.Device, Username: request.Username,
			Password: request.Password, Interface: request.Interface,
		},
		Confirm: request.Confirm,
	}
	if !command.Confirm {
		plan, err := h.backend.DeployPlan(r.Context(), command)
		if err != nil {
			h.backendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "confirmed": false})
		return
	}
	result, err := h.backend.Deploy(r.Context(), command)
	// A failed deployment still returns its audit trail: a failed attempt is
	// exactly the case an operator needs the record of.
	body := map[string]any{"result": result, "confirmed": true}
	if err != nil {
		body["error"] = deployFailure(err)
		writeJSON(w, deployStatus(err), body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// deployFailure is the stable code a screen shows. The underlying message can
// name a device answer, so it is logged rather than returned.
func deployFailure(err error) string {
	switch {
	case errors.Is(err, application.ErrConnectionInvalid):
		return "connection_invalid"
	case errors.Is(err, application.ErrDeviceIncompatible):
		return "device_incompatible"
	case errors.Is(err, application.ErrBackupRequired):
		return "backup_unavailable"
	case errors.Is(err, application.ErrVerifyFailed):
		return "verify_failed"
	case errors.Is(err, application.ErrRollbackFailed):
		return "rollback_failed"
	case errors.Is(err, application.ErrDeployerUnavailable):
		return "deployer_unavailable"
	case errors.Is(err, application.ErrConfirmationRequired):
		return "confirmation_required"
	default:
		return "deploy_failed"
	}
}

func deployStatus(err error) int {
	switch {
	case errors.Is(err, application.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, application.ErrConnectionInvalid), errors.Is(err, application.ErrDeployComposition),
		errors.Is(err, application.ErrConfirmationRequired), errors.Is(err, application.ErrDeployerUnavailable):
		return http.StatusBadRequest
	default:
		return http.StatusConflict
	}
}
