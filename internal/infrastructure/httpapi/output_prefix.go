package httpapi

import (
	"errors"
	"github.com/Muratovnik/routevane/internal/application"
	"net/http"
)

func (h *handler) setOutputFQDNPrefix(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Prefix string `json:"fqdn_group_prefix"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	output, err := h.backend.SetOutputFQDNPrefix(r.Context(), id, request.Prefix)
	if errors.Is(err, application.ErrFQDNPrefix) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_fqdn_prefix"})
		return
	}
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}
