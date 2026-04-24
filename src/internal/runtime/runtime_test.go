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
