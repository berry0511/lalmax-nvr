package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/emby"
	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/stretchr/testify/require"
)

type embyFlowPuller struct {
	request     media.StartHLSPullRequest
	playlist    string
	playlistURL string
}

func (p *embyFlowPuller) StartHLSPull(ctx context.Context, req media.StartHLSPullRequest) (*media.StreamSession, error) {
	p.request = req
	playlistURL, headers, err := req.PlaylistURLProvider(ctx)
	if err != nil {
		return nil, err
	}
	p.playlistURL = playlistURL
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, io.ErrUnexpectedEOF
	}
	data, err := io.ReadAll(response.Body)
	p.playlist = string(data)
	return &media.StreamSession{StreamID: req.StreamID, Protocol: "hls_pull"}, err
}

func (p *embyFlowPuller) StopHLSPull(context.Context, string) error { return nil }
func (p *embyFlowPuller) StopAllHLSPulls()                          {}
func (p *embyFlowPuller) PullingStreamIDs() []string                { return nil }

func TestEmbyAPIPublishFlowAgainstMockEmby(t *testing.T) {
	const token = "mock-emby-access-token"
	embyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Emby-Token"); got != token {
			t.Errorf("X-Emby-Token = %q, want token", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Users/user-1/Items":
			if r.URL.Query().Get("Limit") == "1" {
				_, _ = io.WriteString(w, `{"Items":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"Items":[{"Id":"movie-1","Name":"Mock Feature Film","Type":"Movie","Overview":"Emby test item","RunTimeTicks":3600000000,"ImageTags":{"Primary":"poster-1"}},{"Id":"series-1","Name":"Filtered Series","Type":"Series"}],"TotalRecordCount":2}`)
		case r.Method == http.MethodGet && r.URL.Path == "/Users/user-1/Items/movie-1":
			_, _ = io.WriteString(w, `{"Id":"movie-1","Name":"Mock Feature Film","Type":"Movie","Overview":"Emby test item","RunTimeTicks":3600000000,"ImageTags":{"Primary":"poster-1"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/Items/movie-1/PlaybackInfo":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PlaybackInfo body: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if body["UserId"] != "user-1" || body["EnableTranscoding"] != true {
				t.Errorf("unexpected PlaybackInfo request: %#v", body)
			}
			_, _ = io.WriteString(w, `{"MediaSources":[{"Id":"source-1"}],"PlaySessionId":"session-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/Videos/movie-1/master.m3u8":
			if r.URL.Query().Get("api_key") != token || r.URL.Query().Get("MediaSourceId") != "source-1" {
				t.Errorf("unexpected HLS request query: %v", r.URL.Query())
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer embyServer.Close()

	db, store := setupTestDB(t)
	defer db.Close()
	puller := &embyFlowPuller{}
	service := emby.NewService(db, puller)
	h := NewHandler(db, store, func(next http.Handler) http.Handler { return next }, nil, nil, "", nil, nil)
	h.SetEmbyService(service)
	router := h.Routes()

	put := doRequest(t, router, http.MethodPut, "/api/emby/config", strings.NewReader(`{"base_url":"`+embyServer.URL+`","user_id":"user-1","token":"`+token+`"}`), "", "")
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.NotContains(t, put.Body.String(), token)

	browse := doRequest(t, router, http.MethodGet, "/api/emby/library?q=Mock&limit=20", nil, "", "")
	require.Equal(t, http.StatusOK, browse.Code, browse.Body.String())
	require.Contains(t, browse.Body.String(), "Mock Feature Film")
	require.NotContains(t, browse.Body.String(), "Filtered Series")

	added := doRequest(t, router, http.MethodPost, "/api/emby/items", strings.NewReader(`{"emby_item_id":"movie-1"}`), "", "")
	require.Equal(t, http.StatusCreated, added.Code, added.Body.String())
	var item struct {
		ID       string `json:"id"`
		StreamID string `json:"stream_id"`
		Name     string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(added.Body.Bytes(), &item))
	require.Equal(t, "Mock Feature Film", item.Name)
	require.NotEmpty(t, item.StreamID)

	started := doRequest(t, router, http.MethodPut, "/api/emby/items/"+item.ID, strings.NewReader(`{"publish_enabled":true,"loop":false}`), "", "")
	require.Equal(t, http.StatusOK, started.Code, started.Body.String())
	require.Contains(t, started.Body.String(), `"publish_enabled":true`)
	require.Equal(t, item.StreamID, puller.request.StreamID)
	require.False(t, puller.request.RepeatOnEnd)
	require.True(t, puller.request.RestrictHeadersToOrigin)
	require.Contains(t, puller.playlistURL, "/Videos/movie-1/master.m3u8?")
	require.Equal(t, "#EXTM3U\n#EXT-X-ENDLIST\n", puller.playlist)

	// The pull manager reports end-of-stream with a nil completion error. For
	// one-shot playback the API state should then be taken offline automatically.
	puller.request.OnComplete(nil)
	items, err := service.ListItems(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.False(t, items[0].PublishEnabled)

	looping := doRequest(t, router, http.MethodPut, "/api/emby/items/"+item.ID, strings.NewReader(`{"publish_enabled":true,"loop":true}`), "", "")
	require.Equal(t, http.StatusOK, looping.Code, looping.Body.String())
	require.True(t, puller.request.RepeatOnEnd)
}
