package sqidsd

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCLINoAddress(t *testing.T) {
	if err := RunCLI(context.Background(), []string{}); err == nil {
		t.Error("RunCLI must fail without a listen address")
	}
}

func TestRunCLITCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := RunCLI(ctx, []string{"127.0.0.1:0"}); err != nil {
		t.Errorf("RunCLI returned an error: %v", err)
	}
}

func TestRunCLIMultipleAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	sock := filepath.Join(t.TempDir(), "multi.sock")
	if err := RunCLI(ctx, []string{sock, "127.0.0.1:0"}); err != nil {
		t.Errorf("RunCLI returned an error: %v", err)
	}
}

func TestRunCLIEnv(t *testing.T) {
	t.Setenv("SQIDSD_ADDRESS", filepath.Join(t.TempDir(), "env.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := RunCLI(ctx, nil); err != nil {
		t.Errorf("RunCLI returned an error: %v", err)
	}
}

func TestRunCLIInvalidAlphabet(t *testing.T) {
	err := RunCLI(context.Background(), []string{"127.0.0.1:0", "--alphabet", "ab"})
	if err == nil {
		t.Error("RunCLI must fail with a too short alphabet")
	}
}
