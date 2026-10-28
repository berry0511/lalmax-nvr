//go:build windows

package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// This helper runs only in subprocesses. Its child lives until killed; both
// processes use finite waits so a failed test cannot leave indefinite work.
func TestWindowsJobHelper(t *testing.T) {
	switch os.Getenv("NVR_JOB_HELPER") {
	case "child":
		if err := os.WriteFile(os.Getenv("NVR_JOB_CHILD_READY"), []byte("ready"), 0600); err != nil {
			os.Exit(2)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsJobHelper$")
		child.Env = append(os.Environ(), "NVR_JOB_HELPER=child")
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("NVR_JOB_CHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(4)
		}
		if os.Getenv("NVR_JOB_PARENT_EXIT") == "1" {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(os.Getenv("NVR_JOB_PARENT_RELEASE")); err == nil {
					os.Exit(0)
				}
				time.Sleep(10 * time.Millisecond)
			}
			os.Exit(5)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

func TestWindowsJobKillsDescendants(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parentExit bool
	}{{"stop", false}, {"parent_exit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile, readyFile := filepath.Join(dir, "child.pid"), filepath.Join(dir, "child.ready")
			t.Setenv("NVR_JOB_HELPER", "parent")
			t.Setenv("NVR_JOB_CHILD_PID", pidFile)
			t.Setenv("NVR_JOB_CHILD_READY", readyFile)
			t.Setenv("NVR_JOB_PARENT_RELEASE", filepath.Join(dir, "release"))
			if tc.parentExit {
				t.Setenv("NVR_JOB_PARENT_EXIT", "1")
			} else {
				t.Setenv("NVR_JOB_PARENT_EXIT", "0")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			proc, err := startCommand(ctx, os.Args[0], []string{"-test.run=^TestWindowsJobHelper$"})
			if err != nil {
				t.Fatal(err)
			}
			defer proc.Stop(context.Background())
			var pid int
			for ctx.Err() == nil {
				data, readErr := os.ReadFile(pidFile)
				if readErr == nil {
					pid, err = strconv.Atoi(string(data))
					if err == nil {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("child PID was not reported")
			}
			handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(handle)
			if tc.parentExit {
				if err := os.WriteFile(filepath.Join(dir, "release"), []byte("exit"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := proc.Wait(); err != nil {
					t.Fatal(err)
				}
			} else {
				for ctx.Err() == nil {
					if _, err := os.Stat(readyFile); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err := proc.Stop(ctx); err != nil {
					t.Fatal(err)
				}
				_ = proc.Wait()
			}
			status, err := windows.WaitForSingleObject(handle, 3000)
			if err != nil || status != windows.WAIT_OBJECT_0 {
				t.Fatalf("descendant did not exit: status=%d err=%v", status, err)
			}
		})
	}
}

func TestWindowsProbeCancellationKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	t.Setenv("NVR_JOB_HELPER", "parent")
	t.Setenv("NVR_JOB_CHILD_PID", pidFile)
	t.Setenv("NVR_JOB_CHILD_READY", filepath.Join(dir, "child.ready"))
	t.Setenv("NVR_JOB_PARENT_EXIT", "0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (CommandProbeRunner{}).Run(ctx, os.Args[0], "-test.run=^TestWindowsJobHelper$")
		done <- err
	}()
	deadline := time.Now().Add(8 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid != 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("probe child did not start")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled probe reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled probe did not return")
	}
	status, err := windows.WaitForSingleObject(handle, 3000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("probe descendant did not exit: status=%d err=%v", status, err)
	}
}

func TestWindowsOwnerCrashHelper(t *testing.T) {
	if os.Getenv("NVR_JOB_OWNER_CRASH") != "1" {
		return
	}
	if _, err := startCommand(context.Background(), os.Args[0], []string{"-test.run=^TestWindowsJobHelper$"}); err != nil {
		os.Exit(6)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("NVR_JOB_PARENT_RELEASE")); err == nil {
			os.Exit(42)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(7)
}

func TestWindowsJobClosesWhenOwnerCrashes(t *testing.T) {
	dir := t.TempDir()
	pidFile, releaseFile := filepath.Join(dir, "child.pid"), filepath.Join(dir, "release")
	t.Setenv("NVR_JOB_OWNER_CRASH", "1")
	t.Setenv("NVR_JOB_HELPER", "parent")
	t.Setenv("NVR_JOB_CHILD_PID", pidFile)
	t.Setenv("NVR_JOB_CHILD_READY", filepath.Join(dir, "child.ready"))
	t.Setenv("NVR_JOB_PARENT_RELEASE", releaseFile)
	t.Setenv("NVR_JOB_PARENT_EXIT", "0")
	wrapper := exec.Command(os.Args[0], "-test.run=^TestWindowsOwnerCrashHelper$")
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wrapper.Process.Kill(); _ = wrapper.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid != 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("managed descendant did not start")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := os.WriteFile(releaseFile, []byte("crash"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := wrapper.Wait(); err == nil {
		t.Fatal("owner was expected to exit abruptly")
	}
	status, err := windows.WaitForSingleObject(handle, 3000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived owner exit: status=%d err=%v", status, err)
	}
}
