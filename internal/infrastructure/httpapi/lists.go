package httpapi

import (
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

func (h *handler) previewList(w http.ResponseWriter, r *http.Request, listID string) {
	if !decodeEmpty(w, r) {
		return
	}
	preview, err := h.backend.PreviewList(r.Context(), listID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) createCustomList(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	list, err := h.backend.CreateCustomList(r.Context(), request.Title, request.Domains)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"list": list})
}

func (h *handler) updateCustomList(w http.ResponseWriter, r *http.Request, listID string) {
	var request struct {
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	list, err := h.backend.UpdateCustomList(r.Context(), listID, request.Title, request.Domains)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list})
}

// removeList deletes one list from the library (ADR 0029). It asks nothing,
// because a list holds no other object: everything it owned goes with it, and
// a route that names it directly refuses the deletion instead of losing it.
func (h *handler) removeList(w http.ResponseWriter, r *http.Request, listID string) {
	if !decodeEmpty(w, r) {
		return
	}
	if err := h.backend.RemoveList(r.Context(), listID); err != nil {
		h.backendError(w, err)
		return
	}
	writeNoContent(w)
}

func (h *handler) listContents(w http.ResponseWriter, r *http.Request, listID string) {
	contents, err := h.backend.ListContents(r.Context(), listID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contents)
}

func (h *handler) refreshList(w http.ResponseWriter, r *http.Request, listID string) {
	if !decodeEmpty(w, r) {
		return
	}
	summary, err := h.backend.RefreshListByID(r.Context(), listID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refresh": summary})
}

func (h *handler) addListSource(w http.ResponseWriter, r *http.Request, listID string) {
	var request struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	source, err := h.backend.AddListSource(r.Context(), listID, request.URL, domain.FeedFormat(request.Format))
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source": source})
}

func (h *handler) updateListSource(w http.ResponseWriter, r *http.Request, listID, sourceID string) {
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.backend.SetListSourceEnabled(r.Context(), listID, sourceID, request.Enabled); err != nil {
		h.backendError(w, err)
		return
	}
	h.listContents(w, r, listID)
}

func (h *handler) removeListSource(w http.ResponseWriter, r *http.Request, listID, sourceID string) {
	if !decodeEmpty(w, r) {
		return
	}
	if err := h.backend.RemoveListSource(r.Context(), listID, sourceID); err != nil {
		h.backendError(w, err)
		return
	}
	h.listContents(w, r, listID)
}

// setListValues records one verdict over a batch of destinations — domains,
// addresses, networks. The batch exists because an operator states a routes
// file in one action; the application bounds it at 1024 values, and the body
// bound this package applies to every route (maxJSONBytes, 64 KiB) carries a
// full batch of ordinary destinations with room to spare. A larger body is
// refused as too large by decodeJSON rather than silently truncated, so the
// route needs no bound of its own.
func (h *handler) setListValues(w http.ResponseWriter, r *http.Request, listID string) {
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
	if err := h.backend.SetListValues(r.Context(), listID, request.Values, application.DomainVerdict(request.Verdict)); err != nil {
		h.backendError(w, err)
		return
	}
	h.listContents(w, r, listID)
}
