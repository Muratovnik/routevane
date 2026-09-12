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
// Without confirm the handler answers with the plan, including read-only device
// inspection when required. Only the confirmed step can mutate the device.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		AttemptID string `json:"attempt_id"`
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
			if errors.Is(err, application.ErrFQDNOwnershipConflict) || errors.Is(err, application.ErrDeployFailed) || errors.Is(err, application.ErrConnectionInvalid) || errors.Is(err, application.ErrDeviceIncompatible) {
				writeJSON(w, deployStatus(err), map[string]string{"error": deployFailure(err)})
				return
			}
			h.backendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "confirmed": false})
		return
	}
	if request.AttemptID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attempt_id_required"})
		return
	}
	attempt, err := h.backend.DeployAttempt(r.Context(), request.AttemptID, command)
	if errors.Is(err, application.ErrDeploymentAttemptInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attempt_id_invalid"})
		return
	}
	if errors.Is(err, application.ErrDeploymentAttemptMismatch) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "attempt_mismatch"})
		return
	}
	if errors.Is(err, application.ErrDeploymentOutcomeUnknown) {
		writeJSON(w, http.StatusAccepted, deploymentAttemptBody(attempt))
		return
	}
	if attempt.ID == "" {
		if err != nil {
			h.backendError(w, err)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "deploy_failed"})
		return
	}
	// A failed deployment still returns its audit trail: a failed attempt is
	// exactly the case an operator needs the record of.
	body := deploymentAttemptBody(attempt)
	if attempt.Status == application.DeploymentAttemptFailed {
		writeJSON(w, deployCodeStatus(attempt.Error), body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *handler) deploymentAttempt(w http.ResponseWriter, r *http.Request, id string) {
	attempt, err := h.backend.DeploymentAttempt(r.Context(), id)
	if errors.Is(err, application.ErrDeploymentOutcomeUnknown) {
		writeJSON(w, http.StatusOK, deploymentAttemptBody(attempt))
		return
	}
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deploymentAttemptBody(attempt))
}

func deploymentAttemptBody(attempt application.DeploymentAttempt) map[string]any {
	status := string(attempt.Status)
	if attempt.Status == application.DeploymentAttemptPending {
		status = "outcome_unknown"
	}
	body := map[string]any{
		"attempt_id":  attempt.ID,
		"artifact_id": attempt.ArtifactID,
		"status":      status,
		"result":      attempt.Result,
		"confirmed":   true,
	}
	if attempt.Error != "" {
		body["error"] = attempt.Error
	}
	return body
}

// deployFailure is the stable code a screen shows. The underlying message can
// name a device answer, so it is logged rather than returned.
func deployFailure(err error) string {
	return application.DeploymentFailureCode(err)
}

func deployCodeStatus(code string) int {
	switch code {
	case "connection_invalid", "confirmation_required", "deployer_unavailable":
		return http.StatusBadRequest
	default:
		return http.StatusConflict
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
