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
// a refusal adds its stable code and step, and the audit record this returns
// carries identities and outcomes.
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
				refusal := deployRefusal(err)
				h.logDeployRefusal(false, refusal["error"], refusal["step"])
				writeJSON(w, deployStatus(err), refusal)
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
				refusal := deployRefusal(err)
				h.logDeployRefusal(true, refusal["error"], refusal["step"])
				writeJSON(w, deployStatus(err), refusal)
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
		h.logDeployRefusal(true, attempt.Error, attempt.Result.FailedStep())
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
	h.logDeployRefusal(true, body["error"].(string), "")
	writeJSON(w, status, body)
}

// logDeployRefusal records one refused preview or send by its stable code and
// the step it stopped at. The error itself stays out of this line: a device's
// own answer can be inside it, and so can the address it was sent to.
func (h *handler) logDeployRefusal(confirmed bool, code, step string) {
	h.logger.Warn("deployment refused", "route", routeDeploy, "confirmed", confirmed, "code", code, "step", step)
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
		if field := deployRefusalField(attempt.Error); field != "" {
			body["field"] = field
		}
	}
	if attempt.Status == application.DeploymentAttemptFailed {
		if step := attempt.Result.FailedStep(); step != "" {
			body["step"] = step
		}
	}
	return body
}

// deployFailure is the stable code a screen shows. The underlying message can
// name a device answer, so it is logged rather than returned.
func deployFailure(err error) string {
	return application.DeploymentFailureCode(err)
}

// deployRefusal is the body of a refusal that stopped a send or a preview
// before it changed anything. A refusal about one connection field names it,
// so the screen can say it beside that field (ADR 0050): a refused address, a
// password the device rejected, an interface the device does not have. Every
// refusal is otherwise the stable code of DeploymentFailureCode, with the step
// it stopped at when the preview names one. The attempt journal keeps
// `connection_invalid` for a refused address: the field is a detail of the
// refusal, not a second failure kind.
func deployRefusal(err error) map[string]string {
	if errors.Is(err, application.ErrAddressRejected) {
		return map[string]string{"error": "address_rejected", "field": "address"}
	}
	code := deployFailure(err)
	body := map[string]string{"error": code}
	if field := deployRefusalField(code); field != "" {
		body["field"] = field
	}
	if step := application.RefusalStep(err); step != "" {
		body["step"] = step
	}
	return body
}

// deployRefusalField is the connection field a device refusal is about.
func deployRefusalField(code string) string {
	switch code {
	case "device_auth_rejected":
		return "credential"
	case "device_interface_missing":
		return "interface"
	default:
		return ""
	}
}

// deployCodeStatus is the status of a stable failure code. A request the route
// cannot act on is 400; a device that rejected one named connection field is
// 422, like a refused address, because the operator must change that field;
// everything else the device or its state refused is 409.
func deployCodeStatus(code string) int {
	switch code {
	case "connection_invalid", "confirmation_required", "deployer_unavailable":
		return http.StatusBadRequest
	case "device_auth_rejected", "device_interface_missing":
		return http.StatusUnprocessableEntity
	default:
		return http.StatusConflict
	}
}

// deployStatus gives a refusal that stopped a send before its attempt the same
// status the device routes give it. A refused address is checked first because
// its error also wraps ErrConnectionInvalid, whose status is 400: the address
// answers 422 on every route that names it (ADR 0050).
func deployStatus(err error) int {
	switch {
	case errors.Is(err, application.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, application.ErrAddressRejected):
		return http.StatusUnprocessableEntity
	case errors.Is(err, application.ErrDeployComposition):
		return http.StatusBadRequest
	default:
		return deployCodeStatus(deployFailure(err))
	}
}
