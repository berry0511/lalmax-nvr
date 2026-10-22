package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherReloadObserversAreReentrant(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "config", "config.example.yaml"))
	if err != nil { t.Fatal(err) }
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, src, 0600); err != nil { t.Fatal(err) }
	cfg, err := Load(path)
	if err != nil { t.Fatal(err) }
	w, err := NewWatcher(cfg, path)
	if err != nil { t.Fatal(err) }
	observed := make(chan struct{}, 1)
	w.Subscribe(func(got *Config) {
		if w.Config() != got { t.Error("observer received obsolete config") }
		w.Subscribe(func(*Config){})
		observed <- struct{}{}
	})
	if err := os.WriteFile(path, append(src, []byte("\n# force new config\n")...), 0600); err != nil { t.Fatal(err) }
	done := make(chan error, 1)
	go func(){ done <- w.Reload() }()
	select {
	case err := <-done:
		if err != nil { t.Fatal(err) }
	case <-time.After(3*time.Second):
		t.Fatal("Watcher.Reload deadlocked a reentrant observer")
	}
	select {
	case <-observed:
	default:
		t.Fatal("reload observer not called")
	}
}
