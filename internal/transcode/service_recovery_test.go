package transcode

import (
	"context"
	"errors"
	"slices"
	"sync"
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

type multiRecoveryRepository struct {
	recoveryRepository
	mu       sync.Mutex
	profiles []Profile
	jobs     []Job
}

type successfulBlockingProbe struct {
	once             sync.Once
	entered, release chan struct{}
}

func (p *successfulBlockingProbe) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if slices.Contains(args, "-show_streams") {
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":320,"height":240},{"codec_type":"audio","codec_name":"aac","sample_rate":"48000","channels":1}]}`), nil
	}
	return []byte(" V..... libx264 encoder\n A..... aac encoder\n"), nil
}

func (r *multiRecoveryRepository) ListTranscodeProfiles(context.Context) ([]Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Profile(nil), r.profiles...), nil
}
func (r *multiRecoveryRepository) ListTranscodeJobs(context.Context) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Job(nil), r.jobs...), nil
}
func (r *multiRecoveryRepository) SetTranscodeJobStopped(_ context.Context, id string, generation uint64, stopped bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.jobs {
		if r.jobs[i].ID == id && r.jobs[i].Generation == generation {
			r.jobs[i].StoppedByUser = stopped
			return nil
		}
	}
	return errors.New("missing job")
}

func TestAlwaysProbeDoesNotBlockStopOrRestartStoppedJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := testProfile(t)
	j := managerJob(t, "stop-during-probe", p)
	repo := &multiRecoveryRepository{profiles: []Profile{p}, jobs: []Job{j}}
	runner := &successfulBlockingProbe{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(runner.release) })
	executor := &fakeExecutor{}
	manager := NewManager(ctx, executor, 1)
	defer manager.Close(context.Background())
	probes := NewProbeService(runner)
	service := NewService(repo, manager, probes, Config{Enabled: true})
	done := make(chan struct{})
	go func() { defer close(done); service.ReconcileAlways(ctx) }()
	select {
	case <-runner.entered:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- service.Stop(ctx, j.ID, j.Generation) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop blocked behind the probe")
	}
	release.Do(func() { close(runner.release) })
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("reconciliation did not finish")
	}
	if report, ok := probes.Result(p.ID); !ok || !report.CanEnable {
		t.Fatal("test must finish a successful probe before checking stale startup")
	}
	if executor.starts.Load() != 0 {
		t.Fatal("stopped job started after probe finished")
	}
}

func TestAlwaysTimedOutProbeDoesNotBackoffUnexaminedJob(t *testing.T) {
	p1 := testProfile(t)
	p2 := p1
	p2.ID = "good-profile"
	j1, j2 := managerJob(t, "slow", p1), managerJob(t, "good", p2)
	repo := &multiRecoveryRepository{profiles: []Profile{p1, p2}, jobs: []Job{j1, j2}}
	runner := &blockingProbe{entered: make(chan struct{}), release: make(chan struct{})}
	executor := &fakeExecutor{}
	manager := NewManager(context.Background(), executor, 2)
	defer manager.Close(context.Background())
	probes := NewProbeService(runner)
	cfg := Config{Enabled: true}
	fingerprint, err := ProbeFingerprint(cfg.Tools(), p2)
	if err != nil {
		t.Fatal(err)
	}
	probes.reports[p2.ID] = CapabilityReport{CanEnable: true, Fingerprint: fingerprint, CheckedAt: time.Now()}
	service := NewService(repo, manager, probes, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	service.ReconcileAlways(ctx)
	if _, exists := service.retry[j2.ID]; exists {
		t.Fatal("unexamined job was assigned backoff")
	}
	next, nextCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer nextCancel()
	if errs := service.ReconcileAlways(next); len(errs) != 0 {
		t.Fatalf("healthy job could not start: %v", errs)
	}
	if _, ok := manager.Status(j2.ID); !ok {
		t.Fatal("healthy job was starved by timed-out probe")
	}
}

func TestAlwaysProbeDoesNotStartRecreatedJobFromOldGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := testProfile(t)
	j := managerJob(t, "recreated", p)
	repo := &multiRecoveryRepository{profiles: []Profile{p}, jobs: []Job{j}}
	runner := &successfulBlockingProbe{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(runner.release) })
	executor := &fakeExecutor{}
	manager := NewManager(ctx, executor, 1)
	defer manager.Close(context.Background())
	probes := NewProbeService(runner)
	service := NewService(repo, manager, probes, Config{Enabled: true})
	done := make(chan struct{})
	go func() { defer close(done); service.ReconcileAlways(ctx) }()
	select {
	case <-runner.entered:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	repo.mu.Lock()
	repo.jobs[0].Generation++ // same ID now denotes a new persisted generation
	repo.mu.Unlock()
	release.Do(func() { close(runner.release) })
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("reconciliation did not finish")
	}
	if report, ok := probes.Result(p.ID); !ok || !report.CanEnable {
		t.Fatal("test did not complete a successful probe")
	}
	if executor.starts.Load() != 0 {
		t.Fatal("stale generation was started after probe")
	}
}

func TestSharedRestartBeginsNewHealthyInterval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := testProfile(t)
	j, peer := managerJob(t, "owner", p), managerJob(t, "peer", p)
	executor := &fakeExecutor{}
	executor.ready.Store(true)
	manager := NewManager(ctx, executor, 1)
	defer manager.Close(context.Background())
	first, err := manager.Acquire(j, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Acquire(peer, p); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	old := manager.work[first.Key]
	manager.mu.Unlock()
	service := NewService(&recoveryRepository{profile: p, job: j}, manager, NewProbeService(nil), Config{Enabled: true})
	service.retry[j.ID] = retryState{generation: j.Generation, attempts: 5, healthySince: time.Now().Add(-time.Minute), healthyVariantID: first.VariantID}
	old.cancel()
	select {
	case <-old.done:
	case <-ctx.Done():
		t.Fatal("old work did not stop")
	}
	if _, err = manager.Acquire(peer, p); err != nil {
		t.Fatal(err)
	}
	var restarted Runtime
	for {
		restarted, _ = manager.Status(j.ID)
		if restarted.State == Running && restarted.VariantID != first.VariantID {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("shared work did not recover")
		case <-time.After(5 * time.Millisecond):
		}
	}
	service.ReconcileAlways(ctx)
	retry, ok := service.retry[j.ID]
	if !ok || retry.attempts != 5 || retry.healthyVariantID != restarted.VariantID || time.Since(retry.healthySince) >= recoveryHealthyResetInterval {
		t.Fatalf("new physical work inherited old healthy interval: %+v", retry)
	}
}
