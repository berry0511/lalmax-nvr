package streamhistory

import (
	"context"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

type historyRepoFake struct {
	rows []*storage.StreamHistory
}

func (f *historyRepoFake) InsertStreamHistory(_ context.Context, history *storage.StreamHistory) error {
	copy := *history
	f.rows = append(f.rows, &copy)
	return nil
}

func (f *historyRepoFake) FinishStreamHistory(_ context.Context, streamID, sessionID string, endedAt time.Time, reason, detail string, _, _ uint64) error {
	for _, row := range f.rows {
		if row.StreamID == streamID && row.SessionID == sessionID && row.EndedAt == nil {
			row.EndedAt = &endedAt
			row.DurationSec = endedAt.Sub(row.StartedAt).Seconds()
			row.OfflineReason = reason
			row.OfflineDetail = detail
			if row.MediaStartedAt != nil && row.MediaEndedAt == nil {
				row.MediaEndedAt = &endedAt
			}
			break
		}
	}
	return nil
}

func (f *historyRepoFake) FinalizeOpenStreamHistories(context.Context, time.Time, string) error {
	return nil
}

func (f *historyRepoFake) RecordStreamHistoryMediaState(_ context.Context, streamID string, at time.Time, active bool) error {
	for i := len(f.rows) - 1; i >= 0; i-- {
		row := f.rows[i]
		if row.StreamID != streamID || row.EndedAt != nil {
			continue
		}
		if active {
			if row.MediaStartedAt == nil {
				row.MediaStartedAt = &at
			}
			row.MediaEndedAt = nil
		} else if row.MediaStartedAt != nil {
			row.MediaEndedAt = &at
		}
		break
	}
	return nil
}

func TestIPTVHistoryEndsWhenMediaGoesIdleAndRestartsOnRecovery(t *testing.T) {
	repo := &historyRepoFake{}
	manager := NewManager(repo, nil)
	streamID := "iptv_test"
	taskStarted := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	mediaStarted := taskStarted.Add(5 * time.Second)
	mediaStopped := mediaStarted.Add(time.Minute)
	mediaRecovered := mediaStopped.Add(30 * time.Second)
	taskStopped := mediaRecovered.Add(time.Minute)

	manager.RecordHLSPullSessionState(streamID, true, taskStarted, "", "")
	if len(repo.rows) != 0 {
		t.Fatal("pull task start must not be recorded as online media")
	}

	manager.RecordMediaState(streamID, true, mediaStarted)
	if len(repo.rows) != 1 || !repo.rows[0].StartedAt.Equal(mediaStarted) {
		t.Fatalf("expected history to begin when media becomes active, got %#v", repo.rows)
	}

	manager.RecordMediaState(streamID, false, mediaStopped)
	first := repo.rows[0]
	if first.EndedAt == nil || !first.EndedAt.Equal(mediaStopped) {
		t.Fatalf("expected idle media interval to end at %s, got %#v", mediaStopped, first.EndedAt)
	}
	if first.OfflineReason != "media_stopped" {
		t.Fatalf("expected media_stopped reason, got %q", first.OfflineReason)
	}
	if first.OfflineDetail != "Media engine reported that the stream group stopped." {
		t.Fatalf("expected a diagnostic detail describing the stop event, got %q", first.OfflineDetail)
	}

	manager.RecordMediaState(streamID, true, mediaRecovered)
	manager.RecordHLSPullAttemptError(streamID, "unexpected HTTP status code: 503")
	mediaStoppedAgain := mediaRecovered.Add(time.Minute)
	manager.RecordMediaState(streamID, false, mediaStoppedAgain)
	if len(repo.rows) != 2 {
		t.Fatalf("expected a new history interval after media recovery, got %d rows", len(repo.rows))
	}
	second := repo.rows[1]
	if !second.StartedAt.Equal(mediaRecovered) || second.EndedAt == nil || !second.EndedAt.Equal(mediaStoppedAgain) {
		t.Fatalf("unexpected recovered media interval: %#v", second)
	}
	if second.OfflineReason != "upstream_error" || second.OfflineDetail != "unexpected HTTP status code: 503" {
		t.Fatalf("expected upstream attempt details, got reason=%q detail=%q", second.OfflineReason, second.OfflineDetail)
	}

	manager.RecordMediaState(streamID, true, taskStopped)
	manager.RecordHLSPullSessionState(streamID, false, taskStopped.Add(time.Minute), "upstream_error", "playlist request failed")
	third := repo.rows[2]
	if third.EndedAt == nil || third.OfflineReason != "upstream_error" || third.OfflineDetail != "playlist request failed" {
		t.Fatalf("expected final task error details, got %#v", third)
	}
}
