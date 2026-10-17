package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/middleware"
	"github.com/stretchr/testify/require"
)

func TestTencentParseStreamID(t *testing.T) {
	for _, tc := range []struct {
		url   string
		id    string
		valid bool
	}{
		{"webrtc://example.org/live/camera-1", "camera-1", true},
		{"webrtc://121.237.176.144:19090/live/32010000001327000001:34020000001320000001", "32010000001327000001:34020000001320000001", true},
		{"webrtc://example.org/live/", "", false},
		{"webrtc://example.org/other/camera", "", false},
		{"webrtc://example.org/live/camera/other", "", false},
		{"https://example.org/live/camera", "", false},
		{"webrtc://example.org/live/%2fetc", "", false},
		{"webrtc://example.org/live/..", "", false},
		{"webrtc://user:password@example.org/live/cam", "", false},
	} {
		id, valid := parseTencentStreamID(tc.url)
		require.Equal(t, tc.valid, valid, tc.url)
		if valid {
			require.Equal(t, tc.id, id)
		}
	}
}

func TestTencentRTCPlay_RequiresAuthentication(t *testing.T) {
	db, store := setupTestDB(t)
	defer db.Close()
	authMW, _ := middleware.NewAuthMiddleware(middleware.AuthProvider{
		GetUsername: func() string { return "admin" },
		GetHash:     func() string { return "a$dummyhashdummyhashdummyhashdum" },
	}, "")
	h := NewHandler(db, store, authMW, nil, nil, "", nil, nil)
	r := httptest.NewRequest(http.MethodPost, "/rtc/v1/play", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTencentRTCPlayAndUnplay_ReuseWHEP(t *testing.T) {
	const streamID = "32010000001327000001:34020000001320000001"
	const offer = "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n"
	const answer = "v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\n"
	var created, deleted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/whep":
			require.Equal(t, "application/sdp", r.Header.Get("Content-Type"))
			w.Header().Set("Location", "/whep/session-1")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(answer))
			created.Add(1)
		case r.Method == http.MethodDelete && r.URL.Path == "/whep/session-1":
			w.WriteHeader(http.StatusNoContent)
			deleted.Add(1)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db, store := setupTestDB(t)
	defer db.Close()
	h := NewHandler(db, store, noopAuthMW(), nil, nil, "", nil, nil)
	h.SetMediaEngine(&stubMediaEngine{
		streamsByID: map[string]*media.StreamInfo{streamID: {StreamID: streamID, Active: true}},
		playURLs:    map[string]string{"webrtc": server.URL + "/whep"},
	})
	routes := h.Routes()
	requestBody, err := json.Marshal(tencentPlayRequest{
		StreamURL: "webrtc://example.org/live/" + streamID,
		SDP:       offer,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/rtc/v1/play", strings.NewReader(string(requestBody)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var got tencentReply
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, http.StatusOK, got.Code)
	require.NotNil(t, got.Data)
	require.Equal(t, answer, got.Data.SDP)
	require.Len(t, got.Data.SessionID, 32)
	require.EqualValues(t, 1, created.Load())

	req = httptest.NewRequest(http.MethodPost, "/rtc/v1/unplay",
		strings.NewReader(`{"sessionid":"`+got.Data.SessionID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.EqualValues(t, 1, deleted.Load())

	// Repeated unplay must not issue a second upstream DELETE.
	req = httptest.NewRequest(http.MethodPost, "/rtc/v1/unplay",
		strings.NewReader(`{"sessionid":"`+got.Data.SessionID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	require.Equal(t, http.StatusNotFound, resp.Code)
	require.EqualValues(t, 1, deleted.Load())
}

func TestTencentRTCPlay_RejectsUnknownStream(t *testing.T) {
	db, store := setupTestDB(t)
	defer db.Close()
	h := NewHandler(db, store, noopAuthMW(), nil, nil, "", nil, nil)
	h.SetMediaEngine(&stubMediaEngine{streamsByID: map[string]*media.StreamInfo{}})
	req := httptest.NewRequest(http.MethodPost, "/rtc/v1/play",
		strings.NewReader(`{"streamurl":"webrtc://host/live/nonexistent","sdp":"v=0"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	h.Routes().ServeHTTP(resp, req)
	require.Equal(t, http.StatusNotFound, resp.Code)
}
