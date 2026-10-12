package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	maxlogic "github.com/q191201771/lalmax/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

func (s *LalMaxServer) initFmp4Router(router *gin.Engine) {
	router.GET("/live/m4s/:streamid", s.HandleHttpFmp4)
	router.GET("/live/hls/:streamid/:type", s.HandleHls)
}

func (s *LalMaxServer) HandleHls(c *gin.Context) {
	if s.hlssvr != nil {
		if s.hlssvr.OnDemandEnabled() {
			key := maxlogic.NewStreamKey(c.Query("app_name"), c.Param("streamid"))
			if _, group := maxlogic.GetGroupManagerInstance().GetGroup(key); group != nil &&
				s.hlssvr.EnsureSessionWithAppName(key.AppName, key.StreamName) {
				group.ReplayCachedGopToHls()
			}
		}
		s.hlssvr.HandleRequest(c)
	} else {
		nazalog.Error("hls is disable")
		c.Status(http.StatusNotFound)
	}
}

func (s *LalMaxServer) HandleHttpFmp4(c *gin.Context) {
	if s.httpfmp4svr != nil {
		s.httpfmp4svr.HandleRequest(c)
	} else {
		nazalog.Error("http-fmp4 is disable")
		c.Status(http.StatusNotFound)
	}
}
