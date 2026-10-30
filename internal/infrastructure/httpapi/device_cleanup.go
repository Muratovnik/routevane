// The device side of retirement (ADR 0060): what a format or a profile
// delivered to devices, and the refusal a cleanup answers with. A cleanup runs
// a send's device lifecycle, so its refusals reuse the send's stable codes.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

// decodeRetirement reads a retirement request: `{}` as before, or
// `{"remove_from_devices": true}` to remove what the retired formats delivered
// first. A JSON null is refused as it always was.
func decodeRetirement(w http.ResponseWriter, r *http.Request) (cleanDevices, ok bool) {
	var request *struct {
		RemoveFromDevices bool `json:"remove_from_devices"`
	}
	if !decodeJSON(w, r, &request) {
		return false, false
	}
	if request == nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false, false
	}
	return request.RemoveFromDevices, true
}

// deviceObjects answers what the ownership ledgers say Routevane delivered,
// by surface, and whether a cleanup could reach every one of them. It never
// contacts a device.
func (h *handler) deviceObjects(w http.ResponseWriter, r *http.Request, read func(context.Context, string) ([]application.DeviceObjects, error), id string) {
	objects, err := read(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	if objects == nil {
		objects = []application.DeviceObjects{}
	}
	cleanable := true
	for _, object := range objects {
		cleanable = cleanable && object.Cleanable
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_objects": objects, "cleanable": cleanable})
}

// retirementError answers a refused cleanup with its stable code, the step and
// the surface it stopped at, the attempts it made, and an explicit statement
// that nothing was retired. Any other error keeps the retirement's own answer.
func (h *handler) retirementError(w http.ResponseWriter, route string, err error) {
	var refusal *application.DeviceCleanupRefusal
	if !errors.As(err, &refusal) {
		h.backendError(w, err)
		return
	}
	code := refusal.Code()
	body := map[string]any{"error": code, "code": code, "retired": false, "device_cleanup": cleanupList(refusal.Cleanups)}
	if len(refusal.Blocked) > 0 {
		body["device_objects"] = refusal.Blocked
	}
	if refusal.Objects != nil {
		body["device_object"] = refusal.Objects
	}
	if refusal.Step != "" {
		body["step"] = refusal.Step
	}
	if field := deployRefusalField(code); field != "" {
		body["field"] = field
	}
	status := deployCodeStatus(code)
	if code == "device_cleanup_unavailable" {
		status = http.StatusConflict
	}
	// The refusal itself can carry a device's answer and the address it was
	// sent to, so only the route, the code and the step are logged.
	h.logger.Warn("device cleanup refused", "route", route, "code", code, "step", refusal.Step)
	writeJSON(w, status, body)
}

// cleanupList keeps an empty list a list on the wire.
func cleanupList(cleanups []application.DeviceCleanupAttempt) []application.DeviceCleanupAttempt {
	if cleanups == nil {
		return []application.DeviceCleanupAttempt{}
	}
	return cleanups
}
