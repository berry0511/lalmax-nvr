package api

import (
	"context"
	"errors"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/transcode"
)

type playbackTargetStub struct {
	source transcode.SourceRef
	id     string
	err    error
	got    []string
}

func (s *playbackTargetStub) PlaybackTarget(_ context.Context, sourceID, jobID string) (transcode.SourceRef, string, error) {
	s.got = []string{sourceID, jobID}
	return s.source, s.id, s.err
}

type derivedPlaybackStub struct {
	source transcode.SourceRef
	id     string
	url    *media.PlayURL
	err    error
	calls  int
}

func (s *derivedPlaybackStub) DerivedPlayURL(_ context.Context, source transcode.SourceRef, id, _ string) (*media.PlayURL, error) {
	s.calls++
	if source != s.source || id != s.id {
		return nil, errors.New("unexpected source or output")
	}
	return s.url, s.err
}

func TestResolveDerivedPlaybackUsesSelectedSourceAndRunningOutput(t *testing.T) {
	source := transcode.SourceRef{EngineID: "embedded", AppName: "live", StreamID: "camera-a"}
	service := &playbackTargetStub{source: source, id: "physical-a"}
	provider := &derivedPlaybackStub{source: source, id: "physical-a", url: &media.PlayURL{URL: "rtsp://nvr/live/physical-a"}}
	engine := &stubMediaEngine{streamsByID: map[string]*media.StreamInfo{"physical-a": {StreamID: "physical-a", Active: true}}}

	got, err := resolveDerivedPlayback(context.Background(), "camera-a", "low", "rtsp", service, provider, engine)
	if err != nil {
		t.Fatal(err)
	}
	if service.got[0] != "camera-a" || service.got[1] != "low" {
		t.Fatalf("wrong logical target: %v", service.got)
	}
	if got.Source != source || got.OutputID != "physical-a" || got.URL.URL != "rtsp://nvr/live/physical-a" || got.Stream == nil || !got.Stream.Active {
		t.Fatalf("unexpected resolved playback: %+v", got)
	}
}

func TestResolveDerivedPlaybackFailsClosedWhenOutputIsOffline(t *testing.T) {
	source := transcode.SourceRef{EngineID: "embedded", AppName: "live", StreamID: "camera-a"}
	service := &playbackTargetStub{source: source, id: "physical-a"}
	provider := &derivedPlaybackStub{source: source, id: "physical-a", url: &media.PlayURL{URL: "rtsp://nvr/live/physical-a"}}
	engine := &stubMediaEngine{streamsByID: map[string]*media.StreamInfo{"physical-a": {StreamID: "physical-a", Active: false}}}

	got, err := resolveDerivedPlayback(context.Background(), "camera-a", "low", "rtsp", service, provider, engine)
	if err == nil || got != nil {
		t.Fatalf("offline selected output must fail without fallback: got=%+v err=%v", got, err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected selected variant URL lookup, got %d calls", provider.calls)
	}
}

func TestResolveDerivedPlaybackDoesNotBuildURLAfterTargetFailure(t *testing.T) {
	service := &playbackTargetStub{err: &transcode.Error{Code: "job_not_found", Message: "output does not belong to this source"}}
	provider := &derivedPlaybackStub{}
	engine := &stubMediaEngine{}

	got, err := resolveDerivedPlayback(context.Background(), "camera-a", "low", "rtsp", service, provider, engine)
	if err == nil || got != nil {
		t.Fatalf("invalid source/job pair must fail: got=%+v err=%v", got, err)
	}
	if provider.calls != 0 {
		t.Fatalf("play URL created for invalid source/job pair: %d calls", provider.calls)
	}
}
