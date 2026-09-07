// The transport for a profile and its outputs: the product's own unit and the
// formats bound to it. Composition is resolved on the server, so these
// handlers pass a request through and never decide what a profile contains.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

func (h *handler) createProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
		application.ProfileComposition
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	profile, err := h.backend.CreateProfile(r.Context(), request.Name, request.ProfileComposition)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": profile})
}

// previewComposition forecasts a composition that has not been created. It
// decodes the same composition fields creation accepts, so a screen forecasts
// exactly what it is about to create rather than a summary of it, and the
// answer costs no stored row. targets is optional: an absent or empty set asks
// about every target in the catalog.
func (h *handler) previewComposition(w http.ResponseWriter, r *http.Request) {
	var request struct {
		application.ProfileComposition
		Targets []string `json:"targets"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	forecasts, err := h.backend.ForecastComposition(r.Context(), request.ProfileComposition, request.Targets)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": forecasts})
}

func (h *handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	cards, err := h.backend.ProfileCards(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": cards})
}

func (h *handler) getProfile(w http.ResponseWriter, r *http.Request, id string) {
	profile, err := h.backend.Profile(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	outputs, err := h.backend.OutputCards(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	schedule, err := h.backend.ProfileSchedule(r.Context(), profile)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":            profile,
		"outputs":            outputs,
		"resolved":           h.backend.ResolvedLists(profile),
		"missing_categories": h.backend.MissingCategories(profile),
		"schedule":           schedule,
	})
}

// updateProfile replaces a profile's name and composition. It is a POST rather than
// a PUT because the mutation guard this transport enforces is written for one
// verb, and adding a second would widen it for no gain.
func (h *handler) updateProfile(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Name string `json:"name"`
		application.ProfileComposition
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	profile, err := h.backend.UpdateProfile(r.Context(), id, request.Name, request.ProfileComposition)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

// setProfileArchived takes a profile off the shelf or puts it back. The reply is the
// profile, so the caller reads the resulting state rather than assuming the verb
// it sent was the state it got.
func (h *handler) setProfileArchived(w http.ResponseWriter, r *http.Request, id string, archived bool) {
	if !decodeEmpty(w, r) {
		return
	}
	act := h.backend.RestoreProfile
	if archived {
		act = h.backend.ArchiveProfile
	}
	profile, err := act(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

// updateProfileSchedule records a profile's own rule. An empty interval is not a
// missing value: it is the profile going back to following the service-wide
// default, which is a different statement from naming the default's value.
func (h *handler) updateProfileSchedule(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		RefreshInterval application.RefreshInterval `json:"refresh_interval"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	profile, err := h.backend.SetProfileRefreshInterval(r.Context(), id, request.RefreshInterval)
	if err != nil {
		h.backendError(w, err)
		return
	}
	schedule, err := h.backend.ProfileSchedule(r.Context(), profile)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "schedule": schedule})
}

// addOutput binds a profile to one format. It has no credential yet: the build
// profile issues the subscription only after a valid artifact is published.
func (h *handler) addOutput(w http.ResponseWriter, r *http.Request, profileID string) {
	var request struct {
		TargetID string `json:"target_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	created, err := h.backend.AddOutput(r.Context(), profileID, request.TargetID)
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
