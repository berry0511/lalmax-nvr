package hls

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	lalmaxconfig "github.com/q191201771/lalmax/config"
	"github.com/stretchr/testify/require"
)

func TestHlsSession_NotReadyReturnsRetryableStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := NewHlsSessionWithAppName("live", "cam", lalmaxconfig.Fmp4HlsConfig{LowLatency: true})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/live/hls/cam/index.m3u8", nil)

	session.HandleRequest(ctx)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
}

func TestHlsServer_OnDemand_LazySession(t *testing.T) {
	svr := NewHlsServer(lalmaxconfig.Fmp4HlsConfig{Enable: true, OnDemand: true})
	require.True(t, svr.OnDemandEnabled())

	svr.NewHlsSessionWithAppName("live", "eager")
	_, ok := svr.getSession("live", "eager")
	require.True(t, ok)

	svr2 := NewHlsServer(lalmaxconfig.Fmp4HlsConfig{Enable: true, OnDemand: true})
	_, ok = svr2.getSession("live", "lazy")
	require.False(t, ok)
	_, ok = svr2.ensureSession("live", "lazy")
	require.True(t, ok)
}

func TestHlsServer_EnsureSessionWithAppNameCreatesOnlyOnce(t *testing.T) {
	svr := NewHlsServer(lalmaxconfig.Fmp4HlsConfig{Enable: true, OnDemand: true})

	require.True(t, svr.EnsureSessionWithAppName("live", "cam"))
	require.False(t, svr.EnsureSessionWithAppName("live", "cam"))
	_, ok := svr.getSession("live", "cam")
	require.True(t, ok)
}

func TestHlsServer_SetEnabled_StopsSessions(t *testing.T) {
	svr := NewHlsServer(lalmaxconfig.Fmp4HlsConfig{Enable: true})
	svr.NewHlsSessionWithAppName("live", "cam-1")
	svr.SetEnabled(false)
	svr.NewHlsSessionWithAppName("live", "cam-2")

	var count int
	svr.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)
	require.False(t, svr.conf.Enable)
}
