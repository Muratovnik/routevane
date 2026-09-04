package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Muratovnik/routevane/internal/application"
)

func (h *handler) exportConfigTransfer(w http.ResponseWriter, r *http.Request) {
	payload, err := h.backend.ExportConfigTransfer(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="routevane-config.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func readTransferBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	reader := http.MaxBytesReader(w, r.Body, application.ConfigTransferMaxBytes)
	payload, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeTransferError(w, application.NewTransferError("too_large", ""))
			return nil, false
		}
		writeTransferError(w, application.NewTransferError("invalid_json", ""))
		return nil, false
	}
	return payload, true
}

func (h *handler) previewConfigTransfer(w http.ResponseWriter, r *http.Request) {
	payload, ok := readTransferBody(w, r)
	if !ok {
		return
	}
	preview, err := h.backend.PreviewConfigTransfer(payload)
	if err != nil {
		writeTransferBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) applyConfigTransfer(w http.ResponseWriter, r *http.Request) {
	payload, ok := readTransferBody(w, r)
	if !ok {
		return
	}
	if err := application.RejectDuplicateJSONKeys(payload); err != nil {
		writeTransferBackendError(w, err)
		return
	}
	var request struct {
		PreviewDigest string          `json:"preview_digest"`
		Transfer      json.RawMessage `json:"transfer"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeTransferError(w, application.NewTransferError("invalid_json", ""))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeTransferError(w, application.NewTransferError("invalid_json", ""))
		return
	}
	if len(request.Transfer) == 0 || string(request.Transfer) == "null" {
		writeTransferError(w, application.NewTransferError("invalid_shape", "transfer"))
		return
	}
	counts, err := h.backend.ApplyConfigTransfer(r.Context(), request.PreviewDigest, request.Transfer)
	if err != nil {
		writeTransferBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Applied bool                             `json:"applied"`
		Counts  application.ConfigTransferCounts `json:"counts"`
	}{true, counts})
}

func writeTransferBackendError(w http.ResponseWriter, err error) {
	var transfer application.TransferError
	if errors.As(err, &transfer) {
		writeTransferError(w, transfer)
		return
	}
	writeTransferError(w, application.NewTransferError("storage_failed", ""))
}

func writeTransferError(w http.ResponseWriter, e application.TransferError) {
	status := http.StatusUnprocessableEntity
	switch e.Code {
	case "config_transfer_too_large":
		status = http.StatusRequestEntityTooLarge
	case "config_transfer_invalid_json", "config_transfer_invalid_shape":
		status = http.StatusBadRequest
	case "config_transfer_preview_required", "config_transfer_preview_mismatch", "config_transfer_destination_not_empty":
		status = http.StatusConflict
	case "config_transfer_identity_exhausted":
		status = http.StatusServiceUnavailable
	case "config_transfer_storage_failed":
		status = http.StatusInternalServerError
	}
	body := map[string]any{"error": "configuration transfer failed", "code": e.Code}
	if e.Path != "" {
		body["path"] = e.Path
	}
	writeJSON(w, status, body)
}
