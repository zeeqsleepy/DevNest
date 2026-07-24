package proc

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// sleepEnv turns a re-execution of this test binary into a process that does
// nothing for a while.
//
// Terminating a process needs a process to terminate, and one that is neither
// a shell built-in nor a program that might be absent. Re-running the test
// binary is the portable way to get one: it exists by definition, on every
// platform, and it exits on its own if a test ever fails to clean up.
const sleepEnv = "DEVNEST_TEST_SLEEP"

func TestMain(m *testing.M) {
	if os.Getenv(sleepEnv) != "" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sleeper starts a child process and returns its pid.
func sleeper(t *testing.T) int {
	t.Helper()

	command := exec.Command(os.Args[0])
	command.Env = append(os.Environ(), sleepEnv+"=1")

	if err := command.Start(); err != nil {
		t.Fatalf("start a child process: %v", err)
	}

	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	return command.Process.Pid
}

func TestAliveFollowsAProcessThroughItsLife(t *testing.T) {
	pid := sleeper(t)

	if !(System{}).Alive(pid) {
		t.Fatalf("process %d was just started and is reported as gone", pid)
	}

	if _, err := (System{}).Terminate(context.Background(), pid, TerminateOptions{Force: true}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if (System{}).Alive(pid) {
		t.Errorf("process %d is still reported as running after being terminated", pid)
	}
}

func TestAliveRejectsImpossiblePids(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if (System{}).Alive(pid) {
			t.Errorf("pid %d is reported as running", pid)
		}
	}
}

func TestDescribeNamesARunningProcess(t *testing.T) {
	pid := sleeper(t)

	described := (System{}).Describe(pid)
	if described.PID != pid {
		t.Errorf("pid = %d, want %d", described.PID, pid)
	}
	if described.Name == "" {
		t.Error("name is empty for a process this test started")
	}
}

// A process that is not there has no name, and that is a description rather
// than an error: the caller asked what something is.
func TestDescribeIsSilentAboutWhatItCannotSee(t *testing.T) {
	if got := (System{}).Describe(0); got.Name != "" || got.PID != 0 {
		t.Errorf("Describe(0) = %+v, want the zero value", got)
	}
}

// The refusal is the point of this test, not the termination. Signalling pid 0
// addresses a whole process group on Unix and pid 1 is init; neither is ever
// what the user meant, and no flag lifts this.
func TestTerminateRefusesTheProcessesThatMustNeverBeSignalled(t *testing.T) {
	for _, pid := range []int{0, 1, -5} {
		_, err := (System{}).Terminate(context.Background(), pid, TerminateOptions{Force: true})
		if err == nil {
			t.Fatalf("pid %d was accepted", pid)
		}
		if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
			t.Errorf("pid %d: code = %q, want %q", pid, got, errors.CodeInvalidInput)
		}
	}
}

func TestTerminateReportsAProcessThatIsNotThere(t *testing.T) {
	// A pid that is almost certainly free: start a process, kill it, and reuse
	// its number before anything else claims it.
	pid := sleeper(t)
	if _, err := (System{}).Terminate(context.Background(), pid, TerminateOptions{Force: true}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	_, err := (System{}).Terminate(context.Background(), pid, TerminateOptions{Force: true})
	if err == nil {
		t.Fatal("terminating an exited process succeeded")
	}
	if got := errors.CodeOf(err); got != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", got, errors.CodeNotFound)
	}
}

// Without Force, the polite request is all that happens. On Unix that is
// SIGTERM and the child exits; on Windows there is no polite request at all
// and the caller is told so rather than handed a kill in disguise.
func TestTerminateWithoutForceStopsAtThePoliteRequest(t *testing.T) {
	pid := sleeper(t)

	result, err := (System{}).Terminate(context.Background(), pid, TerminateOptions{
		Grace: 3 * time.Second,
	})

	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("Windows reported a graceful termination, which it cannot perform")
		}
		if got := errors.CodeOf(err); got != errors.CodeUnsupported {
			t.Errorf("code = %q, want %q", got, errors.CodeUnsupported)
		}
		return
	}

	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !result.Graceful {
		t.Error("the process was killed when it had answered the request")
	}
	if (System{}).Alive(pid) {
		t.Error("the process is still running after a graceful termination")
	}
}

func TestTerminateObservesCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no graceful request, so there is no wait to cancel")
	}

	pid := sleeper(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The signal is sent before the wait, so cancellation is observed while
	// waiting rather than instead of acting. Either the process is already
	// gone or the wait reports the cancellation; both are correct, and what
	// must not happen is a hang.
	done := make(chan struct{})
	go func() {
		_, _ = (System{}).Terminate(ctx, pid, TerminateOptions{Grace: time.Minute})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Terminate ignored a cancelled context and kept waiting")
	}
}
