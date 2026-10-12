package hls

import (
	"sync"
	"time"

	config "github.com/q191201771/lalmax/config"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

type HlsServer struct {
	sessions        sync.Map
	conf            config.Fmp4HlsConfig
	invalidSessions sync.Map
}

func NewHlsServer(conf config.Fmp4HlsConfig) *HlsServer {
	svr := &HlsServer{
		conf: conf,
	}

	go svr.maintenanceLoop()

	return svr
}

func (s *HlsServer) NewHlsSession(streamName string) {
	s.NewHlsSessionWithAppName("", streamName)
}

func (s *HlsServer) SetEnabled(enable bool) {
	if s == nil {
		return
	}
	s.conf.Enable = enable
	if enable {
		return
	}
	s.sessions.Range(func(key, value interface{}) bool {
		session := value.(*HlsSession)
		s.invalidSessions.Store(session.SessionId, session)
		s.sessions.Delete(key)
		return true
	})
}

func (s *HlsServer) SetOnDemand(onDemand bool, idleTimeoutMs int) {
	if s == nil {
		return
	}
	s.conf.OnDemand = onDemand
	if idleTimeoutMs > 0 {
		s.conf.OnDemandIdleTimeoutMs = idleTimeoutMs
	}
	if onDemand {
		s.tickOnDemandIdle()
	}
}

func (s *HlsServer) OnDemandEnabled() bool {
	return s != nil && s.conf.OnDemand
}

// EnsureSessionWithAppName creates an on-demand session only when one is not
// already present. It returns true when this call created the session.
func (s *HlsServer) EnsureSessionWithAppName(appName, streamName string) bool {
	if s == nil || !s.conf.Enable || !s.conf.OnDemand {
		return false
	}
	if _, ok := s.getSession(appName, streamName); ok {
		return false
	}
	key := hlsSessionKey(appName, streamName)
	session := NewHlsSessionWithAppName(appName, streamName, s.conf)
	_, loaded := s.sessions.LoadOrStore(key, session)
	return !loaded
}

func (s *HlsServer) NewHlsSessionWithAppName(appName, streamName string) {
	if s == nil || !s.conf.Enable {
		return
	}
	nazalog.Infof("new hls session, appName:%s, streamName:%s", appName, streamName)
	session := NewHlsSessionWithAppName(appName, streamName, s.conf)
	s.sessions.Store(hlsSessionKey(appName, streamName), session)
}

func (s *HlsServer) OnMsg(streamName string, msg base.RtmpMsg) {
	s.OnMsgWithAppName("", streamName, msg)
}

func (s *HlsServer) OnMsgWithAppName(appName, streamName string, msg base.RtmpMsg) {
	if s == nil || !s.conf.Enable {
		return
	}
	value, ok := s.sessions.Load(hlsSessionKey(appName, streamName))
	if ok {
		session := value.(*HlsSession)
		session.OnMsg(msg)
	}
}

func (s *HlsServer) OnStop(streamName string) {
	s.OnStopWithAppName("", streamName)
}

func (s *HlsServer) OnStopWithAppName(appName, streamName string) {
	key := hlsSessionKey(appName, streamName)
	value, ok := s.sessions.Load(key)
	if ok {
		session := value.(*HlsSession)
		s.invalidSessions.Store(session.SessionId, session)
		s.sessions.Delete(key)
	}
}

func (s *HlsServer) HandleRequest(ctx *gin.Context) {
	streamName := ctx.Param("streamid")
	appName := ctx.Query("app_name")
	session, ok := s.ensureSession(appName, streamName)
	if !ok {
		return
	}
	session.touchAccess()
	session.HandleRequest(ctx)
}

func (s *HlsServer) ensureSession(appName, streamName string) (*HlsSession, bool) {
	if session, ok := s.getSession(appName, streamName); ok {
		return session, true
	}
	if s == nil || !s.conf.Enable {
		return nil, false
	}
	if !s.conf.OnDemand {
		return nil, false
	}
	s.NewHlsSessionWithAppName(appName, streamName)
	return s.getSession(appName, streamName)
}

func (s *HlsServer) getSession(appName, streamName string) (*HlsSession, bool) {
	value, ok := s.sessions.Load(hlsSessionKey(appName, streamName))
	if ok {
		return value.(*HlsSession), true
	}

	var found *HlsSession
	matchCount := 0
	s.sessions.Range(func(_, value interface{}) bool {
		session := value.(*HlsSession)
		if session.streamName != streamName {
			return true
		}
		found = session
		matchCount++
		return matchCount <= 1
	})
	if matchCount != 1 {
		return nil, false
	}
	return found, true
}

type sessionKey struct {
	appName    string
	streamName string
}

func hlsSessionKey(appName, streamName string) sessionKey {
	return sessionKey{
		appName:    appName,
		streamName: streamName,
	}
}

func (s *HlsServer) maintenanceLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.tickOnDemandIdle()
		s.cleanInvalidSessions()
	}
}

func (s *HlsServer) tickOnDemandIdle() {
	if s == nil || !s.conf.Enable || !s.conf.OnDemand {
		return
	}
	idleMs := s.conf.OnDemandIdleTimeoutMs
	if idleMs <= 0 {
		idleMs = 60000
	}
	now := time.Now().Unix()
	idleSec := int64(idleMs) / 1000
	s.sessions.Range(func(key, value interface{}) bool {
		session := value.(*HlsSession)
		last := session.lastAccessUnix.Load()
		if last == 0 {
			last = session.createdUnix.Load()
		}
		if now-last >= idleSec {
			s.invalidSessions.Store(session.SessionId, session)
			s.sessions.Delete(key)
		}
		return true
	})
}

func (s *HlsServer) cleanInvalidSessions() {
	s.invalidSessions.Range(func(k, v interface{}) bool {
		session := v.(*HlsSession)
		nazalog.Info("clean invalid session, streamName:", session.streamName, " sessionId:", k)
		session.OnStop()
		s.invalidSessions.Delete(k)
		return true
	})
}
