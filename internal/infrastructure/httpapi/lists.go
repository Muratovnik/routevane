// The transport for a list and its outputs: the product's own unit and the
// formats bound to it. Composition is resolved on the server, so these
// handlers pass a request through and never decide what a list contains.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

func (h *handler) createList(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
		application.ListComposition
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	list, err := h.backend.CreateList(r.Context(), request.Name, request.ListComposition)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": list})
}

// previewComposition forecasts a composition that has not been created. It
// decodes the same composition fields creation accepts, so a screen forecasts
// exactly what it is about to create rather than a summary of it, and the
// answer costs no stored row. targets is optional: an absent or empty set asks
// about every target in the catalog.
func (h *handler) previewComposition(w http.ResponseWriter, r *http.Request) {
	var request struct {
		application.ListComposition
		Targets []string `json:"targets"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	forecasts, err := h.backend.ForecastComposition(r.Context(), request.ListComposition, request.Targets)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": forecasts})
}

func (h *handler) listLists(w http.ResponseWriter, r *http.Request) {
	cards, err := h.backend.ListCards(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": cards})
}

func (h *handler) getList(w http.ResponseWriter, r *http.Request, id string) {
	list, err := h.backend.List(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	outputs, err := h.backend.OutputCards(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	schedule, err := h.backend.ListSchedule(r.Context(), list)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":            list,
		"outputs":            outputs,
		"resolved":           h.backend.ResolvedServices(list),
		"missing_categories": h.backend.MissingCategories(list),
		"schedule":           schedule,
	})
}

// updateList replaces a list's name and composition. It is a POST rather than
// a PUT because the mutation guard this transport enforces is written for one
// verb, and adding a second would widen it for no gain.
func (h *handler) updateList(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Name string `json:"name"`
		application.ListComposition
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	list, err := h.backend.UpdateList(r.Context(), id, request.Name, request.ListComposition)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": list})
}

// setListArchived takes a list off the shelf or puts it back. The reply is the
// list, so the caller reads the resulting state rather than assuming the verb
// it sent was the state it got.
func (h *handler) setListArchived(w http.ResponseWriter, r *http.Request, id string, archived bool) {
	if !decodeEmpty(w, r) {
		return
	}
	act := h.backend.RestoreList
	if archived {
		act = h.backend.ArchiveList
	}
	list, err := act(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": list})
}

// updateListSchedule records a list's own rule. An empty interval is not a
// missing value: it is the list going back to following the service-wide
// default, which is a different statement from naming the default's value.
func (h *handler) updateListSchedule(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		RefreshInterval application.RefreshInterval `json:"refresh_interval"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	list, err := h.backend.SetListRefreshInterval(r.Context(), id, request.RefreshInterval)
	if err != nil {
		h.backendError(w, err)
		return
	}
	schedule, err := h.backend.ListSchedule(r.Context(), list)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": list, "schedule": schedule})
}

// addOutput binds a list to one format. It has no credential yet: the build
// route issues the subscription only after a valid artifact is published.
func (h *handler) addOutput(w http.ResponseWriter, r *http.Request, listID string) {
	var request struct {
		TargetID string `json:"target_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	created, err := h.backend.AddOutput(r.Context(), listID, request.TargetID)
	if err != nil {
		if errors.Is(err, application.ErrOutputExists) {
			writeJSON(w, http.StatusConflict, map[string]any{"output": created.Output, "error": "output exists"})
			return
		}
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"output": created.Output})
}

func (h *handler) getOutput(w http.ResponseWriter, r *http.Request, id string) {
	output, err := h.backend.Output(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

// setOutputDevice changes only where future automatic deliveries go. An empty
// identity detaches the device while the output, subscription and immutable
// artifact history stay intact.
func (h *handler) setOutputDevice(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		DeviceID string `json:"device_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	output, err := h.backend.SetOutputDevice(r.Context(), id, request.DeviceID)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}

func (h *handler) refresh(w http.ResponseWriter, r *http.Request, id string) {
	if !decodeEmpty(w, r) {
		return
	}
	summaries, err := h.backend.Refresh(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refresh": summaries})
}

func (h *handler) build(w http.ResponseWriter, r *http.Request, id string) {
	if !decodeEmpty(w, r) {
		return
	}
	result, err := h.backend.Build(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	token, err := h.backend.IssueSubscription(r.Context(), id)
	if err != nil && !errors.Is(err, application.ErrSubscriptionExists) {
		h.backendError(w, err)
		return
	}
	response := struct {
		application.SafePublishedBuild
		SubscriptionURL string `json:"subscription_url,omitempty"`
	}{SafePublishedBuild: result.SafeProjection()}
	if token != "" {
		response.SubscriptionURL = h.origin + "/v1/subscriptions/" + token
	}
	writeJSON(w, http.StatusOK, response)
}
