package httpapi

import (
	"errors"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

func (h *handler) setOutputKeepAddresses(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Keep *bool `json:"keep_addresses"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	// An absent value is refused rather than read as false: a client that
	// misspelled the field must not switch the setting off.
	if request.Keep == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_keep_addresses"})
		return
	}
	output, err := h.backend.SetOutputKeepAddresses(r.Context(), id, *request.Keep)
	if errors.Is(err, application.ErrKeepAddresses) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_keep_addresses"})
		return
	}
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}
