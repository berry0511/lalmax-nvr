package ban

import (
	"context"
	"errors"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/auth"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	"github.com/q191201771/lal/pkg/base"
)

type testStreamBanRepository struct {
	bans map[string]*storage.StreamBan
	err  error
}

func (r *testStreamBanRepository) GetStreamBan(_ context.Context, id string) (*storage.StreamBan, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.bans[id], nil
}
func (r *testStreamBanRepository) InsertStreamBan(_ context.Context, ban *storage.StreamBan) error {
	if r.bans == nil {
		r.bans = make(map[string]*storage.StreamBan)
	}
	r.bans[ban.StreamID] = ban
	return nil
}
func (r *testStreamBanRepository) DeleteStreamBan(_ context.Context, id string) error {
	delete(r.bans, id)
	return nil
}
func (r *testStreamBanRepository) ListStreamBans(_ context.Context) ([]storage.StreamBan, error) {
	return nil, nil
}

func TestRTMPPublishAuthDefaultsToRequired(t *testing.T) {
	mediaAuth := auth.NewMediaAuthorizer()
	mediaAuth.SetPublishTokens("rtmp", map[string]string{"camera1": "expected-key"})
	m := NewManager(&testStreamBanRepository{}, mediaAuth)
	publisher := base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			Protocol:   base.SessionProtocolRtmpStr,
			StreamName: "camera1",
		},
	}
	if err := m.OnPubStart(publisher); err == nil {
		t.Fatal("default RTMP publishing must reject missing stream_key")
	}
	publisher.UrlParam = "stream_key=incorrect"
	if err := m.OnPubStart(publisher); err == nil {
		t.Fatal("default RTMP publishing must reject invalid stream_key")
	}
	publisher.UrlParam = "stream_key=expected-key"
	if err := m.OnPubStart(publisher); err != nil {
		t.Fatalf("expected authorized RTMP publisher, got %v", err)
	}
}

func TestRTMPAnonymousPublishStillEnforcesBans(t *testing.T) {
	repo := &testStreamBanRepository{}
	mediaAuth := auth.NewMediaAuthorizer()
	mediaAuth.SetPublishTokens("rtmp", map[string]string{"camera1": "expected-key"})
	m := NewManager(repo, mediaAuth)
	m.SetRTMPPublishAuthEnabled(false)
	publisher := base.PubStartInfo{SessionEventCommonInfo: base.SessionEventCommonInfo{
		Protocol: base.SessionProtocolRtmpStr, StreamName: "camera1",
	}}
	for _, param := range []string{"", "stream_key=incorrect", "stream_key=%not-a-valid-escape"} {
		publisher.UrlParam = param
		if err := m.OnPubStart(publisher); err != nil {
			t.Fatalf("anonymous RTMP publisher with params %q rejected: %v", param, err)
		}
	}

	repo.bans = map[string]*storage.StreamBan{
		"camera1": {StreamID: "camera1", Reason: "blocked"},
	}
	if err := m.OnPubStart(publisher); err == nil {
		t.Fatal("banned RTMP stream must be rejected even when publish auth is disabled")
	}

	repo.bans = nil
	repo.err = errors.New("database unavailable")
	if err := m.OnPubStart(publisher); err == nil {
		t.Fatal("RTMP publisher must be rejected if ban check fails")
	}
}

func TestRTMPAuthOptOutDoesNotDisableRTSPAuthentication(t *testing.T) {
	repo := &testStreamBanRepository{}
	mediaAuth := auth.NewMediaAuthorizer()
	mediaAuth.SetPublishTokens("media", map[string]string{"camera1": "shared-key"})
	m := NewManager(repo, mediaAuth)
	m.SetRTMPPublishAuthEnabled(false)
	rtsp := base.PubStartInfo{SessionEventCommonInfo: base.SessionEventCommonInfo{
		Protocol: base.SessionProtocolRtspStr, StreamName: "camera1",
	}}
	if err := m.OnPubStart(rtsp); err == nil {
		t.Fatal("RTSP must still require configured shared publish token")
	}
	rtsp.UrlParam = "token=shared-key"
	if err := m.OnPubStart(rtsp); err != nil {
		t.Fatalf("RTSP shared token should remain accepted, got %v", err)
	}
}
