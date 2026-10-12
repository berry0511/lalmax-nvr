package emby

import (
	"context"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	"github.com/stretchr/testify/require"
)

type fallbackRepository struct {
	Repository
	updated chan bool
}

func (r *fallbackRepository) SetEmbyItemPublish(_ context.Context, _ string, enabled, _ bool) error {
	r.updated <- enabled
	return nil
}

type fallbackPuller struct {
	media.HLSPuller
	stopped chan string
}

func (p *fallbackPuller) StopHLSPull(_ context.Context, streamID string) error {
	p.stopped <- streamID
	return nil
}

func TestFinishAfterDurationDisablesPublishEvenWhenPullStopIsAsync(t *testing.T) {
	repo := &fallbackRepository{updated: make(chan bool, 1)}
	puller := &fallbackPuller{stopped: make(chan string, 1)}
	service := &Service{
		db: repo, puller: puller,
		active: map[string]bool{"emby_test": false},
		timers: make(map[string]*time.Timer),
		epochs: map[string]uint64{"emby_test": 7},
	}
	item := storage.EmbyItem{ID: "item", StreamID: "emby_test"}

	service.finishAfterDuration(item, false, 7)

	require.False(t, service.active["emby_test"])
	require.False(t, <-repo.updated)
	select {
	case id := <-puller.stopped:
		require.Equal(t, item.StreamID, id)
	case <-time.After(time.Second):
		t.Fatal("expected the HLS pull to be stopped")
	}
}
