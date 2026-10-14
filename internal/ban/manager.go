package ban

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
)

var logger = slog.Default().With("component", "ban")

// Manager enforces stream bans via lal's IAuthentication interface.
// All protocols (RTMP, RTSP, SRT, WHIP, GB28181) are checked uniformly.
type Manager struct {
	db            storage.StreamBanRepository
	kickFunc      func(ctx context.Context, sessionID string) error
	publishTokens map[string]map[string]string
}

func NewManager(db storage.StreamBanRepository) *Manager {
	return &Manager{db: db}
}

// SetKickFunc sets the function used to kick currently active banned streams.
func (m *Manager) SetKickFunc(fn func(ctx context.Context, sessionID string) error) {
	m.kickFunc = fn
}

// SetRTMPStreamKeys installs the required publish token for each RTMP stream ID.
func (m *Manager) SetRTMPStreamKeys(keys map[string]string) {
	m.SetPublishTokens("rtmp", keys)
}

// SetPublishTokens installs per-stream publish credentials for one protocol.
func (m *Manager) SetPublishTokens(protocol string, keys map[string]string) {
	if m.publishTokens == nil {
		m.publishTokens = make(map[string]map[string]string)
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	cloned := make(map[string]string, len(keys))
	for streamID, token := range keys {
		cloned[streamID] = token
	}
	m.publishTokens[protocol] = cloned
}

// OnPubStart checks if the stream is banned. Returns error to reject the session.
func (m *Manager) OnPubStart(info base.PubStartInfo) error {
	if strings.EqualFold(info.Protocol, base.SessionProtocolRtmpStr) {
		params, err := url.ParseQuery(info.UrlParam)
		if err != nil {
			return fmt.Errorf("invalid RTMP publish parameters for stream %q", info.StreamName)
		}
		return m.AuthorizePublish("rtmp", info.StreamName, params.Get("stream_key"))
	}
	return m.rejectBannedStream(info.StreamName, info.Protocol, false)
}

// AuthorizePublish checks a protocol token and rejects banned stream IDs.
func (m *Manager) AuthorizePublish(protocol, streamID, token string) error {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	want := m.publishTokens[protocol][streamID]
	if want == "" {
		return fmt.Errorf("%s publishing is not configured for stream %q", protocol, streamID)
	}
	if !secureTokenEqual(token, want) {
		return fmt.Errorf("invalid %s publish token for stream %q", protocol, streamID)
	}
	return m.rejectBannedStream(streamID, protocol, true)
}

func (m *Manager) rejectBannedStream(streamID, protocol string, failClosed bool) error {
	ban, err := m.db.GetStreamBan(context.Background(), streamID)
	if err != nil {
		logger.Error("failed to check ban", "stream", streamID, "error", err)
		if !failClosed {
			return nil
		}
		return fmt.Errorf("failed to authorize %s stream %q", protocol, streamID)
	}
	if ban != nil {
		reason := ban.Reason
		if reason == "" {
			reason = "banned"
		}
		logger.Info("rejected banned stream", "stream", streamID, "protocol", protocol, "reason", reason)
		return fmt.Errorf("stream %q is banned: %s", streamID, reason)
	}
	return nil
}

func secureTokenEqual(got, want string) bool {
	return got != "" && len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// OnSubStart allows all subscriptions.
func (m *Manager) OnSubStart(info base.SubStartInfo) error {
	return nil
}

// OnHls allows all HLS requests.
func (m *Manager) OnHls(streamName, urlParam string) error {
	return nil
}

// Ban adds a ban for a stream.
func (m *Manager) Ban(ctx context.Context, streamID, reason string, expiresAt *time.Time) error {
	ban := &storage.StreamBan{
		StreamID:  streamID,
		Reason:    reason,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	if err := m.db.InsertStreamBan(ctx, ban); err != nil {
		return err
	}
	logger.Info("stream banned", "stream_id", streamID, "reason", reason)
	return nil
}

// Unban removes a ban for a stream.
func (m *Manager) Unban(ctx context.Context, streamID string) error {
	return m.db.DeleteStreamBan(ctx, streamID)
}

// ListBans returns all active bans.
func (m *Manager) ListBans(ctx context.Context) ([]storage.StreamBan, error) {
	return m.db.ListStreamBans(ctx)
}

// Ensure Manager implements logic.IAuthentication
var _ logic.IAuthentication = (*Manager)(nil)
