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
// A one-use credential is handed to the deployer without being stored, logged,
// or returned. A registered selection may resolve a consented saved credential
// under the delivery gate. The request log records the route and method only,
// and the audit record this returns carries identities and outcomes.
//
// Without confirm the handler answers with the plan, including read-only device
// inspection when required. Only the confirmed step can mutate the device.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		AttemptID          string                                `json:"attempt_id"`
		DeviceID           string                                `json:"device_id"`
		ExpectedConnection *application.DeviceConnectionSnapshot `json:"expected_connection"`
		Device             string                                `json:"device"`
		Username           string                                `json:"username"`
		Password           string                                `json:"password"`
		Interface          string                                `json:"interface"`
		Confirm            bool                                  `json:"confirm"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.DeviceID != "" && (request.ExpectedConnection == nil || request.Device != "" || request.Username != "" || request.Interface != "") || request.DeviceID == "" && request.ExpectedConnection != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "connection_invalid", "attempt_started": false})
		return
	}
	command := application.DeployCommand{
		ArtifactID:         id,
		DeviceID:           request.DeviceID,
		ExpectedConnection: request.ExpectedConnection,
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
				writeJSON(w, deployStatus(err), deployRefusal(err))
				return
			}
			h.backendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "confirmed": false})
		return
	}
	if request.AttemptID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "attempt_id_required", "attempt_started": false})
		return
	}
	attempt, err := h.backend.DeployAttempt(r.Context(), request.AttemptID, command)
	if errors.Is(err, application.ErrDeploymentNotStarted) {
		h.deployNotStarted(w, err)
		return
	}
	if errors.Is(err, application.ErrDeploymentAttemptInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "attempt_id_invalid", "attempt_started": false})
		return
	}
	if errors.Is(err, application.ErrDeploymentAttemptMismatch) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "attempt_mismatch", "attempt_started": false})
		return
	}
	if errors.Is(err, application.ErrDeploymentOutcomeUnknown) {
		writeJSON(w, http.StatusAccepted, deploymentAttemptBody(attempt))
		return
	}
	if attempt.ID == "" {
		if err != nil {
			if errors.Is(err, application.ErrConnectionInvalid) || errors.Is(err, application.ErrDeviceIncompatible) || errors.Is(err, application.ErrDeployerUnavailable) {
				writeJSON(w, deployStatus(err), deployRefusal(err))
				return
			}
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

// deployNotStarted is used only when the backend explicitly proves that this
// request stopped before claiming an attempt. Journal/transport ambiguity must
// retain recovery semantics even when no attempt record was returned.
func (h *handler) deployNotStarted(w http.ResponseWriter, err error) {
	status, code := deployStatus(err), deployFailure(err)
	switch {
	case errors.Is(err, application.ErrNotFound):
		code = "not found"
	case errors.Is(err, application.ErrArtifactUnavailable):
		status, code = http.StatusServiceUnavailable, "artifact unavailable"
	case errors.Is(err, application.ErrConnectionInvalid), errors.Is(err, application.ErrDeviceIncompatible), errors.Is(err, application.ErrDeployerUnavailable):
	default:
		status, code = http.StatusUnprocessableEntity, "operation failed"
		h.logger.Warn("deployment refused before starting an attempt", "error", err.Error())
	}
	body := map[string]any{"error": code, "attempt_started": false}
	if errors.Is(err, application.ErrAddressRejected) {
		body["error"], body["field"] = "address_rejected", "address"
	}
	writeJSON(w, status, body)
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

// deployRefusal is the body of a refusal that stopped a send before it touched
// anything. A refused address is the one refusal that names a field, so the
// screen can say it beside the address it was typed in (ADR 0050); every other
// refusal is the stable code of DeploymentFailureCode alone. The attempt
// journal keeps `connection_invalid` for it: the field is a detail of the
// refusal, not a second failure kind.
func deployRefusal(err error) map[string]string {
	if errors.Is(err, application.ErrAddressRejected) {
		return map[string]string{"error": "address_rejected", "field": "address"}
	}
	return map[string]string{"error": deployFailure(err)}
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
