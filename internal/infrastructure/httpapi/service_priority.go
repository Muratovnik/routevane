package httpapi

import (
	"net/http"
)

// setDefaultPriority replaces the complete library ordering. The application
// validates that the request is one full permutation of the currently
// available service ids; this handler only owns the JSON shape and response.
func (h *handler) setDefaultPriority(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DefaultPriority []string `json:"default_priority"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.backend.SetDefaultPriority(r.Context(), request.DefaultPriority); err != nil {
		h.backendError(w, err)
		return
	}
	priority, err := h.backend.DefaultPriority(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"default_priority": priority})
}
