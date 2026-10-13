package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lalmax-pro/lalmax-nvr/internal/model"
	"github.com/lalmax-pro/lalmax-nvr/internal/wsstream"
)

// --- WebSocket streaming endpoint ---

// handleStreamWS handles GET /api/cameras/{id}/stream/ws
// It upgrades the HTTP connection to a WebSocket and streams binary-encoded
// video frames (CodecInfo first, then VideoFrame messages).
func (h *Handler) handleStreamWS(w http.ResponseWriter, r *http.Request) {
	id := playResourceID(r)
	if isStreamPlayRoute(r) {
		if camID := h.cameraIDForStream(r.Context(), id); camID != "" {
			id = camID
		}
	}

	if h.wsMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "WebSocket streaming not available")
		return
	}

	// Check camera exists
	cam, err := h.db.GetCamera(r.Context(), id)
	if err != nil {
		slog.Error("WS: failed to get camera", "camera_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get camera")
		return
	}
	if cam == nil {
		writeError(w, http.StatusNotFound, "camera not found")
		return
	}

	// On-demand registration: if WebSocket stream not registered, register it
	if !h.wsMgr.IsActive(id) {
		if h.camMgr == nil {
			writeError(w, http.StatusNotFound, "WebSocket stream not active")
			return
		}
		rec := h.camMgr.GetRecorder(id)
		var codec model.Format
		var sps, pps, vps []byte
		var hub *model.StreamHub
		if rec != nil {
			codec, sps, pps, vps = getCodecParams(rec)
			hub = getStreamHub(rec)
		} else if h.groupStreamSource != nil {
			codec, sps, pps, vps, hub, _ = h.groupStreamSource.StreamInfo(id)
		}
		if hub == nil {
			slog.Warn("WS: live frame source not running", "camera_id", id)
			writeError(w, http.StatusBadRequest, "camera live stream not running")
			return
		}
		slog.Info("WS: on-demand register", "camera_id", id, "codec", codec, "has_sps", sps != nil, "has_pps", pps != nil)
		if sps == nil || pps == nil {
			writeError(w, http.StatusServiceUnavailable, "waiting for video stream")
			return
		}

		if err := h.wsMgr.RegisterStream(id, codec, sps, pps, vps, hub); err != nil {
			if !errors.Is(err, wsstream.ErrStreamExists) {
				slog.Error("WS: failed to register", "camera_id", id, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to register WebSocket stream")
				return
			}
		} else {
			// Fresh registration — attach audio track if the recorder has one.
			if rec != nil {
				if aCodec, aConfig := getAudioParams(rec); aCodec != "" {
					if aci, ok := wsstream.BuildAudioCodecInfo(aCodec, aConfig); ok {
						if err := h.wsMgr.SetAudioConfig(id, aci); err != nil {
							slog.Warn("WS: failed to attach audio", "camera_id", id, "error", err)
						}
					} else {
						slog.Warn("WS: unusable audio config", "camera_id", id, "codec", aCodec)
					}
				}
			}
		}
	}

	slog.Info("WS: serving", "camera_id", id)

	// Serve WebSocket stream (blocks until client disconnects)
	if err := h.wsMgr.ServeWS(id, w, r); err != nil {
		if errors.Is(err, wsstream.ErrStreamNotActive) {
			writeError(w, http.StatusNotFound, "WebSocket stream not active")
			return
		}
		if errors.Is(err, wsstream.ErrMaxViewers) {
			writeError(w, http.StatusServiceUnavailable, "maximum WebSocket viewers reached")
			return
		}
		slog.Error("WS: serve failed", "camera_id", id, "error", err, "error_type", fmt.Sprintf("%T", err))
	}
}
