package gb28181

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
)

// Reproduces the second review's RTP timeout: the SIP 481 is separately
// classified as an absent remote dialog, and the HTTP RTP stop returns 1003.
func TestGB28181StopExpiredRTPReceiverAllowsReinvite(t *testing.T) {
	for _, tc := range []struct {
		code int
		wantError bool
	}{
		{0, false}, {1003, false}, {1001, true}, {2002, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/ctrl/stop_rtp_pub" || r.URL.Query().Get("session_id") != "timed-out-receiver" {
				t.Errorf("unexpected stop RTP request: %s", r.URL.String())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error_code": tc.code, "desp": "rtp pub session not found",
			})
		}))
		engine, err := media.NewLalmaxHTTP(media.LalmaxHTTPConfig{BaseURL: server.URL})
		if err != nil { t.Fatal(err) }
		g := &GB28181API{mediaEngine: engine}
		stream := &Streams{DeviceID: "device", SessionID: "timed-out-receiver"}
		if !byeConfirmsRemoteDialogGone(481) { t.Fatal("remote missing SIP dialog should allow local cleanup") }
		err = g.stopPlayRTPReceiver(stream)
		if tc.wantError && err == nil { t.Errorf("media code %d incorrectly allowed recovery", tc.code) }
		if !tc.wantError && err != nil { t.Errorf("media code %d blocked recovery: %v", tc.code, err) }
		server.Close()
	}
}
