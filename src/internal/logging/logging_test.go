package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLoggerCreatesJSONHandler(t *testing.T) {
	logger, err := New(Options{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	if logger == nil {
		t.Fatal("expected logger")
	}
}

func TestNewHandlerCreatesJSONHandler(t *testing.T) {
	handler, err := newHandler("json", io.Discard, slog.LevelDebug)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	if _, ok := handler.(*slog.JSONHandler); !ok {
		t.Fatalf("expected JSON handler, got %T", handler)
	}
}

func TestNewHandlerCreatesTextHandler(t *testing.T) {
	handler, err := newHandler("text", io.Discard, slog.LevelInfo)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	if _, ok := handler.(*slog.TextHandler); !ok {
		t.Fatalf("expected Text handler, got %T", handler)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	_, err := New(Options{Level: "trace", Format: "json"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := `invalid level "trace"`; err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestNewRejectsInvalidFormat(t *testing.T) {
	_, err := New(Options{Level: "info", Format: "xml"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := `invalid format "xml"`; err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestNewWriterUsesStdoutWhenFilePathEmpty(t *testing.T) {
	writer := newWriter(Options{})
	if writer != os.Stdout {
		t.Fatalf("expected stdout writer, got %T", writer)
	}
}

func TestNewWriterUsesLumberjackWhenFilePathSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoproxy.log")
	writer := newWriter(Options{FilePath: path, MaxSize: 12, MaxBackups: 7})

	if got := fmt.Sprintf("%T", writer); got != "*lumberjack.Logger" {
		t.Fatalf("expected lumberjack logger, got %s", got)
	}
	logger, err := New(Options{Level: "info", Format: "text", FilePath: path, MaxSize: 12, MaxBackups: 7})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Info("file output")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected log file content")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLevel(tc.input)
			if err != nil {
				t.Fatalf("parse level: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected level: got %v want %v", got, tc.want)
			}
		})
	}
}
