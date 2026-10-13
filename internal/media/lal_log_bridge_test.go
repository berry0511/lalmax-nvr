package media

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
	"github.com/stretchr/testify/require"

	"github.com/lalmax-pro/lalmax-nvr/internal/middleware"
)

func TestLalLogHookWritesTaggedEntryToServiceLogRing(t *testing.T) {
	ring := middleware.NewLogRing(8)
	previous := slog.Default()
	slog.SetDefault(slog.New(middleware.NewRingHandler(ring, slog.LevelDebug)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	line := " INFO 2026/10/13 12:00:00.123 lal stream started"
	lalLogHook()(nazalog.LevelInfo, []byte(line))

	entries := ring.Snapshot(middleware.LogRingFilter{MinLevel: slog.LevelDebug, Limit: 8})
	require.Len(t, entries, 1)
	require.Equal(t, "lal/lalmax", entries[0].Message)
	require.Equal(t, "lal/lalmax", entries[0].Attrs["component"])
	require.Equal(t, "LevelInfo", entries[0].Attrs["lal_level"])
	require.Equal(t, "INFO 2026/10/13 12:00:00.123 lal stream started", entries[0].Attrs["lal_message"])
}

func TestLalConfigInitializationPreservesLogBridge(t *testing.T) {
	ring := middleware.NewLogRing(32)
	previousSlog := slog.Default()
	slog.SetDefault(slog.New(middleware.NewRingHandler(ring, slog.LevelDebug)))
	t.Cleanup(func() { slog.SetDefault(previousSlog) })

	previousLal := base.Log.GetOption()
	t.Cleanup(func() { _ = base.Log.Init(func(current *nazalog.Option) { *current = previousLal }) })
	installLalLogBridge()

	logic.LoadConfAndInitLog([]byte(`{"log":{"level":2,"filename":"/dev/null","is_to_stdout":false}}`))
	base.Log.Info("bridge survived config initialization")

	entries := ring.Snapshot(middleware.LogRingFilter{MinLevel: slog.LevelDebug, Limit: 32})
	require.NotEmpty(t, entries)
	var found bool
	for _, entry := range entries {
		if message, ok := entry.Attrs["lal_message"].(string); ok && strings.Contains(message, "bridge survived config initialization") {
			found = true
			require.Equal(t, "lal/lalmax", entry.Attrs["component"])
		}
	}
	require.True(t, found, "embedded lal logger output should reach the NVR service-log ring")
}
