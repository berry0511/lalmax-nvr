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

// Hiding the WHEP diagnostic panel must not hide the WHIP publishing page logs.
func TestBuiltInWHIPPublisherKeepsDiagnosticsVisible(t *testing.T) {
	html := buildWHIPPublishHTML()
	if strings.Contains(html, "pre{display:none;") {
		t.Fatal("WHIP publisher status was unintentionally hidden")
	}
	if !strings.Contains(html, `<pre id="log">`) {
		t.Fatal("WHIP publisher logging area missing")
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

func TestBuiltInWHEPPlayerFillsViewportWithoutStretchingVideo(t *testing.T) {
	html := buildWHEPPlayHTML()
	for _, required := range []string{
		"html,body{width:100%;height:100%;margin:0;overflow:hidden}",
		"main{position:fixed;inset:0;",
		"video{display:block;width:100%;height:100%;object-fit:contain;",
		"pre{display:none;position:absolute;",
	} {
		if !strings.Contains(html, required) {
			t.Errorf("built-in WHEP player missing viewport style %q", required)
		}
	}
	for _, restricted := range []string{
		"main{max-width:960px",
		"video{width:100%;background:#000;border-radius:12px}",
	} {
		if strings.Contains(html, restricted) {
			t.Errorf("built-in WHEP player still has constrained layout %q", restricted)
		}
	}
}
