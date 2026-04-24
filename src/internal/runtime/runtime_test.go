package runtime

import (
	"context"
	"testing"

	"github.com/caorenmei/autoproxy3/src/internal/config"
	"github.com/caorenmei/autoproxy3/src/internal/rulesources"
)

func TestNewReturnsRunnerThatRuns(t *testing.T) {
	cfg := config.Config{ListenAddr: "127.0.0.1:8080", AutoDetect: config.AutoDetectConfig{RulesPath: "/var/lib/autoproxy/auto.txt"}}
	runner, err := New(cfg)
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

func TestNewPreparesRuleSourceDependencies(t *testing.T) {
	cfg := config.Config{ListenAddr: "127.0.0.1:8080", AutoDetect: config.AutoDetectConfig{RulesPath: "/var/lib/autoproxy/auto.txt"}}
	runnerValue, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	concrete, ok := runnerValue.(runner)
	if !ok {
		t.Fatalf("expected concrete runner, got %T", runnerValue)
	}
	if concrete.customRuleLoader == nil {
		t.Fatal("expected custom rule loader")
	}
	store, ok := concrete.autoDetectStore.(rulesources.AutoDetectStore)
	if !ok {
		t.Fatalf("expected auto-detect store type, got %T", concrete.autoDetectStore)
	}
	if store.Path != cfg.AutoDetect.RulesPath {
		t.Fatalf("unexpected auto-detect rules path: got %q want %q", store.Path, cfg.AutoDetect.RulesPath)
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
