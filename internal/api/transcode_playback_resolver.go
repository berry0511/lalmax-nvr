package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/transcode"
)

var errDerivedPlaybackUnavailable = errors.New("derived playback unavailable")

type transcodePlaybackTargetService interface {
	PlaybackTarget(context.Context, string, string) (transcode.SourceRef, string, error)
}

type resolvedDerivedPlayback struct {
	Source   transcode.SourceRef
	OutputID string
	URL      *media.PlayURL
	Stream   *media.StreamInfo
}

// resolveDerivedPlayback is shared by NVR preview and HA publication. Callers
// must authorize the logical source before calling; this method never starts a
// job and never falls back to the original stream when the selected output is
// unavailable.
func resolveDerivedPlayback(
	ctx context.Context,
	sourceID, jobID, protocol string,
	service transcodePlaybackTargetService,
	provider derivedPlaybackProvider,
	engine media.Engine,
) (*resolvedDerivedPlayback, error) {
	if service == nil || provider == nil || engine == nil {
		return nil, errDerivedPlaybackUnavailable
	}
	source, outputID, err := service.PlaybackTarget(ctx, sourceID, jobID)
	if err != nil {
		return nil, err
	}
	play, err := provider.DerivedPlayURL(ctx, source, outputID, protocol)
	if err != nil || play == nil || play.URL == "" {
		return nil, fmt.Errorf("%w: output URL unavailable", errDerivedPlaybackUnavailable)
	}
	stream, err := engine.GetStream(ctx, outputID)
	if err != nil || stream == nil || !stream.Active {
		return nil, fmt.Errorf("%w: output is offline", errDerivedPlaybackUnavailable)
	}
	return &resolvedDerivedPlayback{Source: source, OutputID: outputID, URL: play, Stream: stream}, nil
}

func (h *Handler) resolveDerivedPlayback(ctx context.Context, sourceID, jobID, protocol string) (*resolvedDerivedPlayback, error) {
	if h.transcodeService == nil || h.transcodePlayback == nil || h.mediaEngine == nil {
		return nil, errDerivedPlaybackUnavailable
	}
	return resolveDerivedPlayback(ctx, sourceID, jobID, protocol, h.transcodeService, h.transcodePlayback, h.mediaEngine)
}

func writeDerivedPlaybackError(w http.ResponseWriter, err error, publicMessage string) {
	var domain *transcode.Error
	if errors.As(err, &domain) {
		transcodeAPIError(w, err)
		return
	}
	writeError(w, http.StatusServiceUnavailable, publicMessage)
}
