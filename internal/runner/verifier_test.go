package runner

import (
	"strings"
	"testing"
	"time"
)

func TestRunVerifierCapturesPassAndCombinedFailure(t *testing.T) {
	verification, err := RunVerifier("printf out; printf err >&2", time.Second)
	if err != nil || !verification.Passed || verification.ExitCode != 0 || verification.Output != "outerr" {
		t.Fatalf("passing verification = %+v err=%v", verification, err)
	}

	verification, err = RunVerifier("printf no; printf why >&2; exit 3", time.Second)
	if err != nil || verification.Passed || verification.ExitCode != 3 || verification.Output != "nowhy" {
		t.Fatalf("failing verification = %+v err=%v", verification, err)
	}
}

func TestRunVerifierRejectsConfigurationExit(t *testing.T) {
	verification, err := RunVerifier("exit 126", time.Second)
	if err == nil || verification.ExitCode != 126 {
		t.Fatalf("verification = %+v err=%v", verification, err)
	}
}

func TestRunVerifierTimesOut(t *testing.T) {
	verification, err := RunVerifier("sleep 1", 10*time.Millisecond)
	if err == nil || !strings.Contains(verification.Error, "timed out") {
		t.Fatalf("verification = %+v err=%v", verification, err)
	}
}

func TestRunVerifierBoundsOutput(t *testing.T) {
	verification, err := RunVerifier("yes x | head -c 9000", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Output) != verificationOutputLimit {
		t.Fatalf("output length = %d", len(verification.Output))
	}
}

func TestRunVerifierReportsStartFailure(t *testing.T) {
	old := shellPath
	shellPath = "/missing/watchd-shell"
	t.Cleanup(func() { shellPath = old })
	verification, err := RunVerifier("true", time.Second)
	if err == nil || verification.Error == "" {
		t.Fatalf("verification = %+v err=%v", verification, err)
	}
}
