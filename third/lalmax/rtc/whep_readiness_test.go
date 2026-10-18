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
