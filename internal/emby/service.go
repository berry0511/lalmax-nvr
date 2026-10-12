package emby

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib"
	"github.com/google/uuid"
	"github.com/lalmax-pro/lalmax-nvr/internal/config"
	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

var (
	ErrNotConfigured = errors.New("Emby server is not configured")
	ErrItemNotFound  = errors.New("Emby media item not found")
)

type Repository interface {
	storage.EmbyRepository
	SetCreatedStreamName(context.Context, string, string) error
	DeleteCreatedStream(context.Context, string) (bool, error)
}

type Service struct {
	db     Repository
	puller media.HLSPuller
	client *http.Client
	mu     sync.Mutex
	active map[string]bool
	timers map[string]*time.Timer
	epochs map[string]uint64
}

type ServerConfig struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	UserID     string `json:"user_id"`
	Token      string `json:"token,omitempty"`
	Configured bool   `json:"configured"`
	HasToken   bool   `json:"has_token"`
}

type MediaItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ItemType     string `json:"item_type"`
	Overview     string `json:"overview,omitempty"`
	ImageTag     string `json:"image_tag,omitempty"`
	RunTimeTicks int64  `json:"run_time_ticks,omitempty"`
}

func NewService(db Repository, puller media.HLSPuller) *Service {
	return &Service{
		db: db, puller: puller, active: make(map[string]bool), timers: make(map[string]*time.Timer), epochs: make(map[string]uint64),
		client: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many Emby redirects")
			}
			if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
				return errors.New("Emby redirect changed origin")
			}
			return nil
		}},
	}
}

func (s *Service) Config(ctx context.Context) (ServerConfig, error) {
	server, err := s.db.GetEmbyServer(ctx)
	if err != nil {
		return ServerConfig{}, err
	}
	if server == nil {
		return ServerConfig{Configured: false}, nil
	}
	return ServerConfig{Name: server.Name, BaseURL: server.BaseURL, UserID: server.UserID, Configured: true, HasToken: server.Token != ""}, nil
}

func (s *Service) SaveConfig(ctx context.Context, cfg ServerConfig) (ServerConfig, error) {
	base, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return ServerConfig{}, err
	}
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.Token == "" {
		old, err := s.db.GetEmbyServer(ctx)
		if err != nil {
			return ServerConfig{}, err
		}
		if old != nil {
			cfg.Token = openSecret(old.Token)
		}
	}
	if cfg.UserID == "" || cfg.Token == "" {
		return ServerConfig{}, errors.New("Emby user ID and access token are required")
	}
	server := storage.EmbyServer{ID: "default", Name: cfg.Name, BaseURL: base, UserID: cfg.UserID, Token: cfg.Token}
	if err := s.verify(ctx, server); err != nil {
		return ServerConfig{}, err
	}
	server.Token = sealSecret(server.Token)
	if err := s.db.SaveEmbyServer(ctx, server); err != nil {
		return ServerConfig{}, err
	}
	return ServerConfig{Name: server.Name, BaseURL: server.BaseURL, UserID: server.UserID, Configured: true, HasToken: true}, nil
}

func (s *Service) Browse(ctx context.Context, q string, limit, offset int) ([]MediaItem, int, error) {
	server, err := s.server(ctx)
	if err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode"}, "Fields": {"Overview,PrimaryImageAspectRatio,RunTimeTicks"}, "Limit": {fmt.Sprint(limit)}, "StartIndex": {fmt.Sprint(offset)}}
	if strings.TrimSpace(q) != "" {
		search := strings.TrimSpace(q)
		if len(search) > 256 {
			search = search[:256]
		}
		query.Set("SearchTerm", search)
	}
	endpoint := server.BaseURL + "/Users/" + url.PathEscape(server.UserID) + "/Items?" + query.Encode()
	var response struct {
		Items []struct {
			ID, Name, Type, Overview string
			RunTimeTicks             int64
			ImageTags                map[string]string
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := s.doJSON(ctx, server, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, 0, err
	}
	items := make([]MediaItem, 0, len(response.Items))
	for _, item := range response.Items {
		if item.ID == "" || (item.Type != "Movie" && item.Type != "Episode") {
			continue
		}
		items = append(items, MediaItem{ID: item.ID, Name: item.Name, ItemType: item.Type, Overview: item.Overview, ImageTag: item.ImageTags["Primary"], RunTimeTicks: item.RunTimeTicks})
	}
	return items, response.TotalRecordCount, nil
}

func (s *Service) AddItem(ctx context.Context, externalID string) (*storage.EmbyItem, error) {
	items, err := s.BrowseItem(ctx, externalID)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(externalID))
	item := storage.EmbyItem{ID: uuid.NewString(), EmbyItemID: items.ID, Name: items.Name, ItemType: items.ItemType, Overview: items.Overview, ImageTag: items.ImageTag, RunTimeTicks: items.RunTimeTicks, StreamID: "emby_" + hex.EncodeToString(sum[:12])}
	if err := s.db.SaveEmbyItem(ctx, item); err != nil {
		return nil, err
	}
	if err := s.db.SetCreatedStreamName(ctx, item.StreamID, item.Name); err != nil {
		return nil, err
	}
	return s.db.GetEmbyItem(ctx, items.ID)
}

func (s *Service) BrowseItem(ctx context.Context, id string) (MediaItem, error) {
	server, err := s.server(ctx)
	if err != nil {
		return MediaItem{}, err
	}
	endpoint := server.BaseURL + "/Users/" + url.PathEscape(server.UserID) + "/Items/" + url.PathEscape(id) + "?Fields=Overview,RunTimeTicks"
	var item struct {
		ID, Name, Type, Overview string
		RunTimeTicks             int64
		ImageTags                map[string]string `json:"ImageTags"`
	}
	if err := s.doJSON(ctx, server, http.MethodGet, endpoint, nil, &item); err != nil {
		return MediaItem{}, err
	}
	if item.ID == "" || (item.Type != "Movie" && item.Type != "Episode") {
		return MediaItem{}, ErrItemNotFound
	}
	return MediaItem{ID: item.ID, Name: item.Name, ItemType: item.Type, Overview: item.Overview, ImageTag: item.ImageTags["Primary"], RunTimeTicks: item.RunTimeTicks}, nil
}

func (s *Service) ListItems(ctx context.Context) ([]storage.EmbyItem, error) {
	return s.db.ListEmbyItems(ctx)
}
func (s *Service) GetItem(ctx context.Context, id string) (*storage.EmbyItem, error) {
	return s.db.GetEmbyItem(ctx, id)
}

func (s *Service) SetPublish(ctx context.Context, id string, enabled, loop bool) error {
	item, err := s.db.GetEmbyItem(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrItemNotFound
	}
	if enabled && s.puller == nil {
		return media.ErrHLSPullEmbeddedOnly
	}
	if err := s.db.SetEmbyItemPublish(ctx, item.ID, enabled, loop); err != nil {
		return err
	}
	if !enabled {
		s.mu.Lock()
		s.epochs[item.StreamID]++
		s.stopCompletionTimerLocked(item.StreamID)
		delete(s.active, item.StreamID)
		s.mu.Unlock()
		if s.puller == nil {
			return nil
		}
		return s.puller.StopHLSPull(ctx, item.StreamID)
	}
	s.mu.Lock()
	current, ok := s.active[item.StreamID]
	s.mu.Unlock()
	if ok && current != loop {
		s.mu.Lock()
		delete(s.active, item.StreamID)
		s.mu.Unlock()
		_ = s.puller.StopHLSPull(ctx, item.StreamID)
	}
	if err := s.startItem(ctx, *item, loop); err != nil {
		_ = s.db.SetEmbyItemPublish(ctx, item.ID, false, loop)
		return err
	}
	return nil
}

func (s *Service) startItem(ctx context.Context, item storage.EmbyItem, loop bool) error {
	s.mu.Lock()
	if current, ok := s.active[item.StreamID]; ok && current == loop {
		s.mu.Unlock()
		return nil
	}
	s.epochs[item.StreamID]++
	epoch := s.epochs[item.StreamID]
	s.active[item.StreamID] = loop
	s.mu.Unlock()
	provider := func(ctx context.Context) (string, map[string]string, error) { return s.playlist(ctx, item.EmbyItemID) }
	_, err := s.puller.StartHLSPull(ctx, media.StartHLSPullRequest{
		StreamID: item.StreamID, PullRetryNum: -1, RepeatOnEnd: loop,
		PlaybackDuration:        time.Duration(item.RunTimeTicks) * 100 * time.Nanosecond,
		RestrictHeadersToOrigin: true, PlaylistURLProvider: provider,
		OnComplete: func(_ error) {
			s.mu.Lock()
			if s.epochs[item.StreamID] != epoch {
				s.mu.Unlock()
				return
			}
			s.stopCompletionTimerLocked(item.StreamID)
			delete(s.active, item.StreamID)
			s.mu.Unlock()
			current, err := s.db.GetEmbyItem(context.Background(), item.ID)
			if err == nil && current != nil && !current.Loop {
				_ = s.db.SetEmbyItemPublish(context.Background(), item.ID, false, current.Loop)
			}
		},
	})
	if err != nil {
		s.mu.Lock()
		if s.epochs[item.StreamID] == epoch {
			delete(s.active, item.StreamID)
		}
		s.mu.Unlock()
	} else {
		s.scheduleCompletionFallback(item, loop, epoch)
	}
	return err
}

// scheduleCompletionFallback clears the published state even when a live-style
// Emby HLS playlist never signals EOS or a media-engine shutdown stalls.
func (s *Service) scheduleCompletionFallback(item storage.EmbyItem, loop bool, epoch uint64) {
	if item.RunTimeTicks <= 0 {
		return
	}
	duration := time.Duration(item.RunTimeTicks)*100*time.Nanosecond + 10*time.Second
	streamID := item.StreamID
	s.mu.Lock()
	current, active := s.active[streamID]
	if s.epochs[streamID] != epoch || !active || current != loop {
		s.mu.Unlock()
		return
	}
	s.stopCompletionTimerLocked(streamID)
	s.timers[streamID] = time.AfterFunc(duration, func() {
		s.finishAfterDuration(item, loop, epoch)
	})
	s.mu.Unlock()
}

func (s *Service) finishAfterDuration(item storage.EmbyItem, loop bool, epoch uint64) {
	s.mu.Lock()
	current, active := s.active[item.StreamID]
	if s.epochs[item.StreamID] != epoch || !active || current != loop {
		s.mu.Unlock()
		return
	}
	delete(s.timers, item.StreamID)
	delete(s.active, item.StreamID)
	s.epochs[item.StreamID]++
	s.mu.Unlock()

	ctx := context.Background()
	if !loop {
		_ = s.db.SetEmbyItemPublish(ctx, item.ID, false, false)
		go func() { _ = s.puller.StopHLSPull(ctx, item.StreamID) }()
		return
	}

	go func() {
		_ = s.puller.StopHLSPull(ctx, item.StreamID)
		current, err := s.db.GetEmbyItem(ctx, item.ID)
		if err == nil && current != nil && current.PublishEnabled && current.Loop {
			_ = s.startItem(ctx, *current, true)
		}
	}()
}

func (s *Service) stopCompletionTimerLocked(streamID string) {
	if timer := s.timers[streamID]; timer != nil {
		timer.Stop()
		delete(s.timers, streamID)
	}
}

func (s *Service) Recover(ctx context.Context) error {
	items, err := s.db.ListEmbyItems(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.PublishEnabled {
			if err := s.startItem(ctx, item, item.Loop); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	for id := range s.timers {
		s.stopCompletionTimerLocked(id)
	}
	clear(s.active)
	s.mu.Unlock()
	if s.puller != nil {
		for _, id := range s.puller.PullingStreamIDs() {
			if strings.HasPrefix(id, "emby_") {
				_ = s.puller.StopHLSPull(context.Background(), id)
			}
		}
	}
}

func (s *Service) RemoveItem(ctx context.Context, id string) error {
	item, err := s.db.GetEmbyItem(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrItemNotFound
	}
	if s.puller != nil {
		s.mu.Lock()
		s.epochs[item.StreamID]++
		s.stopCompletionTimerLocked(item.StreamID)
		delete(s.active, item.StreamID)
		s.mu.Unlock()
		_ = s.puller.StopHLSPull(ctx, item.StreamID)
	}
	if err := s.db.DeleteEmbyItem(ctx, item.ID); err != nil {
		return err
	}
	_, err = s.db.DeleteCreatedStream(ctx, item.StreamID)
	return err
}

func (s *Service) server(ctx context.Context) (storage.EmbyServer, error) {
	server, err := s.db.GetEmbyServer(ctx)
	if err != nil {
		return storage.EmbyServer{}, err
	}
	if server == nil {
		return storage.EmbyServer{}, ErrNotConfigured
	}
	server.Token = openSecret(server.Token)
	return *server, nil
}

func (s *Service) verify(ctx context.Context, server storage.EmbyServer) error {
	endpoint := server.BaseURL + "/Users/" + url.PathEscape(server.UserID) + "/Items?Limit=1"
	return s.doJSON(ctx, server, http.MethodGet, endpoint, nil, &map[string]any{})
}

func (s *Service) playlist(ctx context.Context, itemID string) (string, map[string]string, error) {
	server, err := s.server(ctx)
	if err != nil {
		return "", nil, err
	}
	playReq := map[string]any{"UserId": server.UserID, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "IsPlayback": true, "DeviceProfile": map[string]any{"Name": "lalmax-nvr", "DirectPlayProfiles": []any{}, "TranscodingProfiles": []any{map[string]any{"Container": "ts", "Type": "Video", "VideoCodec": "h264,hevc", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming", "MinSegments": 1, "SegmentLength": 4, "BreakOnNonKeyFrames": true}}}}
	body, _ := json.Marshal(playReq)
	endpoint := server.BaseURL + "/Items/" + url.PathEscape(itemID) + "/PlaybackInfo"
	var info struct {
		MediaSources []struct {
			ID string `json:"Id"`
		} `json:"MediaSources"`
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := s.doJSON(ctx, server, http.MethodPost, endpoint, body, &info); err != nil {
		return "", nil, err
	}
	if len(info.MediaSources) == 0 || info.MediaSources[0].ID == "" {
		return "", nil, errors.New("Emby returned no playable media source")
	}
	params := url.Values{"UserId": {server.UserID}, "DeviceId": {"lalmax-nvr"}, "MediaSourceId": {info.MediaSources[0].ID}, "PlaySessionId": {info.PlaySessionID}, "StartTimeTicks": {"0"}, "VideoCodec": {"h264,hevc"}, "AudioCodec": {"aac"}, "SegmentContainer": {"ts"}, "MinSegments": {"1"}, "SegmentLength": {"4"}, "BreakOnNonKeyFrames": {"true"}, "api_key": {server.Token}}
	return server.BaseURL + "/Videos/" + url.PathEscape(itemID) + "/master.m3u8?" + params.Encode(), map[string]string{"X-Emby-Token": server.Token}, nil
}

func (s *Service) doJSON(ctx context.Context, server storage.EmbyServer, method, endpoint string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", server.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby API returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("Emby URL must be an absolute HTTP or HTTPS URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Emby URL must not include credentials, query, or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func sealSecret(value string) string {
	key := config.GetEncryptionKey()
	if key == nil {
		return value
	}
	sealed, err := config.Encrypt(value, key)
	if err != nil {
		return value
	}
	return sealed
}

func openSecret(value string) string {
	key := config.GetEncryptionKey()
	if key == nil {
		return value
	}
	opened, err := config.Decrypt(value, key)
	if err != nil {
		return value
	}
	return opened
}

func IsEndOfStream(err error) bool { return errors.Is(err, gohlslib.ErrClientEOS) }
