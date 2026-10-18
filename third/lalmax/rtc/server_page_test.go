package rtc

import (
	"strings"
	"testing"
)

// The built-in WHEP page must attach tracks even when browsers provide an
// RTCTrackEvent without streams. SDP negotiation alone is not evidence of
// a connected, decoding WebRTC session.
func TestBuiltInWHEPPlayerHandlesStreamlessRemoteTracks(t *testing.T) {
	html := buildWHEPPlayHTML()
	for _, required := range []string{
		"new MediaStream()",
		"pc.ontrack = event =>",
		"attachTrack(event.track)",
		"media.addTrack(track)",
		"video.srcObject = media",
		"video.play()",
		"pc.getReceivers()",
		"pc.oniceconnectionstatechange",
		"pc.onconnectionstatechange",
		"pc.getStats()",
		"framesDecoded",
		"ICE: ",
	} {
		if !strings.Contains(html, required) {
			t.Errorf("built-in WHEP page missing %q", required)
		}
	}
	if strings.Contains(html, "video.srcObject=e.streams[0]") {
		t.Fatal("page still assumes streams[0] is always present")
	}
}

func TestBuiltInWHEPPlayerHidesDiagnosticsByDefault(t *testing.T) {
	html := buildWHEPPlayHTML()
	if !strings.Contains(html, "pre{display:none;") {
		t.Fatal("WHEP status diagnostics must be hidden by default")
	}
	if !strings.Contains(html, "new URLSearchParams(location.search).get('debug') === '1'") {
		t.Fatal("debug=1 query parameter must reveal WHEP diagnostics")
	}
	if !strings.Contains(html, "status.style.display = 'block';") {
		t.Fatal("WHEP failures should expose diagnostic errors")
	}
}

func TestBuiltInWHEPPlayerReportsActualStateNotPrematureConnected(t *testing.T) {
	html := buildWHEPPlayHTML()
	if !strings.Contains(html, "SDP negotiated; waiting for media") {
		t.Fatal("page should describe SDP completion accurately")
	}
	if strings.Contains(html, "log('connected: '+location.href)") {
		t.Fatal("page must not announce WebRTC connected just after SDP")
	}
	if !strings.Contains(html, "WHEP HTTP ") {
		t.Fatal("page should report actual WHEP HTTP errors")
	}
}
