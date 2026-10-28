package transcode

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recoveryRepository struct {
	profile Profile
	job     Job
}

func (r *recoveryRepository) ListTranscodeProfiles(context.Context) ([]Profile, error) {
	return []Profile{r.profile}, nil
}
func (r *recoveryRepository) ListTranscodeJobs(context.Context) ([]Job, error) {
	return []Job{r.job}, nil
}
func (*recoveryRepository) SaveTranscodeProfile(context.Context, Profile, uint64) (Profile, error) {
	return Profile{}, errors.New("not used")
}
func (*recoveryRepository) DeleteTranscodeProfile(context.Context, string, uint64) error {
	return errors.New("not used")
}
func (*recoveryRepository) SaveTranscodeJob(context.Context, Job, uint64) (Job, error) {
	return Job{}, errors.New("not used")
}
func (*recoveryRepository) DeleteTranscodeJob(context.Context, string, uint64) error {
	return errors.New("not used")
}
func (*recoveryRepository) SetTranscodeJobStopped(context.Context, string, uint64, bool) error {
	return errors.New("not used")
}

func TestAlwaysHealthyRunResetsRecoveryBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := testProfile(t)
	j := managerJob(t, "camera", p)
	executor := &fakeExecutor{}
	executor.ready.Store(true)
	manager := NewManager(ctx, executor, 1)
	manager.healthTimeout = 20 * time.Millisecond
	defer manager.Close(context.Background())
	first, err := manager.Acquire(j, p)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		runtime, _ := manager.Status(j.ID)
		if runtime.State == Running {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job did not become healthy")
		case <-time.After(5 * time.Millisecond):
		}
	}
	probes := NewProbeService(nil)
	cfg := Config{Enabled: true}
	fingerprint, err := ProbeFingerprint(cfg.Tools(), p)
	if err != nil {
		t.Fatal(err)
	}
	probes.reports[p.ID] = CapabilityReport{CanEnable: true, Fingerprint: fingerprint, CheckedAt: time.Now()}
	service := NewService(&recoveryRepository{profile: p, job: j}, manager, probes, cfg)
	service.retry[j.ID] = retryState{generation: j.Generation, attempts: 8, next: time.Now().Add(time.Minute)}
	service.ReconcileAlways(ctx)
	retryBeforeReset, exists := service.retry[j.ID]
	if !exists || retryBeforeReset.healthySince.IsZero() {
		t.Fatal("short healthy run must preserve backoff and start the health interval")
	}
	retryBeforeReset.healthySince = time.Now().Add(-recoveryHealthyResetInterval - time.Second)
	service.retry[j.ID] = retryBeforeReset
	service.ReconcileAlways(ctx)
	if _, exists := service.retry[j.ID]; exists {
		t.Fatal("sustained healthy run did not clear earlier backoff")
	}

	manager.mu.Lock()
	done := manager.work[first.Key].done
	manager.mu.Unlock()
	executor.ready.Store(false)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("lost media did not stop")
	}
	if errs := service.ReconcileAlways(ctx); len(errs) != 0 {
		t.Fatalf("restart rejected: %v", errs)
	}
	retry := service.retry[j.ID]
	if retry.attempts != 1 {
		t.Fatalf("expected first retry after recovery, got %d", retry.attempts)
	}
	if delay := time.Until(retry.next); delay < 4*time.Second || delay > 5*time.Second {
		t.Fatalf("expected initial backoff, got %v", delay)
	}
}

func TestAlwaysShortHealthyPeriodKeepsBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := testProfile(t)
	j := managerJob(t, "flapping", p)
	executor := &fakeExecutor{}
	executor.ready.Store(true)
	manager := NewManager(ctx, executor, 1)
	manager.healthTimeout = 20 * time.Millisecond
	defer manager.Close(context.Background())
	work, err := manager.Acquire(j, p)
	if err != nil {
		t.Fatal(err)
	}
	for {
		runtime, _ := manager.Status(j.ID)
		if runtime.State == Running {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("job did not become healthy")
		case <-time.After(5 * time.Millisecond):
		}
	}
	service := NewService(&recoveryRepository{profile: p, job: j}, manager, NewProbeService(nil), Config{Enabled: true})
	service.retry[j.ID] = retryState{generation: j.Generation, attempts: 5, next: time.Now().Add(time.Minute)}
	service.ReconcileAlways(ctx)
	if service.retry[j.ID].healthySince.IsZero() {
		t.Fatal("health interval not recorded")
	}
	manager.mu.Lock()
	done := manager.work[work.Key].done
	manager.mu.Unlock()
	executor.ready.Store(false)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("lost media did not stop")
	}
	service.ReconcileAlways(ctx)
	retry := service.retry[j.ID]
	if retry.attempts != 5 || !retry.healthySince.IsZero() {
		t.Fatalf("short healthy period reset backoff: %+v", retry)
	}
}
