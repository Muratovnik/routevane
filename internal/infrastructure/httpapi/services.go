package httpapi

import (
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

func (h *handler) previewService(w http.ResponseWriter, r *http.Request, serviceID string) {
	if !decodeEmpty(w, r) {
		return
	}
	preview, err := h.backend.PreviewService(r.Context(), serviceID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) createCustomService(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	service, err := h.backend.CreateCustomService(r.Context(), request.Title, request.Domains)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"list": service})
}

func (h *handler) updateCustomService(w http.ResponseWriter, r *http.Request, serviceID string) {
	var request struct {
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	service, err := h.backend.UpdateCustomService(r.Context(), serviceID, request.Title, request.Domains)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": service})
}

// removeService deletes one list from the library (ADR 0029). It asks nothing,
// because a list holds no other object: everything it owned goes with it, and
// a route that names it directly refuses the deletion instead of losing it.
func (h *handler) removeService(w http.ResponseWriter, r *http.Request, serviceID string) {
	if !decodeEmpty(w, r) {
		return
	}
	if err := h.backend.RemoveService(r.Context(), serviceID); err != nil {
		h.backendError(w, err)
		return
	}
	writeNoContent(w)
}

func (h *handler) serviceContents(w http.ResponseWriter, r *http.Request, serviceID string) {
	contents, err := h.backend.ServiceContents(r.Context(), serviceID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contents)
}

func (h *handler) refreshService(w http.ResponseWriter, r *http.Request, serviceID string) {
	if !decodeEmpty(w, r) {
		return
	}
	summary, err := h.backend.RefreshServiceByID(r.Context(), serviceID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refresh": summary})
}

func (h *handler) addServiceSource(w http.ResponseWriter, r *http.Request, serviceID string) {
	var request struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	source, err := h.backend.AddServiceSource(r.Context(), serviceID, request.URL, domain.FeedFormat(request.Format))
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source": source})
}

func (h *handler) updateServiceSource(w http.ResponseWriter, r *http.Request, serviceID, sourceID string) {
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.backend.SetServiceSourceEnabled(r.Context(), serviceID, sourceID, request.Enabled); err != nil {
		h.backendError(w, err)
		return
	}
	h.serviceContents(w, r, serviceID)
}

func (h *handler) removeServiceSource(w http.ResponseWriter, r *http.Request, serviceID, sourceID string) {
	if !decodeEmpty(w, r) {
		return
	}
	if err := h.backend.RemoveServiceSource(r.Context(), serviceID, sourceID); err != nil {
		h.backendError(w, err)
		return
	}
	h.serviceContents(w, r, serviceID)
}

// setServiceValues records one verdict over a batch of destinations — domains,
// addresses, networks. The batch exists because an operator states a routes
// file in one action; the application bounds it at 1024 values, and the body
// bound this package applies to every route (maxJSONBytes, 64 KiB) carries a
// full batch of ordinary destinations with room to spare. A larger body is
// refused as too large by decodeJSON rather than silently truncated, so the
// route needs no bound of its own.
func (h *handler) setServiceValues(w http.ResponseWriter, r *http.Request, serviceID string) {
	var request struct {
		Values  []string `json:"values"`
		Verdict string   `json:"verdict"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Values) == 0 {
		writeError(w, http.StatusBadRequest, "no destinations")
		return
	}
	if err := h.backend.SetServiceValues(r.Context(), serviceID, request.Values, application.DomainVerdict(request.Verdict)); err != nil {
		h.backendError(w, err)
		return
	}
	h.serviceContents(w, r, serviceID)
}
