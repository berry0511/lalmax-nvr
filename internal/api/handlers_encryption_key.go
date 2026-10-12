package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lalmax-pro/lalmax-nvr/internal/config"
)

func (h *Handler) handleGetEncryptionKeySettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    config.HasKey(),
		"managed_by": config.EncryptionKeySource(),
	})
}

func (h *Handler) handleSetEncryptionKey(w http.ResponseWriter, r *http.Request) {
	if h.configPath == "" {
		writeError(w, http.StatusInternalServerError, "config path is not available")
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	if err := config.CreateWebEncryptionKey(h.configPath+".encryption-key", body.Key); err != nil {
		if errors.Is(err, config.ErrEncryptionKeyConfigured) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, config.ErrInvalidEncryptionKey) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save encryption key")
		return
	}
	h.logOperation(r, "config.update", "encryption_key", "", "success", "encryption key configured", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "updated",
		"enabled":    true,
		"managed_by": "web",
	})
}
