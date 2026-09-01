// The device registry's transport. What an operator installed is a fact about
// their own network, so it is its own surface rather than part of the catalog's.
package httpapi

import (
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

// listDevices answers with the registry and whether the platform can keep a
// credential. Credential-free deployers can still offer unattended delivery;
// their requirements travel through the deployment catalog.
func (h *handler) listDevices(w http.ResponseWriter, r *http.Request) {
	cards, err := h.backend.DeviceCards(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices":                cards,
		"secret_store_available": h.backend.SecretStoreAvailable(),
	})
}

func (h *handler) createDevice(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TargetID  string `json:"target_id"`
		Name      string `json:"name"`
		Address   string `json:"address"`
		Account   string `json:"account"`
		Interface string `json:"interface"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	device, err := h.backend.RegisterDevice(r.Context(), request.TargetID, request.Name, request.Address, request.Account, request.Interface)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"device": device})
}

func (h *handler) updateDevice(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		Account   string `json:"account"`
		Interface string `json:"interface"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	device, err := h.backend.UpdateDevice(r.Context(), id, request.Name, request.Address, request.Account, request.Interface)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": device})
}

func (h *handler) forgetDevice(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.backend.ForgetDevice(r.Context(), id); err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"forgotten": id})
}

// autoDelivery turns unattended delivery on or off. Turning it on carries the
// credential in the request body and nowhere else: the reply states the device,
// never the secret, and the request is never logged.
func (h *handler) autoDelivery(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Enabled    bool   `json:"enabled"`
		Credential string `json:"credential"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	var device application.Device
	var err error
	if request.Enabled {
		device, err = h.backend.EnableAutoDelivery(r.Context(), id, request.Credential)
	} else {
		device, err = h.backend.DisableAutoDelivery(r.Context(), id)
	}
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": device})
}
