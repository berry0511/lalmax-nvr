package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *LalMaxServer) initRtcRouter(router *gin.Engine) {
	rtc := router.Group("/webrtc")
	rtc.GET("/whip", s.HandleWHIP)
	rtc.POST("/whip", s.HandleWHIP)
	rtc.OPTIONS("/whip", s.HandleWHIP)
	rtc.DELETE("/whip", s.HandleWHIP)

	playAuth := s.protectHTTPPlay(s.HandleWHEP)
	rtc.GET("/whep", playAuth)
	rtc.POST("/whep", playAuth)
	rtc.OPTIONS("/whep", playAuth)
	rtc.DELETE("/whep", playAuth)

	jessibucaAuth := s.protectHTTPPlay(s.HandleJessibuca)
	rtc.POST("/play/live/:streamid", jessibucaAuth)
	rtc.DELETE("/play/live/:streamid", jessibucaAuth)
}

func (s *LalMaxServer) HandleWHIP(c *gin.Context) {
	switch c.Request.Method {
	case "GET":
		if s.rtcsvr != nil {
			s.rtcsvr.ServeWHIPPublishPage(c)
		} else {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusServiceUnavailable, "<!doctype html><meta charset=utf-8><title>WHIP</title><p>RTC 未启用：请在配置中将 <code>lalmax.rtc_config.enable</code> 设为 <code>true</code> 并重启服务。</p><p>推流地址示例：<code>/webrtc/whip?streamid=test110</code></p>")
		}
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHIP(c)
		} else {
			c.String(http.StatusServiceUnavailable, "rtc disabled")
		}
	case "OPTIONS":
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Expose-Headers", "Location")
		c.Header("Access-Control-Max-Age", "86400")
		c.Header("Accept-Post", "application/sdp")
		c.Status(http.StatusNoContent)
	case "DELETE":
		// TODO 实现 DELETE
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleWHEP(c *gin.Context) {
	switch c.Request.Method {
	case "GET":
		if s.rtcsvr != nil {
			s.rtcsvr.ServeWHEPPlayPage(c)
		} else {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusServiceUnavailable, "<!doctype html><meta charset=utf-8><title>WHEP</title><p>RTC 未启用：请在配置中将 <code>lalmax.rtc_config.enable</code> 设为 <code>true</code> 并重启服务。</p><p>播放地址示例：<code>http://127.0.0.1:1290/webrtc/whep?streamid=test110</code>（端口以 <code>http_listen_addr</code> 为准）</p>")
		}
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHEP(c)
		} else {
			c.String(http.StatusServiceUnavailable, "rtc disabled")
		}
	case "OPTIONS":
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Expose-Headers", "Location")
		c.Header("Access-Control-Max-Age", "86400")
		c.Header("Accept-Post", "application/sdp")
		c.Status(http.StatusNoContent)
	case "DELETE":
		// WHEP DELETE 由客户端发起通知服务端结束会话
		// whepSession 会通过 PeerConnection 状态变化自动清理
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleJessibuca(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleJessibuca(c)
		}
	case "DELETE":
		// TODO 实现 DELETE
		c.Status(http.StatusOK)
	}
}
