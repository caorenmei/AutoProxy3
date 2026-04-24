package runtime

import (
	"context"
	"testing"

	"github.com/caorenmei/autoproxy3/src/internal/config"
)

func TestNewReturnsRunnerThatRuns(t *testing.T) {
	runner, err := New(config.Config{ListenAddr: "127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if runner == nil {
		t.Fatal("expected runner")
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunReturnsContextErrorWhenCancelled(t *testing.T) {
	runner, err := New(config.Config{ListenAddr: "127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = runner.Run(ctx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}
