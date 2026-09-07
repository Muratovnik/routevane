// The transport for categories. A category is operator-owned over the shipped
// catalog seed (ADR 0028): these handlers pass a request through and never
// decide what a category contains, because the merge with the catalog is the
// server's answer and a screen holding a copy of it would drift.
package httpapi

import (
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

func (h *handler) createCategory(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title string   `json:"title"`
		Lists []string `json:"lists"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	category, err := h.backend.CreateCategory(r.Context(), request.Title, request.Lists)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": category})
}

// updateCategory carries a partial edit. Both fields are pointers because an
// absent field and an empty one are different requests: omitting lists
// leaves membership alone, while sending an empty array clears it. Lists is
// the complete desired membership; the server computes the overlay difference
// against the catalog, so a stale client copy cannot store a verdict the
// operator never gave.
func (h *handler) updateCategory(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Title *string   `json:"title"`
		Lists *[]string `json:"lists"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	category, err := h.backend.UpdateCategory(r.Context(), id, application.CategoryUpdate{Title: request.Title, Lists: request.Lists})
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": category})
}

// removeCategory carries the one question deleting a category asks: what
// becomes of the lists it held. The answer is required rather than defaulted —
// "detach" and "delete" are different outcomes for the operator's data, and a
// request that forgot to say which is a request the server must not guess at.
// An unknown answer is refused by the application, which owns the two words.
func (h *handler) removeCategory(w http.ResponseWriter, r *http.Request, id string) {
	// This key already says what ADR 0039 renames things to: a category holds
	// lists, and these are those lists, not the routes the same word means
	// elsewhere in this API. It survives the swap unchanged.
	var request struct {
		Lists string `json:"lists"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.backend.RemoveCategory(r.Context(), id, application.CategoryListDisposition(request.Lists)); err != nil {
		h.backendError(w, err)
		return
	}
	writeNoContent(w)
}
