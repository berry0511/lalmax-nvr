package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

var traceIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func (h *Handler) handleAPIObservability(w http.ResponseWriter, r *http.Request) {
	window := parseObservabilityWindow(r.URL.Query().Get("window"), 5*time.Minute)
	series := parseObservabilityWindow(r.URL.Query().Get("series"), time.Hour)
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = value
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, h.apiObserver.Aggregator().Snapshot(window, series, limit))
}

func (h *Handler) traceRepository(w http.ResponseWriter) storage.TraceRepository {
	repo, ok := h.db.(storage.TraceRepository)
	if !ok || repo == nil {
		writeError(w, http.StatusServiceUnavailable, "local trace storage unavailable")
		return nil
	}
	return repo
}

func (h *Handler) handleListObservabilityTraces(w http.ResponseWriter, r *http.Request) {
	repo := h.traceRepository(w)
	if repo == nil {
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = value
	}
	traces, err := repo.ListTraces(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list traces")
		return
	}
	template := ""
	if h.config != nil {
		template = strings.TrimSpace(h.config.Observability.TraceUIURLTemplate)
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": traces, "external_trace_url_template": template})
}

func (h *Handler) handleGetObservabilityTrace(w http.ResponseWriter, r *http.Request) {
	traceID := strings.ToLower(chi.URLParam(r, "traceID"))
	if !traceIDPattern.MatchString(traceID) {
		writeError(w, http.StatusBadRequest, "trace_id must be 32 hexadecimal characters")
		return
	}
	repo := h.traceRepository(w)
	if repo == nil {
		return
	}
	spans, err := repo.GetTraceSpans(r.Context(), traceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trace")
		return
	}
	if len(spans) == 0 {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trace_id": traceID, "spans": spans})
}

func parseObservabilityWindow(raw string, fallback time.Duration) time.Duration {
	switch raw {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	default:
		return fallback
	}
}
