package logic

import (
	"testing"
	"time"

	"github.com/q191201771/lal/pkg/base"
)

type deadlockTestGroupObserver struct{}

func (deadlockTestGroupObserver) CleanupHlsIfNeeded(string, string, string) {}
func (deadlockTestGroupObserver) OnHlsMakeTs(base.HlsMakeTsInfo)            {}
func (deadlockTestGroupObserver) OnRelayPullStart(base.PullStartInfo)       {}
func (deadlockTestGroupObserver) OnRelayPullStop(base.PullStopInfo)         {}

func TestCustomizePublisherStatisticsHaveSessionIdentity(t *testing.T) {
	first := NewCustomizePubSessionContext("voip_first")
	defer first.Dispose()
	second := NewCustomizePubSessionContext("voip_second")
	defer second.Dispose()
	for _, session := range []*CustomizePubSessionContext{first, second} {
		stat := base.Session2StatPub(session)
		if stat.SessionId == "" || stat.SessionId != session.UniqueKey() {
			t.Fatalf("publisher control key %q does not match statistics ID %q", session.UniqueKey(), stat.SessionId)
		}
		if stat.Protocol != base.SessionProtocolCustomizeStr || stat.BaseType != base.SessionBaseTypePubStr {
			t.Fatalf("unexpected custom publisher statistics: %+v", stat)
		}
	}
	if first.UniqueKey() == second.UniqueKey() {
		t.Fatal("custom publishers must have unique IDs")
	}
}

func TestDelCustomizePubSessionWithHLSDoesNotDeadlock(t *testing.T) {
	cfg := Config{}
	cfg.HlsConfig.Enable = true
	group := NewGroup("live", "hls-cleanup", &cfg, GroupOption{}, deadlockTestGroupObserver{})
	session, err := group.AddCustomizePubSession("hls-cleanup")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		group.DelCustomizePubSession(session)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deleting a custom publisher with HLS enabled deadlocked")
	}
}

func TestCustomizePublisherUsesLiveAppGroup(t *testing.T) {
	server := NewServerManager(func(option *Option) {
		option.ConfRawContent = []byte(`{"hls":{"enable":true,"on_demand":true,"url_pattern":"/hls/"}}`)
	})
	streamName := "customize-live-app"
	session, err := server.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}
	if got := server.groupManager.GetGroup("live", streamName); got == nil {
		t.Fatal("custom publisher group must be registered in the live app")
	}
	done := make(chan struct{})
	go func() {
		server.DelCustomizePubSession(session)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deleting a custom publisher from the live app deadlocked")
	}
	if group := server.groupManager.GetGroup("live", streamName); group == nil || group.HasInSession() {
		t.Fatal("custom publisher was not removed from the live app group")
	}
}
