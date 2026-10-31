package transcode

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// boundedOutput drains subprocess output while retaining only a fixed prefix.
// A malicious/noisy tool cannot grow the probe's memory without bound.
type boundedOutput struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *boundedOutput) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

type CommandProbeRunner struct{}

func (CommandProbeRunner) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, &Error{Code: "probe_failed", Message: "media capability check failed or timed out"}
	}
	cmd := exec.Command(path, args...)
	configureProcess(cmd)
	cmd.Cancel = nil // Process-tree cancellation is managed by the owner below.
	cmd.WaitDelay = time.Second
	output := &boundedOutput{limit: 256 * 1024}
	cmd.Stdout = output
	cmd.Stderr = output
	owner, err := startOwnedProcess(cmd)
	if err != nil {
		return nil, &Error{Code: "probe_failed", Message: "media capability check failed or timed out"}
	}
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		owner.close()
		exited <- err
	}()
	select {
	case err = <-exited:
	case <-ctx.Done():
		_ = owner.kill()
		<-exited
		err = ctx.Err()
	}
	if err != nil || ctx.Err() != nil {
		return nil, &Error{Code: "probe_failed", Message: "media capability check failed or timed out"}
	}
	return output.Bytes(), nil
}
