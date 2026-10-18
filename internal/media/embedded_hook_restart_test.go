package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	lalmaxserver "github.com/q191201771/lalmax/server"
)

type restartHookProbe struct { fired chan lalmaxserver.HookEvent }
func (p *restartHookProbe) Name() string { return "restart-hook-probe" }
func (p *restartHookProbe) OnHookEvent(ev lalmaxserver.HookEvent) error {
	select { case p.fired <- ev: default: }
	return nil
}

func TestEmbeddedHookRegistrationSurvivesRestart(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join(t.TempDir(), "lalmax.json")
	raw, err := embeddedConfigJSON(EmbeddedLalmaxConfig{})
	if err != nil { t.Fatal(err) }
	var conf map[string]any
	if err := json.Unmarshal(raw, &conf); err != nil { t.Fatal(err) }
	max := conf["lalmax"].(map[string]any)
	max["rtc_config"] = map[string]any{"enable": false}
	max["http_config"] = map[string]any{"http_listen_addr": "127.0.0.1:0"}
	max["jt1078_config"] = map[string]any{"enable": false}
	max["fmp4_config"] = map[string]any{}
	lal := conf["lal"].(map[string]any)
	lal["rtsp"] = map[string]any{"enable": false}
	lal["httpflv"] = map[string]any{"enable": false}
	lal["httpts"] = map[string]any{"enable": true, "http_listen_addr": "127.0.0.1:0", "url_pattern": "/live/"}
	lal["log"] = map[string]any{"filename": "", "is_to_file": false, "is_to_stdout": false, "level": 5}
	raw, err = json.Marshal(conf)
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, raw, 0600); err != nil { t.Fatal(err) }
	engine, err := NewEmbeddedLalmax(EmbeddedLalmaxConfig{ConfigPath: path})
	if err != nil { t.Fatal(err) }
	probe := &restartHookProbe{fired: make(chan lalmaxserver.HookEvent, 4)}
	unsubscribe, err := engine.RegisterHookPlugin(probe, lalmaxserver.HookPluginOptions{
		Filter: lalmaxserver.NewHookEventFilter("", "", "", []string{lalmaxserver.HookEventStreamNotFound}),
	})
	if err != nil { t.Fatal(err) }
	t.Cleanup(unsubscribe)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func(){ _ = engine.Shutdown(context.Background()) })
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
		engine.Server().HTTPHandler().ServeHTTP(w,r)
	}))
	defer api.Close()
	engine.httpEngine, err = NewLalmaxHTTP(LalmaxHTTPConfig{BaseURL: api.URL})
	if err != nil { t.Fatal(err) }
	engine.LalmaxHTTP = engine.httpEngine
	for i:=0; i<2; i++ {
		if i==0 { err=engine.Start(ctx) } else { err=engine.Restart(ctx,1935,9000,false,false) }
		if err!=nil { t.Fatal(err) }
		engine.Server().HookHub().NotifyStreamNotFound(lalmaxserver.ZlmOnStreamNotFoundPayload{
			App:"live", Stream:"test-hook", Schema:"whep",
		})
		select {
		case got:= <-probe.fired:
			if got.Event != lalmaxserver.HookEventStreamNotFound { t.Fatalf("unexpected hook event: %s",got.Event) }
		case <-time.After(3*time.Second):
			t.Fatalf("hook not delivered after iteration %d",i)
		}
	}
}
