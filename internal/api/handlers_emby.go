package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/lalmax-pro/lalmax-nvr/internal/emby"
)

func (h *Handler) handleEmbyGetConfig(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	cfg, err := h.embySvc.Config(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Emby configuration")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) handleEmbySaveConfig(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	var cfg emby.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	saved, err := h.embySvc.SaveConfig(r.Context(), cfg)
	if err != nil {
		writeEmbyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *Handler) handleEmbyBrowse(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := h.embySvc.Browse(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeEmbyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *Handler) handleEmbyListItems(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	items, err := h.embySvc.ListItems(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed Emby items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleEmbyAddItem(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	var req struct {
		ID string `json:"emby_item_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.embySvc.AddItem(r.Context(), req.ID)
	if err != nil {
		writeEmbyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) handleEmbyUpdateItem(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	var req struct {
		PublishEnabled *bool `json:"publish_enabled"`
		Loop           *bool `json:"loop"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.embySvc.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Emby item")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "Emby item not found")
		return
	}
	enabled, loop := item.PublishEnabled, item.Loop
	if req.PublishEnabled != nil {
		enabled = *req.PublishEnabled
	}
	if req.Loop != nil {
		loop = *req.Loop
	}
	if err := h.embySvc.SetPublish(r.Context(), item.ID, enabled, loop); err != nil {
		writeEmbyError(w, err)
		return
	}
	item, err = h.embySvc.GetItem(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload Emby item")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleEmbyDeleteItem(w http.ResponseWriter, r *http.Request) {
	if h.embySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "Emby module unavailable")
		return
	}
	if err := h.embySvc.RemoveItem(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeEmbyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func writeEmbyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, emby.ErrNotConfigured):
		writeError(w, http.StatusConflict, "Emby server is not configured")
	case errors.Is(err, emby.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "Emby media item not found")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
