package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/level09/watchd/internal/store"
)

const verificationOutputLimit = 8 * 1024

var shellPath = "sh"

func RunVerifier(command string, timeout time.Duration) (*store.Verification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	output, runErr := cmd.CombinedOutput()
	verification := &store.Verification{
		Command: command, Output: truncateVerificationOutput(output),
		ExitCode: -1, DurationMS: time.Since(start).Milliseconds(),
	}
	if ctx.Err() == context.DeadlineExceeded {
		verification.Error = fmt.Sprintf("verifier timed out after %s", timeout)
		return verification, fmt.Errorf("%s", verification.Error)
	}
	if runErr == nil {
		verification.Passed = true
		verification.ExitCode = 0
		return verification, nil
	}
	var exitErr *exec.ExitError
	if !asExitError(runErr, &exitErr) {
		verification.Error = runErr.Error()
		return verification, fmt.Errorf("starting verifier: %w", runErr)
	}
	verification.ExitCode = exitErr.ExitCode()
	if verification.ExitCode == 126 || verification.ExitCode == 127 {
		verification.Error = fmt.Sprintf("verifier command exited %d", verification.ExitCode)
		return verification, fmt.Errorf("%s", verification.Error)
	}
	return verification, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func truncateVerificationOutput(output []byte) string {
	output = bytes.TrimSpace(output)
	if len(output) > verificationOutputLimit {
		output = output[:verificationOutputLimit]
	}
	return string(output)
}
