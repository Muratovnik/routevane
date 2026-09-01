package httpapi

import (
	"net/http"
	"strconv"

	"github.com/Muratovnik/routevane/internal/application"
)

// exportList is a one-off render. It deliberately does not add an output or
// issue a subscription: those belong to a persistent consumer, while this
// response belongs only to the explicit download click that requested it.
func (h *handler) exportList(w http.ResponseWriter, r *http.Request, listID string) {
	var request struct {
		FormatID string `json:"format_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.backend.Export(r.Context(), listID, request.FormatID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeExport(w, listID, result)
}

func writeExport(w http.ResponseWriter, listID string, result application.ExportPayload) {
	w.Header().Set("Content-Type", result.Descriptor.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="routevane-`+listID+`-`+result.Format.ID+`.`+result.Descriptor.FileExtension+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Payload)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Payload)
}
