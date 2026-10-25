package download

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	resumeProcessEnvironment      = "DAI_TEST_RESUME_LOCK"
	resumeProcessSelection        = "-test.run=^TestResumeLockSurvivesProcessRestart$"
	resumeProcessReady            = "LOCKED\n"
	resumeProcessUnexpectedSignal = "lock owner failed to become ready: %q, %v"
	resumeProcessJoinTimeout      = "terminated lock owner did not exit before deadline"
	resumeProcessUnexpectedExit   = "terminated lock owner exited successfully"
)

func TestResumeLockSurvivesProcessRestart(test *testing.T) {
	if path := os.Getenv(resumeProcessEnvironment); path != "" {
		lock, err := newResumeLock(path)
		if err != nil {
			test.Fatal(err)
		}

		if _, err := fmt.Fprint(os.Stdout, resumeProcessReady); err != nil {
			test.Fatal(err)
		}

		_, copyError := io.Copy(io.Discard, os.Stdin)

		if err := lock.Close(); err != nil {
			test.Fatal(err)
		}

		if copyError != nil {
			test.Fatal(copyError)
		}

		return
	}

	operationContext, cancel := context.WithTimeout(test.Context(), parallelDeadline)
	defer cancel()

	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}

	lockPath := filepath.Join(test.TempDir(), fmt.Sprintf(resumeLockFormat, parallelPlaylistID))
	command := exec.CommandContext(operationContext, executable, resumeProcessSelection)

	command.Env = append(os.Environ(), resumeProcessEnvironment+"="+lockPath)

	output, err := command.StdoutPipe()
	if err != nil {
		test.Fatal(err)
	}

	input, err := command.StdinPipe()
	if err != nil {
		test.Fatal(err)
	}

	defer func() {
		_ = input.Close()
	}()

	if err := command.Start(); err != nil {
		test.Fatal(err)
	}

	done := make(chan struct{})

	var processError error
	go func() {
		processError = command.Wait()

		close(done)
	}()

	test.Cleanup(func() {
		cancel()

		<-done
	})

	reader := bufio.NewReader(output)

	signal, err := reader.ReadString('\n')
	if err != nil || signal != resumeProcessReady {
		test.Fatalf(resumeProcessUnexpectedSignal, signal, err)
	}

	other, err := newResumeLock(lockPath)
	if err == nil {
		_ = other.Close()

		test.Fatal(resumeTestBusyLock)
	}

	if err := command.Process.Kill(); err != nil {
		test.Fatal(err)
	}

	select {
	case <-done:
	case <-operationContext.Done():
		test.Fatal(resumeProcessJoinTimeout)
	}

	if processError == nil {
		test.Fatal(resumeProcessUnexpectedExit)
	}

	recovered, err := newResumeLock(lockPath)
	if err != nil {
		test.Fatal(err)
	}

	if err := recovered.Close(); err != nil {
		test.Fatal(err)
	}
}
