package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Tencent-style JSON/SDP signaling adapter, modeled on the published
// /rtc/v1/play and /rtc/v1/unplay API contract. This is NOT a replacement for
// WHEP and does not imply compatibility with an unverified TCPlayer SDK build.
//
// Routes are protected by the existing NVR authentication middleware. Never
// expose them unauthenticated merely to make an external demo load.
const (
	tencentMaxSDPBytes = 1 << 20
	tencentMaxSessions = 4096
	tencentSessionTTL  = 2 * time.Hour
)

type tencentPlayRequest struct {
	StreamURL string `json:"streamurl"`
	SDP       string `json:"sdp"`
	ClientIP  string `json:"clientip,omitempty"`
}

type tencentPlayData struct {
	SDP       string `json:"sdp"`
	SessionID string `json:"sessionid"`
}

type tencentReply struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data *tencentPlayData `json:"data,omitempty"`
}

type tencentUnplayRequest struct {
	SessionID string `json:"sessionid"`
	Data      struct {
		SessionID string `json:"sessionid"`
	} `json:"data"`
}

type tencentSession struct {
	upstreamURL string
	created     time.Time
}

func parseTencentStreamID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "webrtc" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", false
	}
	// Resolve only local published streams. Do not interpret the user-supplied
	// host as an upstream HTTP target (SSRF / open-proxy protection).
	if !strings.HasPrefix(u.Path, "/live/") {
		return "", false
	}
	streamID := strings.TrimPrefix(u.Path, "/live/")
	if streamID == "" || streamID == "." || streamID == ".." ||
		strings.ContainsAny(streamID, "/\\") || strings.Contains(u.EscapedPath(), "%2f") ||
		strings.Contains(u.EscapedPath(), "%2F") {
		return "", false
	}
	return streamID, true
}

func tencentError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, tencentReply{Code: status, Msg: msg})
}

func decodeTencentBody(r *http.Request, out any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, tencentMaxSDPBytes+4096)).Decode(out)
}

// handleTencentRTCPlay turns a published-stream JSON SDP offer into a WHEP
// session on the existing media engine; it neither starts another GB28181
// INVITE nor opens a second media/ICE stack.
func (h *Handler) handleTencentRTCPlay(w http.ResponseWriter, r *http.Request) {
	if h.mediaEngine == nil {
		tencentError(w, http.StatusServiceUnavailable, "media engine unavailable")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		tencentError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return
	}
	var in tencentPlayRequest
	if err := decodeTencentBody(r, &in); err != nil {
		tencentError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	streamID, ok := parseTencentStreamID(in.StreamURL)
	if !ok || len(in.SDP) == 0 || len(in.SDP) > tencentMaxSDPBytes ||
		!strings.HasPrefix(strings.TrimSpace(in.SDP), "v=0") {
		tencentError(w, http.StatusBadRequest, "invalid streamurl or SDP offer")
		return
	}
	info, err := h.mediaEngine.GetStream(r.Context(), streamID)
	if err != nil {
		tencentError(w, http.StatusBadGateway, "stream lookup failed")
		return
	}
	if info == nil || !info.Active {
		tencentError(w, http.StatusNotFound, "stream not active")
		return
	}
	upstream, err := h.mediaPlayURL(r.Context(), streamID, "webrtc")
	if err != nil || upstream == nil {
		tencentError(w, http.StatusServiceUnavailable, "WebRTC unavailable")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream.String(), strings.NewReader(in.SDP))
	if err != nil {
		tencentError(w, http.StatusBadGateway, "failed to create WHEP request")
		return
	}
	req.Header.Set("Content-Type", "application/sdp")
	resp, err := h.playProxyClient().Do(req)
	if err != nil {
		tencentError(w, http.StatusBadGateway, "WHEP negotiation failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tencentError(w, http.StatusBadGateway, "WHEP negotiation rejected")
		return
	}
	answer, err := io.ReadAll(io.LimitReader(resp.Body, tencentMaxSDPBytes+1))
	if err != nil || len(answer) == 0 || len(answer) > tencentMaxSDPBytes {
		tencentError(w, http.StatusBadGateway, "invalid WHEP SDP answer")
		return
	}
	location := resp.Header.Get("Location")
	if location == "" {
		tencentError(w, http.StatusBadGateway, "WHEP session location missing")
		return
	}
	sessionURL := resolveUpstreamLocation(upstream, location)
	parsed, err := url.Parse(sessionURL)
	if err != nil || parsed.Scheme != upstream.Scheme || parsed.Host != upstream.Host {
		tencentError(w, http.StatusBadGateway, "WHEP session origin mismatch")
		return
	}
	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		tencentError(w, http.StatusInternalServerError, "session allocation failed")
		return
	}
	id := hex.EncodeToString(rawID)
	h.tencentSessionsMu.Lock()
	if h.tencentSessions == nil {
		h.tencentSessions = make(map[string]tencentSession)
	}
	now := time.Now()
	// Bound memory: callers must unplay; expired entries are pruned lazily.
	for k, session := range h.tencentSessions {
		if now.Sub(session.created) >= tencentSessionTTL {
			delete(h.tencentSessions, k)
		}
	}
	if len(h.tencentSessions) >= tencentMaxSessions {
		h.tencentSessionsMu.Unlock()
		h.deleteTencentUpstreamSession(r, sessionURL)
		tencentError(w, http.StatusServiceUnavailable, "too many sessions")
		return
	}
	h.tencentSessions[id] = tencentSession{upstreamURL: sessionURL, created: now}
	h.tencentSessionsMu.Unlock()

	writeJSON(w, http.StatusOK, tencentReply{
		Code: http.StatusOK,
		Msg:  "OK",
		Data: &tencentPlayData{SDP: string(answer), SessionID: id},
	})
}

func (h *Handler) deleteTencentUpstreamSession(r *http.Request, upstreamURL string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, upstreamURL, nil)
	if err != nil {
		return err
	}
	resp, err := h.playProxyClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream WHEP DELETE returned %d", resp.StatusCode)
	}
	return nil
}

func (h *Handler) handleTencentRTCUnplay(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		tencentError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return
	}
	var in tencentUnplayRequest
	if err := decodeTencentBody(r, &in); err != nil {
		tencentError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	id := in.SessionID
	if id == "" {
		id = in.Data.SessionID
	}
	if len(id) != 32 {
		tencentError(w, http.StatusBadRequest, "invalid sessionid")
		return
	}
	h.tencentSessionsMu.Lock()
	session, ok := h.tencentSessions[id]
	if ok {
		delete(h.tencentSessions, id)
	}
	h.tencentSessionsMu.Unlock()
	if !ok {
		tencentError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := h.deleteTencentUpstreamSession(r, session.upstreamURL); err != nil {
		// Restore the session so a client can retry cleanup.
		h.tencentSessionsMu.Lock()
		h.tencentSessions[id] = session
		h.tencentSessionsMu.Unlock()
		tencentError(w, http.StatusBadGateway, "failed to release WHEP session")
		return
	}
	writeJSON(w, http.StatusOK, tencentReply{Code: http.StatusOK, Msg: "OK"})
}
