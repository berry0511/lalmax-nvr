package rtc

import (
	"testing"

	"github.com/q191201771/lal/pkg/base"
	maxlogic "github.com/q191201771/lalmax/logic"
)

// The RTP receiver creates a Group before it receives codec headers.
// WHEP must wait for a decodable track instead of returning SDP too early.
func TestWHEPMediaReadyRequiresCodecHeader(t *testing.T) {
	if whepMediaReady(nil) {
		t.Fatal("nil group reported media-ready")
	}
	mgr := maxlogic.NewComplexGroupManager()
	group, _ := mgr.GetOrCreateGroup(maxlogic.NewStreamKey("live", "test-whep-ready"), "ready-test", nil, 1, 128)
	if group == nil { t.Fatal("failed to create test group") }
	if whepMediaReady(group) {
		t.Fatal("newly allocated group without codec headers is not WHEP-ready")
	}

	group.OnMsg(base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
		Payload: []byte{0x17, 0x00, 0x00, 0x00, 0x00, 0x01},
	})
	if !whepMediaReady(group) {
		t.Fatal("H264 sequence header should make Group WHEP-ready")
	}
}
func TestWHEPMediaReadySupportsAudioOnlyG711(t *testing.T) {
	mgr := maxlogic.NewComplexGroupManager()
	group, _ := mgr.GetOrCreateGroup(maxlogic.NewStreamKey("live", "test-audio-ready"), "audio-ready", nil, 1, 128)
	if group == nil { t.Fatal("failed to create audio test group") }
	group.OnMsg(base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdAudio},
		Payload: []byte{0x70, 0x00},
	})
	if !whepMediaReady(group) {
		t.Fatal("G711A audio stream should be WHEP-ready")
	}
}

// A GB28181 device may send G711 audio immediately after INVITE while its
// first SPS/PPS/IDR video is still pending. An audio-only SDP answer would
// permanently leave that camera without video until the viewer reconnects.
func TestWHEPGB28181WaitsForVideoHeaderWhenAudioArrivesFirst(t *testing.T) {
	const gbStream = "34020000001320000001:34020000001320000002"
	mgr := maxlogic.NewComplexGroupManager()
	group, _ := mgr.GetOrCreateGroup(maxlogic.NewStreamKey("rtp", gbStream), "gb-audio-first", nil, 1, 128)
	if group == nil { t.Fatal("failed to create group") }
	if !whepGB28181VideoStream(gbStream) { t.Fatal("GB28181 stream ID not detected") }
	group.OnMsg(base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdAudio},
		Payload: []byte{0x70, 0x00},
	})
	if !whepMediaReady(group) { t.Fatal("audio-only readiness should remain valid for VoIP") }
	if whepMediaReadyForStream(group, gbStream) {
		t.Fatal("GB28181 camera became WHEP-ready before a video sequence header")
	}
	group.OnMsg(base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
		Payload: []byte{0x17, 0x00, 0x00, 0x00, 0x00, 0x01},
	})
	if !whepMediaReadyForStream(group, gbStream) {
		t.Fatal("GB28181 camera did not become WHEP-ready after H264 sequence header")
	}
}

func TestWHEPVideoRequiredOnlyForValidGB28181StreamIDs(t *testing.T) {
	for _, id := range []string{
		"voip_1001_abc", "live-audio",
		"34020000001320000001:invalid",
		"3402000000132000000x:34020000001320000002",
		"34020000001320000001",
	} {
		if whepGB28181VideoStream(id) {
			t.Errorf("misclassified audio-capable stream %q as GB28181", id)
		}
	}
}
