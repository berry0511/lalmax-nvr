package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/lalmax-pro/lalmax-nvr/internal/observability"
	"github.com/q191201771/lal/pkg/base"
	"go.opentelemetry.io/otel/attribute"
)

const (
	hlsPullRetryInterval   = 2 * time.Second
	hlsPullMaxRetryWait    = 30 * time.Second
	defaultMaxHLSPullTasks = 32
)

var (
	// ErrHLSPullEmbeddedOnly is returned when native HLS pull is used outside embedded mode.
	ErrHLSPullEmbeddedOnly = errors.New("native HLS pull requires media.mode=embedded")
	// ErrHLSPullUnsupportedCodec is returned when the playlist has no H.264/H.265+AAC tracks.
	ErrHLSPullUnsupportedCodec = errors.New("hls pull requires h264/h265 video and optional aac audio")
	// ErrHLSPullBusy is returned when the concurrent pull budget is exhausted.
	ErrHLSPullBusy = errors.New("hls pull concurrency limit reached")
	// errHLSPullPlaybackDuration marks the natural end of a bounded VOD playback.
	errHLSPullPlaybackDuration = errors.New("hls pull playback duration reached")
	hlsPullURLPattern          = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
)

// HLSPullManager pulls standard HLS playlists and injects frames into lal.
type HLSPullManager struct {
	engine       Engine
	httpClient   *http.Client
	maxTasks     int
	sessionState func(streamID string, online bool, at time.Time, offlineReason, offlineDetail string)
	pullError    func(streamID, detail string)

	mu    sync.Mutex
	tasks map[string]*hlsPullTask
}

type hlsPullTask struct {
	streamID string
	req      StartHLSPullRequest
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewHLSPullManager creates a native HLS puller bound to an embedded media engine.
func NewHLSPullManager(engine Engine) *HLSPullManager {
	return &HLSPullManager{
		engine: engine,
		httpClient: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          32,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
			},
		},
		maxTasks: defaultMaxHLSPullTasks,
		tasks:    make(map[string]*hlsPullTask),
	}
}

// SetMaxTasks caps how many HLS pulls can run at once. 0 or negative keeps the default.
func (m *HLSPullManager) SetMaxTasks(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.mu.Lock()
	m.maxTasks = n
	m.mu.Unlock()
}

// SetSessionCallback reports when a pull task is established and closed.
func (m *HLSPullManager) SetSessionCallback(callback func(streamID string, online bool, at time.Time, offlineReason, offlineDetail string)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.sessionState = callback
	m.mu.Unlock()
}

// SetPullErrorCallback receives sanitized errors before a failed pull session
// is removed from the media engine.
func (m *HLSPullManager) SetPullErrorCallback(callback func(streamID, detail string)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.pullError = callback
	m.mu.Unlock()
}

func (m *HLSPullManager) notifyPullError(streamID string, err error) {
	if err == nil {
		return
	}
	detail := sanitizeHLSPullError(err.Error())
	slog.Warn("HLS pull attempt failed", "stream_id", streamID, "error", detail)
	m.mu.Lock()
	callback := m.pullError
	m.mu.Unlock()
	if callback != nil {
		callback(streamID, detail)
	}
}

func sanitizeHLSPullError(detail string) string {
	return hlsPullURLPattern.ReplaceAllStringFunc(detail, func(raw string) string {
		trimmed := raw
		suffix := ""
		for len(trimmed) > 0 && strings.ContainsRune(").,;]}", rune(trimmed[len(trimmed)-1])) {
			suffix = trimmed[len(trimmed)-1:] + suffix
			trimmed = trimmed[:len(trimmed)-1]
		}
		u, err := url.Parse(trimmed)
		if err != nil {
			return "[URL redacted]" + suffix
		}
		u.User = nil
		if u.Path != "" {
			u.Path = "/[path redacted]"
			u.RawPath = ""
		}
		if u.RawQuery != "" {
			u.RawQuery = "redacted"
			u.ForceQuery = true
		}
		u.Fragment = ""
		return u.String() + suffix
	})
}

func (m *HLSPullManager) notifySession(streamID string, online bool, at time.Time, offlineReason, offlineDetail string) {
	m.mu.Lock()
	callback := m.sessionState
	m.mu.Unlock()
	if callback != nil {
		callback(streamID, online, at, offlineReason, offlineDetail)
	}
}

func (m *HLSPullManager) PullingStreamIDs() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		ids = append(ids, id)
	}
	return ids
}

func (m *HLSPullManager) StartHLSPull(ctx context.Context, req StartHLSPullRequest) (result *StreamSession, retErr error) {
	ctx, span := observability.StartBusinessSpan(ctx, "media.hls_pull.start",
		attribute.String("nvr.stream.id", strings.TrimSpace(req.StreamID)),
		attribute.String("nvr.media.source.kind", "hls"),
	)
	defer func() { observability.EndBusinessSpan(span, retErr) }()
	if m == nil || m.engine == nil {
		return nil, ErrHLSPullEmbeddedOnly
	}
	streamID := strings.TrimSpace(req.StreamID)
	playlist := strings.TrimSpace(req.PlaylistURL)
	if streamID == "" {
		return nil, errors.New("stream ID is required")
	}
	if playlist == "" && req.PlaylistURLProvider == nil {
		return nil, errors.New("playlist URL is required")
	}
	if !engineSupportsCustomizePub(m.engine) {
		return nil, ErrHLSPullEmbeddedOnly
	}

	m.mu.Lock()
	if existing, ok := m.tasks[streamID]; ok {
		m.mu.Unlock()
		return &StreamSession{StreamID: existing.streamID, Protocol: "hls_pull", StartedAt: time.Now()}, nil
	}
	if m.maxTasks > 0 && len(m.tasks) >= m.maxTasks {
		m.mu.Unlock()
		return nil, ErrHLSPullBusy
	}
	ctx, cancel := context.WithCancel(observability.DetachedContext(ctx))
	startedAt := time.Now()
	task := &hlsPullTask{
		streamID: streamID,
		req:      req,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	task.req.StreamID = streamID
	task.req.PlaylistURL = playlist
	m.tasks[streamID] = task
	m.mu.Unlock()

	m.notifySession(streamID, true, startedAt, "", "")
	go m.runTask(ctx, task)
	return &StreamSession{StreamID: streamID, Protocol: "hls_pull", StartedAt: startedAt}, nil
}

func (m *HLSPullManager) StopHLSPull(_ context.Context, streamID string) error {
	streamID = strings.TrimSpace(streamID)
	m.mu.Lock()
	task, ok := m.tasks[streamID]
	if ok {
		delete(m.tasks, streamID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	task.cancel()
	select {
	case <-task.done:
	case <-time.After(8 * time.Second):
	}
	return nil
}

func (m *HLSPullManager) StopAllHLSPulls() {
	m.mu.Lock()
	tasks := make([]*hlsPullTask, 0, len(m.tasks))
	for id, task := range m.tasks {
		delete(m.tasks, id)
		tasks = append(tasks, task)
	}
	m.mu.Unlock()
	for _, task := range tasks {
		task.cancel()
	}
	for _, task := range tasks {
		select {
		case <-task.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *HLSPullManager) runTask(ctx context.Context, task *hlsPullTask) {
	defer close(task.done)
	var completedWith error
	completionReason := "stopped"
	var completionDetail string
	defer func() {
		m.mu.Lock()
		if cur, ok := m.tasks[task.streamID]; ok && cur == task {
			delete(m.tasks, task.streamID)
		}
		m.mu.Unlock()
		m.notifySession(task.streamID, false, time.Now(), completionReason, completionDetail)
		if ctx.Err() == nil && task.req.OnComplete != nil {
			task.req.OnComplete(completedWith)
		}
	}()

	attempts := 0
	wait := hlsPullRetryInterval
	for {
		if ctx.Err() != nil {
			return
		}
		attempts++
		if task.req.PlaylistURLProvider != nil {
			playlistURL, headers, err := task.req.PlaylistURLProvider(ctx)
			if err != nil {
				completedWith = err
			} else {
				task.req.PlaylistURL = playlistURL
				task.req.Headers = headers
			}
			if err != nil {
				m.notifyPullError(task.streamID, err)
				if task.req.PullRetryNum >= 0 && attempts > task.req.PullRetryNum {
					completionReason = "upstream_error"
					completionDetail = sanitizeHLSPullError(err.Error())
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				if wait < hlsPullMaxRetryWait {
					wait = min(wait*2, hlsPullMaxRetryWait)
				}
				continue
			}
		}
		err := m.pullOnce(ctx, task)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, gohlslib.ErrClientEOS) {
			if task.req.RepeatOnEnd {
				attempts = 0
				wait = hlsPullRetryInterval
				continue
			}
			completionReason = "source_ended"
			completedWith = nil
			return
		}
		if errors.Is(err, errHLSPullPlaybackDuration) {
			if task.req.RepeatOnEnd {
				attempts = 0
				wait = hlsPullRetryInterval
				continue
			}
			completionReason = "playback_completed"
			completedWith = nil
			return
		}
		completedWith = err
		if task.req.PullRetryNum >= 0 && attempts > task.req.PullRetryNum {
			completionReason = "upstream_error"
			completionDetail = sanitizeHLSPullError(err.Error())
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait < hlsPullMaxRetryWait {
			wait *= 2
			if wait > hlsPullMaxRetryWait {
				wait = hlsPullMaxRetryWait
			}
		}
	}
}

func (m *HLSPullManager) pullOnce(ctx context.Context, task *hlsPullTask) (retErr error) {
	attrs := []attribute.KeyValue{
		attribute.String("nvr.stream.id", task.streamID),
		attribute.String("nvr.media.source.kind", "hls"),
	}
	var session CustomizePubSession
	ctx, err := observability.RunBusinessOperation(ctx, "media.hls_pull.open_session", func(opCtx context.Context) error {
		var err error
		session, err = m.engine.AddCustomizePubSession(opCtx, task.streamID)
		return err
	}, attrs...)
	if err != nil {
		return err
	}
	session.WithOption(func(option *base.AvPacketStreamOption) {
		option.VideoFormat = base.AvPacketStreamVideoFormatAnnexb
		option.AudioFormat = base.AvPacketStreamAudioFormatRawAac
	})
	defer func() {
		if retErr != nil && ctx.Err() == nil && !errors.Is(retErr, gohlslib.ErrClientEOS) && !errors.Is(retErr, errHLSPullPlaybackDuration) {
			m.notifyPullError(task.streamID, retErr)
		}
		opCtx, span := observability.StartBusinessSpan(observability.DetachedContext(ctx), "media.hls_pull.close_session", attrs...)
		closeErr := m.engine.DelCustomizePubSession(opCtx, session)
		observability.EndBusinessSpan(span, closeErr)
	}()

	client := &gohlslib.Client{
		URI:                       task.req.PlaylistURL,
		HTTPClient:                headerClient(m.httpClient, task.req.Headers, task.req.PlaylistURL, task.req.RestrictHeadersToOrigin),
		OnDownloadPrimaryPlaylist: func(string) {},
		OnDownloadStreamPlaylist:  func(string) {},
		OnDownloadSegment:         func(string) {},
		OnDownloadPart:            func(string) {},
		OnDecodeError:             func(error) {},
	}
	var onlineOnce sync.Once
	videoStarted := make(chan struct{})
	markOnline := func() {
		onlineOnce.Do(func() {
			close(videoStarted)
		})
	}
	client.OnTracks = func(tracks []*gohlslib.Track) error {
		return attachHLSTracks(client, session, tracks, markOnline)
	}
	ctx, err = observability.RunBusinessOperation(ctx, "media.hls_pull.start_client", func(context.Context) error {
		return client.Start()
	}, attrs...)
	if err != nil {
		return err
	}
	defer client.Close()

	if task.req.PlaybackDuration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-client.Wait():
			return err
		}
	}
	waitForVideo := time.NewTimer(30 * time.Second)
	defer waitForVideo.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-videoStarted:
	case err := <-client.Wait():
		return err
	case <-waitForVideo.C:
		return errors.New("HLS pull did not receive video before playback duration timer")
	}
	playbackTimer := time.NewTimer(task.req.PlaybackDuration)
	defer playbackTimer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-client.Wait():
		return err
	case <-playbackTimer.C:
		return errHLSPullPlaybackDuration
	}
}

func attachHLSTracks(client *gohlslib.Client, session CustomizePubSession, tracks []*gohlslib.Track, onVideoFrame ...func()) error {
	info, err := ClassifyHLSTracks(tracks)
	if err != nil {
		return err
	}
	if info.VideoParamSets != nil {
		_ = session.FeedAvPacket(base.AvPacket{
			Payload:     info.VideoParamSets,
			PayloadType: info.VideoPT,
			Pts:         0,
			Timestamp:   0,
		})
	}
	if len(info.AudioASC) > 0 {
		if err := session.FeedAudioSpecificConfig(info.AudioASC); err != nil {
			return err
		}
	}
	for _, track := range tracks {
		clockRate := track.ClockRate
		if clockRate <= 0 {
			clockRate = 90000
		}
		switch codec := track.Codec.(type) {
		case *codecs.H264, *codecs.H265:
			pt := info.VideoPT
			client.OnDataH26x(track, func(pts, dts int64, au [][]byte) {
				payload := AnnexBFromAU(au)
				if len(payload) == 0 {
					return
				}
				if len(onVideoFrame) > 0 && onVideoFrame[0] != nil {
					onVideoFrame[0]()
				}
				_ = session.FeedAvPacket(base.AvPacket{
					Payload:     payload,
					PayloadType: pt,
					Pts:         clockTicksToMilliseconds(pts, clockRate),
					Timestamp:   clockTicksToMilliseconds(dts, clockRate),
				})
			})
		case *codecs.MPEG4Audio:
			sampleRate := codec.Config.SampleRate
			if sampleRate <= 0 {
				sampleRate = 44100
			}
			client.OnDataMPEG4Audio(track, func(pts int64, aus [][]byte) {
				ts := clockTicksToMilliseconds(pts, clockRate)
				step := int64(0)
				if sampleRate > 0 {
					step = int64(1024 * 1000 / sampleRate)
				}
				for i, au := range aus {
					_ = session.FeedAvPacket(base.AvPacket{
						Payload:     au,
						PayloadType: base.AvPacketPtAac,
						Pts:         ts + int64(i)*step,
						Timestamp:   ts + int64(i)*step,
					})
				}
			})
		}
	}
	return nil
}

// HLSTrackInfo is the codec summary extracted from a gohlslib track list.
type HLSTrackInfo struct {
	VideoCodec      string
	AudioCodec      string
	VideoPT         base.AvPacketPt
	VideoParamSets  []byte
	AudioASC        []byte
	Playable        bool
	Recordable      bool
	UnsupportedHint string
}

// ClassifyHLSTracks maps gohlslib tracks onto lal-friendly codec metadata.
func ClassifyHLSTracks(tracks []*gohlslib.Track) (HLSTrackInfo, error) {
	var info HLSTrackInfo
	for _, track := range tracks {
		if track == nil {
			continue
		}
		switch codec := track.Codec.(type) {
		case *codecs.H264:
			info.VideoCodec = "h264"
			info.VideoPT = base.AvPacketPtAvc
			info.VideoParamSets = AnnexBFromAU([][]byte{codec.SPS, codec.PPS})
		case *codecs.H265:
			info.VideoCodec = "h265"
			info.VideoPT = base.AvPacketPtHevc
			info.VideoParamSets = AnnexBFromAU([][]byte{codec.VPS, codec.SPS, codec.PPS})
		case *codecs.MPEG4Audio:
			info.AudioCodec = "aac"
			if asc, err := codec.Config.Marshal(); err == nil {
				info.AudioASC = asc
			}
		case *codecs.Opus:
			info.UnsupportedHint = "opus audio is not supported"
		case *codecs.AV1:
			info.UnsupportedHint = "av1 video is not supported"
		case *codecs.VP9:
			info.UnsupportedHint = "vp9 video is not supported"
		}
	}
	switch info.VideoCodec {
	case "h264", "h265":
		info.Playable = true
		info.Recordable = true
		return info, nil
	default:
		if info.UnsupportedHint == "" {
			info.UnsupportedHint = ErrHLSPullUnsupportedCodec.Error()
		}
		return info, fmt.Errorf("%w: %s", ErrHLSPullUnsupportedCodec, info.UnsupportedHint)
	}
}

func clockTicksToMilliseconds(ticks int64, clockRate int) int64 {
	if clockRate <= 0 {
		return 0
	}
	return ticks * 1000 / int64(clockRate)
}

// AnnexBFromAU concatenates NAL units with 4-byte start codes.
func AnnexBFromAU(au [][]byte) []byte {
	total := 0
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		total += 4 + len(nalu)
	}
	if total == 0 {
		return nil
	}
	out := make([]byte, 0, total)
	start := []byte{0, 0, 0, 1}
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		out = append(out, start...)
		out = append(out, nalu...)
	}
	return out
}

func headerClient(baseClient *http.Client, headers map[string]string, playlistURL string, restrictOrigin bool) *http.Client {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	if len(headers) == 0 {
		return baseClient
	}
	transport := baseClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	allowedOrigin := ""
	if restrictOrigin {
		if u, err := url.Parse(playlistURL); err == nil {
			allowedOrigin = strings.ToLower(u.Scheme + "://" + u.Host)
		}
	}
	return &http.Client{
		Timeout:   baseClient.Timeout,
		Transport: &headerRoundTripper{base: transport, headers: headers, allowedOrigin: allowedOrigin},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if allowedOrigin != "" && strings.ToLower(req.URL.Scheme+"://"+req.URL.Host) != allowedOrigin {
				return errors.New("HLS redirect changed origin")
			}
			return nil
		},
	}
}

func engineSupportsCustomizePub(engine Engine) bool {
	if engine == nil {
		return false
	}
	if _, ok := engine.(*LalmaxHTTP); ok {
		return false
	}
	return true
}

type headerRoundTripper struct {
	base          http.RoundTripper
	headers       map[string]string
	allowedOrigin string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if h.allowedOrigin == "" || strings.ToLower(req.URL.Scheme+"://"+req.URL.Host) == h.allowedOrigin {
		for k, v := range h.headers {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				continue
			}
			req.Header.Set(k, v)
		}
	}
	return h.base.RoundTrip(req)
}
