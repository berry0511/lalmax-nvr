package api

import (
	"context"
	"fmt"
	"net/url"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

// Resolve a selected output while keeping the publication's source identity.
// No task is started by catalog polling; unavailable selected output stays offline.
func (h *Handler) homeAssistantVariant(ctx context.Context, cfg storage.HomeAssistantSettings, p storage.HomeAssistantPublication) (string, media.StreamInfo, error) {
	playback, err := h.resolveDerivedPlayback(ctx, p.StreamID, p.TranscodeJobID, "rtsp")
	if err != nil {
		return "", media.StreamInfo{}, fmt.Errorf("derived RTSP unavailable: %w", err)
	}
	u, err := url.Parse(playback.URL.URL)
	if err != nil || u.Scheme != "rtsp" || u.Hostname() == "" {
		return "", media.StreamInfo{}, fmt.Errorf("invalid derived RTSP URL")
	}
	if cfg.RTSPBaseURL != "" {
		base, err := url.Parse(cfg.RTSPBaseURL)
		if err != nil {
			return "", media.StreamInfo{}, err
		}
		u.Host = base.Host
	}
	u.User = nil
	u.Fragment = ""
	return u.String(), *playback.Stream, nil
}
